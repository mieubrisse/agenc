package wrapper

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/mission"
)

const (
	repoRefDebouncePeriod = 5 * time.Second
	heartbeatInterval     = 30 * time.Second
)

// wrapperState tracks the wrapper's position in the restart state machine.
type wrapperState int

const (
	stateRunning        wrapperState = iota // Claude is running normally
	stateRestartPending                     // A graceful restart was requested; waiting for Claude to become idle
	stateRestarting                         // Claude is being killed and will be respawned
)

// Wrapper manages a Claude child process for a single mission.
type Wrapper struct {
	agencDirpath   string
	missionID      string
	gitRepoName    string
	initialPrompt  string
	missionDirpath string
	agentDirpath   string
	db             *database.DB
	claudeCmd      *exec.Cmd
	logger         *slog.Logger

	// hasConversation tracks whether a Claude conversation exists that can be
	// resumed with `claude -c`. Set to true at startup for resumes, and flipped
	// to true when the user submits their first message (claude-state becomes
	// "busy"). Written from watchClaudeState goroutine, read from the main
	// event loop, so we use atomic.Bool.
	hasConversation atomic.Bool

	// Channels for internal communication between goroutines and the main loop.
	// All are buffered with capacity 1 and use non-blocking sends to avoid
	// goroutine leaks.
	claudeExited chan error // receives the exit error from cmd.Wait()

	// commandCh receives commands from the socket listener goroutine.
	commandCh chan commandWithResponse

	// claudeIdleCh fires when claude-state transitions to "idle". Used by the
	// main event loop to trigger deferred graceful restarts.
	claudeIdleCh chan struct{}

	// state tracks the wrapper's position in the restart state machine.
	state wrapperState

	// pendingRestart stores the deferred restart command when in stateRestartPending.
	pendingRestart *Command
}

// NewWrapper creates a new Wrapper for the given mission. The initialPrompt
// parameter is optional; if non-empty, it will be passed to Claude when
// starting a new conversation (not used for resumes).
func NewWrapper(agencDirpath string, missionID string, gitRepoName string, initialPrompt string, db *database.DB) *Wrapper {
	return &Wrapper{
		agencDirpath:   agencDirpath,
		missionID:      missionID,
		gitRepoName:    gitRepoName,
		initialPrompt:  initialPrompt,
		missionDirpath: config.GetMissionDirpath(agencDirpath, missionID),
		agentDirpath:   config.GetMissionAgentDirpath(agencDirpath, missionID),
		db:             db,
		claudeExited:   make(chan error, 1),
		commandCh:      make(chan commandWithResponse, 1),
		claudeIdleCh:   make(chan struct{}, 1),
		state:          stateRunning,
	}
}

// Run executes the wrapper lifecycle. For a new mission, pass isResume=false.
// For a resume, pass isResume=true. Run blocks until Claude exits naturally
// or the wrapper shuts down.
func (w *Wrapper) Run(isResume bool) error {
	// Set up logger that writes to the log file
	logFilepath := config.GetMissionWrapperLogFilepath(w.agencDirpath, w.missionID)
	logFile, err := os.OpenFile(logFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open wrapper log file")
	}
	defer logFile.Close()
	w.logger = slog.New(slog.NewTextHandler(logFile, nil))

	// Write wrapper PID
	pidFilepath := config.GetMissionPIDFilepath(w.agencDirpath, w.missionID)
	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write wrapper PID file")
	}
	defer os.Remove(pidFilepath)

	// Write initial claude-state as "idle" (Claude hasn't started processing yet)
	claudeStateFilepath := config.GetMissionClaudeStateFilepath(w.agencDirpath, w.missionID)
	if err := os.WriteFile(claudeStateFilepath, []byte("idle"), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write initial claude-state")
	}

	// Set up context for background goroutines
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Catch SIGINT, SIGTERM, and SIGHUP to prevent Go's default termination.
	// Claude is in the same process group, so terminal Ctrl-C reaches it
	// directly. We catch the signal here just to keep the wrapper alive
	// until Claude exits. SIGHUP is sent by tmux when a window or session
	// is destroyed — without handling it, deferred cleanup (PID file removal)
	// would not run.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigCh)

	// Write initial heartbeat and start periodic heartbeat loop
	if err := w.db.UpdateHeartbeat(w.missionID); err != nil {
		w.logger.Warn("Failed to write initial heartbeat", "error", err)
	}
	go w.writeHeartbeat(ctx)

	// Start background watchers for state changes
	go w.watchClaudeState(ctx)
	if w.gitRepoName != "" {
		go w.watchWorkspaceRemoteRefs(ctx)
	}

	// Start socket listener for receiving commands (restart, etc.)
	socketFilepath := config.GetMissionSocketFilepath(w.agencDirpath, w.missionID)
	go listenSocket(ctx, socketFilepath, w.commandCh, w.logger)

	// Track whether a resumable conversation exists. For resumes, one already
	// exists. For new missions, we start with false and flip to true when the
	// user submits their first message (claude-state becomes "busy").
	if isResume {
		w.hasConversation.Store(true)
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

	// Rename the tmux window to "<short_id> <repo-name>" when inside the
	// AgenC tmux session.
	w.renameWindowForTmux()

	// Clone fresh credentials from global Keychain before initial spawn
	w.cloneCredentials()

	// Spawn initial Claude process
	if isResume {
		w.claudeCmd, err = mission.SpawnClaudeResume(w.agencDirpath, w.missionID, w.agentDirpath)
	} else {
		w.claudeCmd, err = mission.SpawnClaudeWithPrompt(w.agencDirpath, w.missionID, w.agentDirpath, w.initialPrompt)
	}
	if err != nil {
		return stacktrace.Propagate(err, "failed to spawn claude")
	}

	// Wait on Claude in a background goroutine
	go func() {
		w.claudeExited <- w.claudeCmd.Wait()
	}()

	// Main event loop with three-state machine:
	//   Running → RestartPending → Restarting → Running
	for {
		select {
		case sig := <-sigCh:
			// User sent SIGINT/SIGTERM to the wrapper. Claude already got the
			// signal from the terminal (same process group). Just wait for it
			// to exit. Forward the signal in case Claude is in a different
			// process group on some platform.
			if w.claudeCmd != nil && w.claudeCmd.Process != nil {
				_ = w.claudeCmd.Process.Signal(sig)
			}
			<-w.claudeExited
			w.writeBackCredentials()
			return nil

		case cmdResp := <-w.commandCh:
			resp := w.handleCommand(cmdResp.cmd)
			cmdResp.responseCh <- resp

		case <-w.claudeIdleCh:
			// Claude became idle while a restart is pending — initiate the restart
			if w.state == stateRestartPending {
				w.logger.Info("Claude is idle, initiating deferred graceful restart",
					"reason", w.pendingRestart.Reason)
				w.state = stateRestarting
				_ = w.claudeCmd.Process.Signal(syscall.SIGINT)
			}

		case <-w.claudeExited:
			if w.state == stateRestarting {
				// Wrapper-initiated restart: write back creds, clone fresh, respawn
				w.logger.Info("Claude exited for restart, respawning")
				w.writeBackCredentials()
				w.cloneCredentials()

				// Respawn Claude: use -c if we have a conversation (graceful),
				// fresh session otherwise (hard)
				if w.hasConversation.Load() {
					w.claudeCmd, err = mission.SpawnClaudeResume(w.agencDirpath, w.missionID, w.agentDirpath)
				} else {
					w.claudeCmd, err = mission.SpawnClaudeWithPrompt(w.agencDirpath, w.missionID, w.agentDirpath, "")
				}
				if err != nil {
					w.logger.Error("Failed to respawn Claude after restart", "error", err)
					return stacktrace.Propagate(err, "failed to respawn claude after restart")
				}

				// Wait on the new Claude process
				go func() {
					w.claudeExited <- w.claudeCmd.Wait()
				}()

				w.state = stateRunning
				w.pendingRestart = nil
				w.logger.Info("Claude respawned successfully", "pid", w.claudeCmd.Process.Pid)
			} else {
				// Natural exit — wrapper exits
				w.writeBackCredentials()
				return nil
			}
		}
	}
}

// handleCommand processes a socket command and returns a Response.
func (w *Wrapper) handleCommand(cmd Command) Response {
	switch cmd.Command {
	case "restart":
		return w.handleRestartCommand(cmd)
	default:
		return Response{Status: "error", Error: "unknown command: " + cmd.Command}
	}
}

// handleRestartCommand processes a restart command. Idempotent: if a restart is
// already pending or in progress, returns ok. A hard restart overrides a pending
// graceful restart.
func (w *Wrapper) handleRestartCommand(cmd Command) Response {
	mode := cmd.Mode
	if mode == "" {
		mode = "graceful"
	}

	switch w.state {
	case stateRestarting:
		// Already restarting — caller's intent is being fulfilled
		w.logger.Info("Restart already in progress, returning ok", "requestedMode", mode)
		return Response{Status: "ok"}

	case stateRestartPending:
		if mode == "hard" {
			// Hard overrides a pending graceful: kill immediately
			w.logger.Info("Hard restart overrides pending graceful restart", "reason", cmd.Reason)
			w.state = stateRestarting
			w.pendingRestart = &cmd
			// For hard restart, don't preserve conversation
			w.hasConversation.Store(false)
			_ = w.claudeCmd.Process.Kill()
			return Response{Status: "ok"}
		}
		// Already pending graceful — idempotent
		w.logger.Info("Graceful restart already pending, returning ok")
		return Response{Status: "ok"}

	case stateRunning:
		if mode == "hard" {
			w.logger.Info("Hard restart requested, killing Claude immediately", "reason", cmd.Reason)
			w.state = stateRestarting
			w.pendingRestart = &cmd
			// For hard restart, don't preserve conversation
			w.hasConversation.Store(false)
			_ = w.claudeCmd.Process.Kill()
			return Response{Status: "ok"}
		}

		// Graceful restart: check if Claude is currently idle
		if w.readClaudeState() == "idle" {
			w.logger.Info("Claude is idle, initiating immediate graceful restart", "reason", cmd.Reason)
			w.state = stateRestarting
			w.pendingRestart = &cmd
			_ = w.claudeCmd.Process.Signal(syscall.SIGINT)
		} else {
			w.logger.Info("Claude is busy, deferring graceful restart until idle", "reason", cmd.Reason)
			w.state = stateRestartPending
			w.pendingRestart = &cmd
		}
		return Response{Status: "ok"}

	default:
		return Response{Status: "error", Error: "unexpected wrapper state"}
	}
}

// cloneCredentials clones fresh credentials from the global Keychain into
// the per-mission entry. Errors are logged as warnings but never fatal.
func (w *Wrapper) cloneCredentials() {
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	if err := claudeconfig.CloneKeychainCredentials(claudeConfigDirpath); err != nil {
		w.logger.Warn("Failed to clone Keychain credentials", "error", err)
	}
}

// writeBackCredentials merges per-mission Keychain credentials back into the
// global entry so MCP OAuth tokens propagate to future missions. Errors are
// logged as warnings but never fail the wrapper exit.
func (w *Wrapper) writeBackCredentials() {
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	if err := claudeconfig.WriteBackKeychainCredentials(claudeConfigDirpath); err != nil {
		if w.logger != nil {
			w.logger.Warn("Failed to write back Keychain credentials", "error", err)
		}
	}
}

// readClaudeState reads the current value of the claude-state file.
func (w *Wrapper) readClaudeState() string {
	claudeStateFilepath := config.GetMissionClaudeStateFilepath(w.agencDirpath, w.missionID)
	data, err := os.ReadFile(claudeStateFilepath)
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(data))
}

// watchClaudeState uses fsnotify to watch the mission root directory for
// changes to the claude-state file. When the file content becomes "idle",
// it sends on the claudeStateIdle channel.
//
// We watch the directory rather than the file directly because shell redirects
// (echo idle > file) may create a new file rather than writing in place,
// which would break a direct file watch.
func (w *Wrapper) watchClaudeState(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.logger.Warn("Failed to create fsnotify watcher", "error", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(w.missionDirpath); err != nil {
		w.logger.Warn("Failed to watch mission directory", "error", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(event.Name) != config.ClaudeStateFilename {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				state := w.readClaudeState()
				if state == "busy" {
					// User submitted a message, so a conversation now exists
					w.hasConversation.Store(true)
				} else if state == "idle" {
					// Notify the main event loop that Claude is idle.
					// Non-blocking send: if the channel is already full, the
					// main loop will pick it up on the next iteration.
					select {
					case w.claudeIdleCh <- struct{}{}:
					default:
					}
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			w.logger.Warn("fsnotify error", "error", err)
		}
	}
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
			repoLibraryDirpath := config.GetRepoDirpath(w.agencDirpath, w.gitRepoName)
			w.logger.Info("Remote ref changed, updating repo library", "repo", w.gitRepoName)
			if _, err := os.Stat(repoLibraryDirpath); os.IsNotExist(err) {
				w.logger.Error("Repo library clone not found; was it removed? Skipping update", "repo", w.gitRepoName, "expected", repoLibraryDirpath)
			} else if err := mission.ForceUpdateRepo(repoLibraryDirpath); err != nil {
				w.logger.Warn("Failed to force-update repo library", "repo", w.gitRepoName, "error", err)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			w.logger.Warn("fsnotify error watching remote refs", "error", err)
		}
	}
}

// writeHeartbeat periodically updates the mission's last_heartbeat timestamp
// in the database so the daemon knows the wrapper is still alive.
func (w *Wrapper) writeHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := w.db.UpdateHeartbeat(w.missionID); err != nil {
				w.logger.Warn("Failed to write heartbeat", "error", err)
			}
		}
	}
}

// HeadlessConfig holds configuration for running a headless mission.
type HeadlessConfig struct {
	Timeout  time.Duration // Maximum runtime before timeout (0 = no timeout)
	CronID   string        // Cron job ID that spawned this mission (optional)
	CronName string        // Cron job name (optional)
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
	// Set up logger
	logFilepath := config.GetMissionWrapperLogFilepath(w.agencDirpath, w.missionID)
	logFile, err := os.OpenFile(logFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open wrapper log file")
	}
	defer logFile.Close()
	w.logger = slog.New(slog.NewTextHandler(logFile, nil))

	w.logger.Info("Starting headless mission",
		"missionID", w.missionID,
		"timeout", cfg.Timeout,
		"cronID", cfg.CronID,
		"cronName", cfg.CronName,
		"isResume", isResume,
	)

	// Write wrapper PID
	pidFilepath := config.GetMissionPIDFilepath(w.agencDirpath, w.missionID)
	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write wrapper PID file")
	}
	defer os.Remove(pidFilepath)

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

	// Write initial heartbeat and start periodic heartbeat loop
	if err := w.db.UpdateHeartbeat(w.missionID); err != nil {
		w.logger.Warn("Failed to write initial heartbeat", "error", err)
	}
	go w.writeHeartbeat(ctx)

	// Clone fresh credentials from global Keychain before spawning Claude
	w.cloneCredentials()

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
		w.writeBackCredentials()
		return nil

	case <-ctx.Done():
		// Timeout or cancellation
		w.logger.Info("Context cancelled, shutting down")
		if err := w.gracefulShutdownClaude(cmd); err != nil {
			w.logger.Warn("Graceful shutdown failed", "error", err)
		}
		w.writeBackCredentials()
		return stacktrace.NewError("headless mission timed out after %v", cfg.Timeout)

	case err := <-claudeExited:
		if err != nil {
			w.logger.Info("Claude process exited with error", "error", err)
			w.writeBackCredentials()
			return stacktrace.Propagate(err, "claude exited with error")
		}
		w.logger.Info("Claude process completed successfully")
		w.writeBackCredentials()
		return nil
	}
}

// buildHeadlessClaudeCmd constructs the command for headless execution.
// Uses claude --print -p <prompt> for new missions, or claude -c -p <prompt>
// for resuming existing conversations.
func (w *Wrapper) buildHeadlessClaudeCmd(isResume bool) (*exec.Cmd, error) {
	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'claude' binary not found in PATH")
	}

	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)

	var args []string
	if isResume {
		// Resume with continuation flag and print mode
		args = []string{"-c", "--print", "-p", w.initialPrompt}
	} else {
		// New conversation with print mode
		args = []string{"--print", "-p", w.initialPrompt}
	}

	secretsEnvFilepath := filepath.Join(w.agentDirpath, config.UserClaudeDirname, config.SecretsEnvFilename)

	var cmd *exec.Cmd
	if _, statErr := os.Stat(secretsEnvFilepath); statErr == nil {
		// secrets.env exists — wrap with op run
		opBinary, err := exec.LookPath("op")
		if err != nil {
			return nil, stacktrace.Propagate(err, "'op' (1Password CLI) not found in PATH; required because '%s' exists", secretsEnvFilepath)
		}

		opArgs := []string{
			"run",
			"--env-file", secretsEnvFilepath,
			"--no-masking",
			"--",
			claudeBinary,
		}
		opArgs = append(opArgs, args...)
		cmd = exec.Command(opBinary, opArgs...)
	} else {
		cmd = exec.Command(claudeBinary, args...)
	}

	cmd.Dir = w.agentDirpath
	cmd.Env = append(os.Environ(),
		"CLAUDE_CONFIG_DIR="+claudeConfigDirpath,
		"AGENC_MISSION_UUID="+w.missionID,
	)

	return cmd, nil
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
		os.Rename(oldPath, newPath) // Ignore errors
	}

	// Move current log to .1
	os.Rename(logFilepath, logFilepath+".1")

	return nil
}
