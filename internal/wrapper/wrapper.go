package wrapper

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/mission"
	"github.com/odyssey/agenc/internal/server"
)

const (
	repoRefDebouncePeriod = 5 * time.Second
	// Heartbeat interval: 10s provides responsive activity tracking for mission
	// sorting while keeping server request volume manageable.
	heartbeatInterval = 10 * time.Second
)

// Wrapper manages a Claude child process for a single mission.
type Wrapper struct {
	agencDirpath   string
	missionID      string
	gitRepoName    string
	initialPrompt  string
	defaultModel   string
	claudeArgs     []string
	missionDirpath string
	agentDirpath   string
	client         *server.Client
	claudeCmd      *exec.Cmd
	logger         *slog.Logger
	tmuxPaneID     string // numeric pane ID from $TMUX_PANE (%-prefix stripped); empty for headless

	// hasConversation tracks whether a Claude conversation exists that can be
	// resumed with `claude -c`. Set to true at startup for resumes, and flipped
	// to true when the user submits their first message (via UserPromptSubmit
	// hook). Both reads and writes happen in the main event loop.
	hasConversation bool

	// claudeIdle tracks whether Claude is currently idle (waiting for user
	// input). Initialized true (Claude hasn't started processing yet). Updated
	// by handleClaudeUpdate from the main event loop.
	claudeIdle bool

	// stateMu protects claudeIdle, hasConversation, state, and needsAttention.
	// The HTTP GET /status handler reads these fields concurrently with the
	// main event loop, so all reads and writes must hold the appropriate lock.
	stateMu          sync.RWMutex
	needsAttention   bool      // true when Claude needs user attention (permission prompt etc.)
	lastUserPromptAt time.Time // zero value means no prompt yet this session

	// Channels for internal communication between goroutines and the main loop.
	// All are buffered with capacity 1 and use non-blocking sends to avoid
	// goroutine leaks.
	claudeExited chan error // receives the exit error from cmd.Wait()

	// commandCh receives commands from the HTTP server goroutine.
	commandCh chan commandWithResponse

	// rebuilding is set while a devcontainer rebuild is in progress.
	// Credential sync goroutines skip work while this is true to avoid racing
	// with claude-config regeneration during the rebuild.
	rebuilding atomic.Bool

	// perMissionCredentialHash caches the SHA-256 hash of the per-mission
	// Keychain credential JSON. The upward sync goroutine compares the current
	// Keychain contents against this hash to detect when Claude updates MCP
	// OAuth tokens. Protected by credentialHashMu since both the upward and
	// downward sync goroutines access it.
	perMissionCredentialHash string
	credentialHashMu         sync.Mutex

	// lastDownwardSyncTimestamp is the broadcast file timestamp from the most
	// recent downward sync. Used to skip stale broadcasts and avoid re-applying
	// the same global credential update twice.
	lastDownwardSyncTimestamp float64

	// devcontainer holds the state for containerized missions. Nil when the
	// mission's repo does not have a devcontainer.json.
	devcontainer *devcontainerState

	// Window coloring configuration for tmux state feedback. Read from config.yml at startup.
	// Empty strings mean that specific color setting is disabled.
	windowBusyBackgroundColor      string
	windowBusyForegroundColor      string
	windowAttentionBackgroundColor string
	windowAttentionForegroundColor string
}

// NewWrapper creates a new Wrapper for the given mission. The initialPrompt
// parameter is optional; if non-empty, it will be passed to Claude when
// starting a new conversation (not used for resumes).
func NewWrapper(agencDirpath string, missionID string, gitRepoName string, initialPrompt string) *Wrapper {
	// Load window coloring config from config.yml
	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	var titleCfg *config.TmuxWindowTitleConfig
	var defaultModel string
	var claudeArgs []string
	if err == nil {
		titleCfg = cfg.GetTmuxWindowTitleConfig()
		defaultModel = cfg.GetDefaultModel(gitRepoName)
		claudeArgs = cfg.GetClaudeArgs(gitRepoName)
	} else {
		titleCfg = &config.TmuxWindowTitleConfig{}
	}

	return &Wrapper{
		agencDirpath:                   agencDirpath,
		missionID:                      missionID,
		gitRepoName:                    gitRepoName,
		initialPrompt:                  initialPrompt,
		defaultModel:                   defaultModel,
		claudeArgs:                     claudeArgs,
		missionDirpath:                 config.GetMissionDirpath(agencDirpath, missionID),
		agentDirpath:                   config.GetMissionAgentDirpath(agencDirpath, missionID),
		client:                         server.NewClient(config.GetServerSocketFilepath(agencDirpath)),
		claudeExited:                   make(chan error, 1),
		commandCh:                      make(chan commandWithResponse, 1),
		claudeIdle:                     true,
		windowBusyBackgroundColor:      titleCfg.GetBusyBackgroundColor(),
		windowBusyForegroundColor:      titleCfg.GetBusyForegroundColor(),
		windowAttentionBackgroundColor: titleCfg.GetAttentionBackgroundColor(),
		windowAttentionForegroundColor: titleCfg.GetAttentionForegroundColor(),
	}
}

// cloneCredentials copies fresh credentials from the global Keychain into the
// per-mission entry so Claude has access to current MCP OAuth tokens at spawn.
func (w *Wrapper) cloneCredentials() {
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	if err := claudeconfig.CloneKeychainCredentials(claudeConfigDirpath); err != nil {
		w.logger.Warn("Failed to clone Keychain credentials", "error", err)
	}
}

// writeBackCredentials merges per-mission Keychain credentials back into the
// global entry so MCP OAuth tokens acquired in this mission persist.
func (w *Wrapper) writeBackCredentials() {
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	if err := claudeconfig.WriteBackKeychainCredentials(claudeConfigDirpath); err != nil {
		if w.logger != nil {
			w.logger.Warn("Failed to write back Keychain credentials", "error", err)
		}
	}
}

// runResources bundles resources created during setup that the caller must
// clean up (via deferred calls) and that the event loop needs.
type runResources struct {
	logFile *os.File
	cancel  context.CancelFunc
	sigCh   chan os.Signal
}

// setupRun performs all one-time initialization for the wrapper lifecycle:
// OAuth check, logging, PID file, signal handling, background goroutines,
// credential cloning, and initial Claude spawn. It returns the resources
// needed by the event loop and registers deferred cleanup on the caller's
// behalf via the returned cleanup function.
func (w *Wrapper) setupRun(isResume bool) (*runResources, func(), error) {
	// Ensure OAuth token exists before installing signal handlers. This must
	// happen first so Ctrl-C works naturally during the interactive setup flow.
	if err := config.SetupOAuthToken(w.agencDirpath); err != nil {
		return nil, nil, err
	}

	// Set up logger that writes to the log file
	logFilepath := config.GetMissionWrapperLogFilepath(w.agencDirpath, w.missionID)
	logFile, err := os.OpenFile(logFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "failed to open wrapper log file")
	}
	w.logger = slog.New(slog.NewJSONHandler(logFile, nil))
	w.logger.Info("Wrapper started",
		"mission_id", database.ShortID(w.missionID),
		"repo", w.gitRepoName,
		"is_resume", isResume,
	)

	// Write wrapper PID
	pidFilepath := config.GetMissionPIDFilepath(w.agencDirpath, w.missionID)
	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		logFile.Close()
		return nil, nil, stacktrace.Propagate(err, "failed to write wrapper PID file")
	}

	// Set up context for background goroutines
	ctx, cancel := context.WithCancel(context.Background())

	// Catch SIGINT, SIGTERM, and SIGHUP to prevent Go's default termination.
	// Claude is in the same process group, so terminal Ctrl-C reaches it
	// directly. We catch the signal here just to keep the wrapper alive
	// until Claude exits. SIGHUP is sent by tmux when a window or session
	// is destroyed — without handling it, deferred cleanup (PID file removal)
	// would not run.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	// Capture the tmux pane ID (e.g. "%42" -> "42") for heartbeat reporting.
	w.tmuxPaneID = strings.TrimPrefix(os.Getenv("TMUX_PANE"), "%")

	// Write initial heartbeat and start periodic heartbeat loop
	if err := w.client.Heartbeat(w.missionID, w.tmuxPaneID, ""); err != nil {
		w.logger.Warn("Failed to write initial heartbeat", "error", err)
	}
	go w.writeHeartbeat(ctx)

	// Start background watcher for git remote ref changes
	if w.gitRepoName != "" {
		go w.watchWorkspaceRemoteRefs(ctx)
	}

	// Start HTTP server for receiving commands (restart, status, etc.)
	socketFilepath := config.GetMissionSocketFilepath(w.agencDirpath, w.missionID)
	go startHTTPServer(ctx, socketFilepath, w, w.logger)

	// Clone global MCP credentials into per-mission Keychain and start sync goroutines.
	w.cloneCredentials()
	w.initCredentialHash()
	go w.watchCredentialUpwardSync(ctx)
	go w.watchCredentialDownwardSync(ctx)

	// Detect and setup devcontainer (after socket server is started so wrapper.sock exists)
	dcState, dcErr := w.detectAndSetupDevcontainer()
	if dcErr != nil {
		cancel()
		signal.Stop(sigCh)
		logFile.Close()
		_ = os.Remove(pidFilepath)
		return nil, nil, stacktrace.Propagate(dcErr, "devcontainer setup failed")
	}
	w.devcontainer = dcState

	if dcState != nil {
		w.logger.Info("Starting devcontainer", "config", dcState.mergedConfigPath)
		if upErr := devcontainerUp(dcState); upErr != nil {
			cancel()
			signal.Stop(sigCh)
			logFile.Close()
			_ = os.Remove(pidFilepath)
			return nil, nil, stacktrace.Propagate(upErr, "devcontainer up failed")
		}
		w.logger.Info("Devcontainer started successfully")
	}

	// Track whether a resumable conversation exists. For resumes, one already
	// exists. For new missions, we start with false and flip to true when the
	// first UserPromptSubmit hook fires.
	if isResume {
		w.hasConversation = true
	}

	// Change the wrapper's working directory to the agent directory so that
	// tmux's #{pane_current_path} reflects the mission directory. This makes
	// built-in tmux splits (prefix + %, prefix + ") open in the agent dir.
	// NOTE: This only works when the wrapper IS the process group leader —
	// i.e., the shell that tmux spawned has exec'd into us (requires a simple
	// command, not a compound one with ; or &&).
	if err := os.Chdir(w.agentDirpath); err != nil {
		w.logger.Warn("Failed to chdir to agent directory", "path", w.agentDirpath, "error", err)
	}

	// Spawn initial Claude process
	if err := w.spawnClaude(isResume); err != nil {
		cancel()
		signal.Stop(sigCh)
		logFile.Close()
		_ = os.Remove(pidFilepath)
		return nil, nil, stacktrace.Propagate(err, "failed to spawn claude")
	}

	// Wait on Claude in a background goroutine
	go func() {
		w.claudeExited <- w.claudeCmd.Wait()
	}()

	res := &runResources{
		logFile: logFile,
		cancel:  cancel,
		sigCh:   sigCh,
	}

	cleanup := func() {
		w.resetWindowTabStyle()
		w.writeBackCredentials()
		if w.devcontainer != nil {
			w.logger.Info("Stopping devcontainer")
			if stopErr := devcontainerStop(w.devcontainer); stopErr != nil {
				w.logger.Error("Failed to stop devcontainer", "error", stopErr)
			}
		}
		signal.Stop(sigCh)
		cancel()
		_ = os.Remove(pidFilepath)
		logFile.Close()
	}

	return res, cleanup, nil
}

// spawnClaude starts a new Claude process, either resuming an existing session
// or starting fresh. When isResume is true and a valid session exists, it
// resumes that session; otherwise it starts a new conversation. For non-resume
// spawns, the wrapper's initialPrompt is passed to Claude.
//
// For containerized missions, claude-config is regenerated with container mode
// before each spawn, and Claude is launched via `devcontainer exec` instead of
// running the binary directly.
func (w *Wrapper) spawnClaude(isResume bool) error {
	isContainerized := w.devcontainer != nil

	// Regenerate claude-config before every containerized spawn so the
	// latest global config takes effect on reload.
	if isContainerized {
		trustedMcpServers := w.loadTrustedMcpServers()
		if err := claudeconfig.BuildMissionConfigDir(
			w.agencDirpath, w.missionID, trustedMcpServers, isContainerized,
		); err != nil {
			return stacktrace.Propagate(err, "failed to regenerate claude-config for containerized mission")
		}
	}

	if isContainerized {
		return w.spawnClaudeInContainer(isResume)
	}
	return w.spawnClaudeDirectly(isResume)
}

// spawnClaudeDirectly spawns Claude as a local process (non-containerized path).
func (w *Wrapper) spawnClaudeDirectly(isResume bool) error {
	var cmd *exec.Cmd
	var err error

	if isResume {
		sessionID := claudeconfig.GetLastSessionID(w.agencDirpath, w.missionID)
		if sessionID != "" && claudeconfig.ProjectDirectoryExists(w.agentDirpath) {
			cmd, err = mission.SpawnClaudeResumeWithSession(w.agencDirpath, w.missionID, w.agentDirpath, w.defaultModel, w.claudeArgs, sessionID, w.initialPrompt)
		} else {
			cmd, err = mission.SpawnClaudeWithPrompt(w.agencDirpath, w.missionID, w.agentDirpath, w.defaultModel, w.claudeArgs, w.initialPrompt)
		}
	} else {
		cmd, err = mission.SpawnClaudeWithPrompt(w.agencDirpath, w.missionID, w.agentDirpath, w.defaultModel, w.claudeArgs, w.initialPrompt)
	}

	if err != nil {
		return stacktrace.Propagate(err, "failed to spawn claude process")
	}
	w.claudeCmd = cmd
	return nil
}

// spawnClaudeInContainer spawns Claude inside the devcontainer via
// `devcontainer exec`. Env vars and config are set via bind mounts and
// containerEnv in the devcontainer.json, not on the local exec.Cmd.
func (w *Wrapper) spawnClaudeInContainer(isResume bool) error {
	// Build the claude args the same way the direct spawn does
	var claudeArgs []string
	if w.defaultModel != "" {
		claudeArgs = append(claudeArgs, "--model", w.defaultModel)
	}
	claudeArgs = append(claudeArgs, w.claudeArgs...)

	if isResume {
		sessionID := claudeconfig.GetLastSessionID(w.agencDirpath, w.missionID)
		if sessionID != "" && claudeconfig.ProjectDirectoryExists(w.agentDirpath) {
			claudeArgs = append(claudeArgs, "-r", sessionID)
		}
		// If no session to resume, start fresh (no extra args)
	}
	if w.initialPrompt != "" {
		claudeArgs = append(claudeArgs, w.initialPrompt)
	}

	cmd := devcontainerExecClaude(w.devcontainer, claudeArgs)
	if err := cmd.Start(); err != nil {
		return stacktrace.Propagate(err, "failed to start claude in devcontainer")
	}

	w.claudeCmd = cmd
	return nil
}

// loadTrustedMcpServers reads the MCP trust configuration for this mission's repo.
func (w *Wrapper) loadTrustedMcpServers() *config.TrustedMcpServers {
	if w.gitRepoName == "" {
		return nil
	}
	cfg, _, err := config.ReadAgencConfig(w.agencDirpath)
	if err != nil {
		return nil
	}
	rc, ok := cfg.GetRepoConfig(w.gitRepoName)
	if !ok {
		return nil
	}
	return rc.TrustedMcpServers
}

// Run executes the wrapper lifecycle. For a new mission, pass isResume=false.
// For a resume, pass isResume=true. Run blocks until Claude exits naturally
// or the wrapper shuts down.
func (w *Wrapper) Run(isResume bool) error {
	res, cleanup, err := w.setupRun(isResume)
	if err != nil {
		return stacktrace.Propagate(err, "failed to initialize wrapper")
	}
	defer cleanup()

	// Main event loop with three-state machine:
	//   Running → RestartPending → Restarting → Running
	for {
		select {
		case sig := <-res.sigCh:
			w.handleSignal(sig)
			return nil

		case cmdResp := <-w.commandCh:
			resp := w.handleCommand(cmdResp.cmd)
			cmdResp.responseCh <- resp

		case exitErr := <-w.claudeExited:
			done, err := w.handleClaudeExit(exitErr)
			if done {
				if err != nil {
					return stacktrace.Propagate(err, "failed to handle claude exit")
				}
				return nil
			}
		}
	}
}

// handleSignal processes a signal received by the wrapper. It forwards the
// signal to the Claude process and waits for it to exit.
func (w *Wrapper) handleSignal(sig os.Signal) {
	w.logger.Info("Wrapper exiting",
		"reason", "signal",
		"signal", sig.String(),
	)
	if w.claudeCmd != nil && w.claudeCmd.Process != nil {
		_ = w.claudeCmd.Process.Signal(sig)
	}
	<-w.claudeExited
}

// handleClaudeExit processes Claude's exit. The wrapper exits when claude
// exits — there is no in-process restart path; reload is handled externally
// via tmux respawn-pane (see internal/server reloadMissionInTmux).
func (w *Wrapper) handleClaudeExit(exitErr error) (done bool, err error) {
	// Natural exit — wrapper exits
	exitCode := 0
	if exitErr != nil {
		if ee, ok := exitErr.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}
	w.logger.Info("Wrapper exiting",
		"reason", "claude_exited",
		"exit_code", exitCode,
		"exit_error", fmt.Sprintf("%v", exitErr),
	)

	// If Claude exited with an error, pause so the user can see
	// any error messages Claude printed to the terminal before
	// the tmux window closes.
	if exitCode != 0 {
		fmt.Fprintf(os.Stderr, "\nClaude exited with code %d. Press Enter to close this window.\n", exitCode)
		_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n') // intentionally ignored: press-enter-to-continue prompt
	}
	return true, nil
}

// handleCommand processes a command from the HTTP server and returns a CommandResponse.
func (w *Wrapper) handleCommand(cmd Command) CommandResponse {
	switch cmd.Command {
	case "claude_update":
		return w.handleClaudeUpdate(cmd)
	case "rebuild":
		return w.handleRebuildCommand()
	default:
		return CommandResponse{Status: "error", Error: "unknown command: " + cmd.Command}
	}
}

// handleRebuildCommand tears down and rebuilds the devcontainer, then respawns
// Claude in the rebuilt container. Only valid for containerized missions.
func (w *Wrapper) handleRebuildCommand() CommandResponse {
	if w.devcontainer == nil {
		return CommandResponse{Status: "error", Error: "mission is not containerized"}
	}

	w.logger.Info("Rebuild requested, stopping Claude and rebuilding container")

	w.rebuilding.Store(true)
	defer w.rebuilding.Store(false)

	w.stateMu.Lock()
	w.hasConversation = false
	w.stateMu.Unlock()

	if w.claudeCmd != nil && w.claudeCmd.Process != nil {
		_ = w.claudeCmd.Process.Signal(syscall.SIGINT)
		// Wait for Claude to exit before rebuilding. Consuming the channel here
		// prevents the main loop's handleClaudeExit from running, since rebuild
		// manages the respawn directly below.
		<-w.claudeExited
	}

	// Rebuild the container
	if err := devcontainerRebuild(w.devcontainer); err != nil {
		w.logger.Error("Devcontainer rebuild failed", "error", err)
		return CommandResponse{Status: "error", Error: "rebuild failed: " + err.Error()}
	}

	// Respawn Claude in the rebuilt container
	if err := w.spawnClaude(false); err != nil {
		w.logger.Error("Failed to respawn Claude after rebuild", "error", err)
		return CommandResponse{Status: "error", Error: "respawn failed: " + err.Error()}
	}

	// Wait on the new Claude process
	go func() {
		w.claudeExited <- w.claudeCmd.Wait()
	}()

	w.logger.Info("Devcontainer rebuild complete, Claude respawned", "pid", w.claudeCmd.Process.Pid)
	return CommandResponse{Status: "ok"}
}

// handleClaudeUpdate processes a claude_update command sent by hooks. It
// updates the wrapper's idle state, hasConversation flag, needsAttention flag,
// and sets tmux pane colors for visual feedback.
func (w *Wrapper) handleClaudeUpdate(cmd Command) CommandResponse {
	w.logger.Info("Received claude_update", "event", cmd.Event, "notification_type", cmd.NotificationType)

	w.stateMu.Lock()
	defer w.stateMu.Unlock()

	switch cmd.Event {
	case "Stop":
		w.claudeIdle = true
		w.hasConversation = true
		w.needsAttention = false
		w.resetWindowTabStyle()

	case "UserPromptSubmit":
		w.claudeIdle = false
		w.hasConversation = true
		w.needsAttention = false
		w.lastUserPromptAt = time.Now().UTC()
		w.setWindowBusy()
		if err := w.client.RecordPrompt(w.missionID); err != nil {
			w.logger.Warn("Failed to record prompt", "error", err)
		}

	case "PostToolUse", "PostToolUseFailure":
		// A tool just completed (or failed) — Claude is still actively working,
		// so reset the window to busy in case a permission prompt turned it orange.
		w.needsAttention = false
		w.setWindowBusy()

	case "Notification":
		// Color the pane for notification types that need user attention
		switch cmd.NotificationType {
		case "permission_prompt", "elicitation_dialog":
			w.needsAttention = true
			w.setWindowNeedsAttention()
		}
	}

	return CommandResponse{Status: "ok"}
}

// getClaudeStateString returns the Claude state as a string for the status API.
// Must be called with stateMu held (at least RLock).
func (w *Wrapper) getClaudeStateString() string {
	if w.needsAttention {
		return "needs_attention"
	}
	if w.claudeIdle {
		return "idle"
	}
	return "busy"
}

// resolveRepoDirpath returns the path to the git repository within the
// mission's agent directory. It supports both the new structure (repo is
// directly in agent/) and the legacy structure (repo is in
// agent/workspace/<repo-short-name>/).
func (w *Wrapper) resolveRepoDirpath() string {
	agentDirpath := config.GetMissionAgentDirpath(w.agencDirpath, w.missionID)

	// Check for legacy workspace/<repo>/ structure
	legacyWorkspaceDirpath := filepath.Join(agentDirpath, "workspace")
	if entries, err := os.ReadDir(legacyWorkspaceDirpath); err == nil {
		for _, entry := range entries {
			if entry.IsDir() {
				return filepath.Join(legacyWorkspaceDirpath, entry.Name())
			}
		}
	}

	// New structure: repo is directly in agent/
	return agentDirpath
}

// watchWorkspaceRemoteRefs uses fsnotify to watch the mission repo's
// .git/refs/remotes/origin/ directory for changes to the default branch ref.
// When the ref changes (e.g. after `git push origin main`), force-updates the
// repo library clone so other missions get fresh copies.
func (w *Wrapper) watchWorkspaceRemoteRefs(ctx context.Context) {
	repoDirpath := w.resolveRepoDirpath()

	defaultBranch, err := mission.GetDefaultBranch(repoDirpath)
	if err != nil {
		w.logger.Warn("Failed to determine default branch for mission repo", "error", err)
		return
	}

	refsDirpath := filepath.Join(repoDirpath, ".git", "refs", "remotes", "origin")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.logger.Warn("Failed to create fsnotify watcher for remote refs", "error", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(refsDirpath); err != nil {
		w.logger.Warn("Failed to watch remote refs directory", "dir", refsDirpath, "error", err)
		return
	}

	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}
	timerActive := false

	for {
		select {
		case <-ctx.Done():
			if !debounceTimer.Stop() && timerActive {
				<-debounceTimer.C
			}
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(event.Name) != defaultBranch {
				continue
			}
			if !(event.Has(fsnotify.Write) || event.Has(fsnotify.Create)) {
				continue
			}
			// Debounce to avoid rapid successive updates
			if !debounceTimer.Stop() && timerActive {
				<-debounceTimer.C
			}
			debounceTimer.Reset(repoRefDebouncePeriod)
			timerActive = true
		case <-debounceTimer.C:
			timerActive = false
			w.logger.Info("Remote ref changed, updating repo library", "repo", w.gitRepoName)
			w.triggerRepoPushEvent()
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			w.logger.Warn("fsnotify error watching remote refs", "error", err)
		}
	}
}

// triggerRepoPushEvent notifies the server that a repo's remote refs changed.
// Falls back to direct ForceUpdateRepo if the server is unreachable.
func (w *Wrapper) triggerRepoPushEvent() {
	socketFilepath := config.GetServerSocketFilepath(w.agencDirpath)
	client := server.NewClient(socketFilepath)
	if err := client.Post("/repos/"+w.gitRepoName+"/push-event", nil, nil); err != nil {
		w.logger.Warn("Server push-event failed, falling back to direct update", "repo", w.gitRepoName, "error", err)
		repoLibraryDirpath := config.GetRepoDirpath(w.agencDirpath, w.gitRepoName)
		if _, statErr := os.Stat(repoLibraryDirpath); os.IsNotExist(statErr) {
			w.logger.Error("Repo library clone not found; was it removed? Skipping update", "repo", w.gitRepoName, "expected", repoLibraryDirpath)
			return
		}
		if err := mission.ForceUpdateRepo(repoLibraryDirpath); err != nil {
			w.logger.Warn("Failed to force-update repo library", "repo", w.gitRepoName, "error", err)
		}
	}
}

// writeHeartbeat periodically updates the mission's last_heartbeat timestamp
// via the server so the system knows the wrapper is still alive.
func (w *Wrapper) writeHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.stateMu.RLock()
			lastPromptAt := w.lastUserPromptAt
			w.stateMu.RUnlock()

			var lastPromptAtStr string
			if !lastPromptAt.IsZero() {
				lastPromptAtStr = lastPromptAt.Format(time.RFC3339)
			}
			if err := w.client.Heartbeat(w.missionID, w.tmuxPaneID, lastPromptAtStr); err != nil {
				w.logger.Warn("Failed to write heartbeat", "error", err)
			}
		}
	}
}

// HeadlessConfig holds configuration for running a headless mission.
type HeadlessConfig struct {
	Timeout time.Duration // Maximum runtime before timeout (0 = no timeout)
}

const (
	headlessLogMaxSize     = 10 * 1024 * 1024 // 10MB
	headlessLogMaxBackups  = 3
	headlessShutdownPeriod = 30 * time.Second
)

// RunHeadless executes a headless mission using claude --print -p <prompt>.
// The Claude output is captured to claude-output.log with log rotation.
// If a previous conversation exists (isResume=true), it uses claude -c -p <prompt>
// to continue the conversation.
func (w *Wrapper) RunHeadless(isResume bool, cfg HeadlessConfig) error {
	// Ensure OAuth token exists before installing signal handlers.
	if err := config.SetupOAuthToken(w.agencDirpath); err != nil {
		return err
	}

	// Set up logger
	logFilepath := config.GetMissionWrapperLogFilepath(w.agencDirpath, w.missionID)
	logFile, err := os.OpenFile(logFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open wrapper log file")
	}
	defer logFile.Close()
	w.logger = slog.New(slog.NewJSONHandler(logFile, nil))

	w.logger.Info("Wrapper started",
		"mission_id", database.ShortID(w.missionID),
		"repo", w.gitRepoName,
		"is_resume", isResume,
		"headless", true,
		"timeout", cfg.Timeout.String(),
	)

	// Write wrapper PID
	pidFilepath := config.GetMissionPIDFilepath(w.agencDirpath, w.missionID)
	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write wrapper PID file")
	}
	defer func() { _ = os.Remove(pidFilepath) }()

	// Set up context with timeout if specified
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var timeoutTimer *time.Timer
	if cfg.Timeout > 0 {
		timeoutTimer = time.AfterFunc(cfg.Timeout, func() {
			w.logger.Warn("Headless mission timed out", "timeout", cfg.Timeout)
			cancel()
		})
		defer timeoutTimer.Stop()
	}

	// Catch SIGINT, SIGTERM, and SIGHUP
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	// Capture the tmux pane ID (will be empty for headless missions).
	w.tmuxPaneID = strings.TrimPrefix(os.Getenv("TMUX_PANE"), "%")

	// Write initial heartbeat and start periodic heartbeat loop
	if err := w.client.Heartbeat(w.missionID, w.tmuxPaneID, ""); err != nil {
		w.logger.Warn("Failed to write initial heartbeat", "error", err)
	}
	go w.writeHeartbeat(ctx)

	// Clone global MCP credentials into per-mission Keychain and start sync goroutines.
	w.cloneCredentials()
	defer w.writeBackCredentials()
	w.initCredentialHash()
	go w.watchCredentialUpwardSync(ctx)
	go w.watchCredentialDownwardSync(ctx)

	// Rotate log file if needed
	claudeOutputLogFilepath := config.GetMissionClaudeOutputLogFilepath(w.agencDirpath, w.missionID)
	if err := rotateLogFileIfNeeded(claudeOutputLogFilepath); err != nil {
		w.logger.Warn("Failed to rotate log file", "error", err)
	}

	// Open output log file
	outputFile, err := os.OpenFile(claudeOutputLogFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open claude output log file")
	}
	defer outputFile.Close()

	// Build and run the claude command
	cmd, err := w.buildHeadlessClaudeCmd(isResume)
	if err != nil {
		return stacktrace.Propagate(err, "failed to build headless claude command")
	}

	cmd.Stdout = outputFile
	cmd.Stderr = outputFile

	if err := cmd.Start(); err != nil {
		return stacktrace.Propagate(err, "failed to start headless claude")
	}

	w.logger.Info("Claude process started", "pid", cmd.Process.Pid)

	// Wait for completion
	claudeExited := make(chan error, 1)
	go func() {
		claudeExited <- cmd.Wait()
	}()

	select {
	case sig := <-sigCh:
		w.logger.Info("Received signal, shutting down", "signal", sig)
		if err := w.gracefulShutdownClaude(cmd); err != nil {
			w.logger.Warn("Graceful shutdown failed", "error", err)
		}
		return nil

	case <-ctx.Done():
		// Timeout or cancellation
		w.logger.Info("Context cancelled, shutting down")
		if err := w.gracefulShutdownClaude(cmd); err != nil {
			w.logger.Warn("Graceful shutdown failed", "error", err)
		}
		return stacktrace.NewError("headless mission timed out after %v", cfg.Timeout)

	case err := <-claudeExited:
		if err != nil {
			w.logger.Info("Claude process exited with error", "error", err)
			return stacktrace.Propagate(err, "claude exited with error")
		}
		w.logger.Info("Claude process completed successfully")
		return nil
	}
}

// buildHeadlessClaudeCmd constructs the command for headless execution.
// Uses claude --print -p <prompt> for new missions, or claude -c -p <prompt>
// for resuming existing conversations.
func (w *Wrapper) buildHeadlessClaudeCmd(isResume bool) (*exec.Cmd, error) {
	var args []string
	if isResume {
		// Resume with continuation flag and print mode
		args = []string{"-c", "--print", "-p", w.initialPrompt}
	} else {
		// New conversation with print mode
		args = []string{"--print", "-p", w.initialPrompt}
	}

	return mission.BuildClaudeCmd(w.agencDirpath, w.missionID, w.agentDirpath, w.defaultModel, w.claudeArgs, args)
}

// gracefulShutdownClaude attempts to gracefully shut down a Claude process.
// First sends SIGTERM, waits for the shutdown period, then sends SIGKILL if needed.
func (w *Wrapper) gracefulShutdownClaude(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Send SIGTERM
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		if err.Error() != "os: process already finished" {
			return stacktrace.Propagate(err, "failed to send SIGTERM")
		}
		return nil
	}

	// Wait for graceful shutdown
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(headlessShutdownPeriod):
		w.logger.Warn("Graceful shutdown timed out, sending SIGKILL")
		_ = cmd.Process.Kill()
		return nil
	case <-done:
		return nil
	}
}

// rotateLogFileIfNeeded rotates the log file if it exceeds the max size.
func rotateLogFileIfNeeded(logFilepath string) error {
	info, err := os.Stat(logFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if info.Size() < headlessLogMaxSize {
		return nil
	}

	// Rotate existing backups
	for i := headlessLogMaxBackups - 1; i >= 1; i-- {
		oldPath := logFilepath + "." + strconv.Itoa(i)
		newPath := logFilepath + "." + strconv.Itoa(i+1)
		if err := os.Rename(oldPath, newPath); err != nil && !os.IsNotExist(err) {
			slog.Warn("Failed to rotate log backup", "from", oldPath, "to", newPath, "error", err)
		}
	}

	// Move current log to .1
	if err := os.Rename(logFilepath, logFilepath+".1"); err != nil {
		slog.Warn("Failed to rotate current log file", "from", logFilepath, "error", err)
	}

	return nil
}
