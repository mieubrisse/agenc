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
	"syscall"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/rjeczalik/notify"

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
	missionDirpath string
	agentDirpath   string
	client         *server.Client
	claudeCmd      *exec.Cmd
	logger         *slog.Logger
	tmuxPaneID     string // numeric pane ID from $TMUX_PANE (%-prefix stripped); empty for headless

	// claudeSpawnArgs is the model/config/per-mission/passthrough arg list
	// handed to every Claude spawn, already merged by mission.MergeClaudeArgs
	// at construction time. Resolved once because the merge reads config, and
	// config must not shift underneath a mission mid-restart.
	claudeSpawnArgs []string

	// unknownClaudeArgKeys names per-mission Claude args this build does not
	// recognize. Only reachable via version skew (a mission created by a newer
	// AgenC, resumed by an older one). Dropped from the spawn but logged, so a
	// mission silently missing a flag is diagnosable.
	unknownClaudeArgKeys []string

	// hasConversation tracks whether a Claude conversation exists that can be
	// resumed with `claude -c`. Set to true at startup for resumes, and flipped
	// to true when the user submits their first message (via UserPromptSubmit
	// hook). Guarded by stateMu: the startup write happens on the Run
	// goroutine after the HTTP server is already serving GET /status, so it
	// races the handler's read without the lock.
	hasConversation bool

	// claudeIdle tracks whether Claude is currently idle (waiting for user
	// input). Initialized true (Claude hasn't started processing yet). Updated
	// by handleClaudeUpdate from the main event loop.
	claudeIdle bool

	// quotaBlocked tracks whether the turn stopped because the account's usage
	// quota was exhausted, rather than because Claude finished. Claude Code
	// can wait out the reset and carry the turn on by itself, so this is not
	// idleness — a mission in this state still has work pending and must not
	// be reaped at the ordinary idle threshold.
	quotaBlocked bool

	// stateMu protects claudeIdle, hasConversation, state, needsAttention, and
	// quotaBlocked. The HTTP GET /status handler reads these fields
	// concurrently with the main event loop, so all reads and writes must hold
	// the appropriate lock.
	stateMu          sync.RWMutex
	needsAttention   bool      // true when Claude needs user attention (permission prompt etc.)
	lastUserPromptAt time.Time // zero value means no prompt yet this session

	// Channels for internal communication between goroutines and the main loop.
	// All are buffered with capacity 1 and use non-blocking sends to avoid
	// goroutine leaks.
	claudeExited chan error // receives the exit error from cmd.Wait()

	// commandCh receives commands from the HTTP server goroutine.
	commandCh chan commandWithResponse

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
//
// missionClaudeArgs holds the mission's own keyed Claude CLI overrides (from
// `agenc mission new --model` and friends) and missionClaudePassthroughArgs the
// verbatim argv it was given after `--`. Both are read from the mission record
// by the caller, which is what makes them outlive this wrapper: every spawn in
// this process, including the restarts the wrapper decides on its own, reuses
// the args resolved here, and the next wrapper reads the record again.
func NewWrapper(
	agencDirpath string,
	missionID string,
	gitRepoName string,
	initialPrompt string,
	missionClaudeArgs map[string]string,
	missionClaudePassthroughArgs []string,
) *Wrapper {
	// Load window coloring config from config.yml
	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	var titleCfg *config.TmuxWindowTitleConfig
	var defaultModel string
	var configClaudeArgs []string
	if err == nil {
		titleCfg = cfg.GetTmuxWindowTitleConfig()
		defaultModel = cfg.GetDefaultModel(gitRepoName)
		configClaudeArgs = cfg.GetClaudeArgs(gitRepoName)
	} else {
		titleCfg = &config.TmuxWindowTitleConfig{}
	}

	return &Wrapper{
		agencDirpath:  agencDirpath,
		missionID:     missionID,
		gitRepoName:   gitRepoName,
		initialPrompt: initialPrompt,
		claudeSpawnArgs: mission.MergeClaudeArgs(mission.ClaudeArgSources{
			ResolvedModel:         defaultModel,
			ConfigClaudeArgs:      configClaudeArgs,
			MissionClaudeArgs:     missionClaudeArgs,
			ClaudePassthroughArgs: missionClaudePassthroughArgs,
		}),
		unknownClaudeArgKeys:           mission.UnknownClaudeArgKeys(missionClaudeArgs),
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

// runResources bundles resources created during setup that the caller must
// clean up (via deferred calls) and that the event loop needs.
type runResources struct {
	logFile *os.File
	cancel  context.CancelFunc
	sigCh   chan os.Signal
}

// setupRun performs all one-time initialization for the wrapper lifecycle:
// logging, PID file, signal handling, background goroutines, and initial Claude
// spawn. It returns the resources needed by the event loop and registers
// deferred cleanup on the caller's behalf via the returned cleanup function.
func (w *Wrapper) setupRun(isResume bool) (*runResources, func(), error) {
	// Verify authentication before spawning Claude. EnsureClaudeAuth is
	// conditional: it returns nil if a token is configured (State-X fallback) or
	// if no token is configured (native auth — Claude will surface its own error
	// if credentials are missing). See config.EnsureClaudeAuth for rationale.
	if err := config.EnsureClaudeAuth(w.agencDirpath); err != nil {
		return nil, nil, stacktrace.Propagate(err, "authentication check failed before spawn")
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
	if len(w.unknownClaudeArgKeys) > 0 {
		w.logger.Warn("Dropping unrecognized per-mission Claude args; this AgenC build does not support them",
			"mission_id", database.ShortID(w.missionID),
			"unknown_claude_arg_keys", w.unknownClaudeArgKeys,
		)
	}

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

	// Track whether a resumable conversation exists. For resumes, one already
	// exists. For new missions, we start with false and flip to true when the
	// first UserPromptSubmit hook fires.
	//
	// Locked because startHTTPServer went live immediately above, and its
	// GET /status handler reads this field from another goroutine.
	if isResume {
		w.stateMu.Lock()
		w.hasConversation = true
		w.stateMu.Unlock()
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
// Before every spawn the per-mission operational-settings file (delivered to
// Claude via `claude --settings`) is (re)written so each reload regenerates it
// with the latest AgenC operational overlay after a config change. Under State
// Y there is no per-mission claude-config snapshot to rebuild — Claude reads the
// user's real ~/.claude directly.
func (w *Wrapper) spawnClaude(isResume bool) error {
	if err := claudeconfig.WriteMissionOpSettings(w.agencDirpath, w.missionID); err != nil {
		return stacktrace.Propagate(err, "failed to write operational settings before spawn")
	}
	// A spawn carrying a prompt submits it immediately, so Claude is working
	// from the moment it boots. Record that now instead of waiting for the
	// UserPromptSubmit hook: claudeIdle starts true, so until that hook lands
	// the wrapper would otherwise report "idle" while Claude is in fact
	// starting a turn — and the server's idle-timeout loop believes it.
	if w.initialPrompt != "" {
		w.stateMu.Lock()
		w.claudeIdle = false
		w.stateMu.Unlock()
	}
	return w.spawnClaudeDirectly(isResume)
}

// spawnClaudeDirectly spawns Claude as a local process.
func (w *Wrapper) spawnClaudeDirectly(isResume bool) error {
	var cmd *exec.Cmd
	var err error

	if isResume {
		sessionID := claudeconfig.GetLastSessionID(w.agencDirpath, w.missionID)
		if sessionID != "" && claudeconfig.ProjectDirectoryExists(w.agentDirpath) {
			cmd, err = mission.SpawnClaudeResumeWithSession(w.agencDirpath, w.missionID, w.agentDirpath, w.claudeSpawnArgs, sessionID, w.initialPrompt)
		} else {
			cmd, err = mission.SpawnClaudeWithPrompt(w.agencDirpath, w.missionID, w.agentDirpath, w.claudeSpawnArgs, w.initialPrompt)
		}
	} else {
		cmd, err = mission.SpawnClaudeWithPrompt(w.agencDirpath, w.missionID, w.agentDirpath, w.claudeSpawnArgs, w.initialPrompt)
	}

	if err != nil {
		return stacktrace.Propagate(err, "failed to spawn claude process")
	}
	w.claudeCmd = cmd
	return nil
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
	default:
		return CommandResponse{Status: "error", Error: "unknown command: " + cmd.Command}
	}
}

// claudeQuotaErrorKind is Claude Code's StopFailure `error` value for a turn
// stopped by the account's usage quota. It is the one error kind that does not
// mean the turn is over: Claude Code can wait for the quota window to reset
// and continue by itself.
const claudeQuotaErrorKind = "rate_limit"

// Notification `notification_type` values Claude Code emits about a quota
// wait. Together they are the authoritative end of the wait, which is why the
// quota state does not have to be guessed at from timing:
//
//	resumed — the window reset and Claude Code picked the turn back up.
//	stale   — "Usage limit has reset · press enter to continue". The wait is
//	          over but a human has to act, so the turn is not resuming.
//	abandoned — "this task will not resume on its own", emitted when
//	          auto-continue is off, when the reset is more than 24 hours out,
//	          or after repeated usage-limit hits.
//
// The last two are Claude Code saying the turn is finished. Without them a
// mission would sit spared until the 6-hour ceiling, long after Claude Code
// announced that recovery was not coming.
const (
	claudeQuotaResumedNotificationType   = "quota_auto_resume_fired"
	claudeQuotaStaleNotificationType     = "quota_auto_resume_stale"
	claudeQuotaAbandonedNotificationType = "quota_auto_resume_disabled"
)

// handleClaudeUpdate processes a claude_update command sent by hooks. It
// updates the wrapper's idle state, hasConversation flag, needsAttention flag,
// and sets tmux pane colors for visual feedback.
func (w *Wrapper) handleClaudeUpdate(cmd Command) CommandResponse {
	w.logger.Info("Received claude_update", "event", cmd.Event, "notification_type", cmd.NotificationType)

	w.stateMu.Lock()
	defer w.stateMu.Unlock()

	switch cmd.Event {
	case "Stop":
		w.markTurnEndedLocked()

	case "StopFailure":
		// A turn that died on an API error. Claude Code fires this INSTEAD OF
		// Stop, so without this case the wrapper never learns the turn ended
		// and reports "busy" until the process dies.
		//
		// It also fires when a SUBAGENT's turn dies, while the main agent
		// carries on working — acting on that would mark a working mission
		// idle, which is the bug KEV-70 just fixed. Two conditions must both
		// hold before we believe the main agent is finished:
		//
		//   PayloadParsed — we actually read the hook payload. A timed-out or
		//     malformed read leaves AgentID empty for a reason that has
		//     nothing to do with who failed, so it must not be mistaken for
		//     evidence about the main agent.
		//   AgentID == "" — Claude Code's agent_id is "present only when the
		//     hook fires from within a subagent […] absent for the main
		//     thread", and its schema says to use this field, not agent_type.
		//
		// Failing either one leaves the state untouched: the mission stays
		// wedged until the server's hard ceiling reclaims it, which is exactly
		// today's behavior and strictly better than killing live work.
		//
		// A turn stopped on a usage quota is NOT a finished turn, which is why
		// the error kind is read here rather than only logged. Claude Code can
		// wait out the reset and resume by itself: one such pause on a real
		// mission lasted 70 minutes before `quota_auto_resume_fired` and the
		// work completed. Reporting idle would hand that mission to the reaper
		// at the 30-minute threshold, 40 minutes before the reset that would
		// have saved it — losing unattended work, which is the KEV-70 harm
		// this change exists to avoid rather than relocate.
		//
		// Safe whichever way Claude Code behaves, which matters because the
		// auto-resume path could not be observed here (it needs subscription
		// auth; this was verified against an API key):
		//   - if StopFailure fires and Claude Code THEN waits, the mission is
		//     spared and the resume lands;
		//   - if StopFailure fires INSTEAD of a wait — the observed 2.1.274
		//     behavior, where ten retries are exhausted first and the session
		//     returns to the prompt — the mission is spared until the hard
		//     ceiling, which is exactly what it did before this change.
		// Either way no mission is reaped earlier than it is today.
		turnEndedForMainAgent := cmd.PayloadParsed && cmd.AgentID == ""
		quotaBlocked := turnEndedForMainAgent && cmd.HookError == claudeQuotaErrorKind
		w.logger.Info(
			"Claude turn ended in an API error",
			"error_kind", cmd.HookError,
			"agent_id", cmd.AgentID,
			"payload_parsed", cmd.PayloadParsed,
			"treated_as_idle", turnEndedForMainAgent && !quotaBlocked,
			"quota_blocked", quotaBlocked,
		)
		if !turnEndedForMainAgent {
			break
		}
		if quotaBlocked {
			w.quotaBlocked = true
			w.needsAttention = false
			break
		}
		w.markTurnEndedLocked()

	case "UserPromptSubmit":
		w.claudeIdle = false
		w.hasConversation = true
		w.needsAttention = false
		w.quotaBlocked = false
		w.lastUserPromptAt = time.Now().UTC()
		w.setWindowBusy()
		if err := w.client.RecordPrompt(w.missionID); err != nil {
			w.logger.Warn("Failed to record prompt", "error", err)
		}

	case "PostToolUse", "PostToolUseFailure":
		// A tool just completed (or failed) — Claude is still actively working,
		// so reset the window to busy in case a permission prompt turned it orange.
		// A running tool also proves any quota wait is over, which is the
		// backstop for a resume that arrives without its notification.
		w.needsAttention = false
		w.quotaBlocked = false
		w.setWindowBusy()

	case "Notification":
		switch cmd.NotificationType {
		case "permission_prompt", "elicitation_dialog":
			// Color the pane for notification types that need user attention
			w.needsAttention = true
			w.setWindowNeedsAttention()
		case claudeQuotaResumedNotificationType:
			// The quota window reset and Claude Code picked the turn back up.
			// Guarded so a stray notification cannot repaint the pane of a
			// mission that was never blocked — which would overwrite the
			// attention color while the status API still says needs_attention.
			if w.quotaBlocked {
				w.quotaBlocked = false
				w.setWindowBusy()
			}
		case claudeQuotaStaleNotificationType, claudeQuotaAbandonedNotificationType:
			// Claude Code has given up on resuming by itself. The turn really
			// is over, so stop sparing the mission and let it be reclaimed on
			// the ordinary idle path rather than held to the hard ceiling.
			if w.quotaBlocked {
				w.markTurnEndedLocked()
			}
		}
	}

	return CommandResponse{Status: "ok"}
}

// markTurnEndedLocked records that Claude's turn is over and it is back at the
// prompt. Shared by every arm of handleClaudeUpdate that ends a turn — Stop, a
// non-quota StopFailure, and a quota wait Claude Code has abandoned — so they
// cannot drift: a turn that died on an API error leaves Claude exactly as idle
// as one that finished normally, and the wrapper must say so either way.
//
// The exception is a turn stopped on the usage quota, which is NOT routed here
// while the wait is live, because Claude Code may still resume it.
//
// needsAttention going false is load-bearing, not incidental. The server's
// reaper spares a mission reporting needs_attention just as it spares one
// reporting busy, so leaving it set here would trade one flavor of
// never-reclaimed mission for another.
//
// Must be called with stateMu held.
func (w *Wrapper) markTurnEndedLocked() {
	w.claudeIdle = true
	w.hasConversation = true
	w.needsAttention = false
	w.quotaBlocked = false
	w.resetWindowTabStyle()
	// Notify the server so any async-queued reload can fire now.
	// Best-effort: errors are logged, not propagated — a missed
	// notification means the pending reload waits for the next turn to end.
	go func() {
		if err := w.client.NotifyClaudeIdle(w.missionID); err != nil {
			w.logger.Warn("Failed to notify server of claude-idle", "error", err)
		}
	}()
}

// getClaudeStateString returns the Claude state as a string for the status API.
// Must be called with stateMu held (at least RLock).
func (w *Wrapper) getClaudeStateString() string {
	if w.needsAttention {
		return "needs_attention"
	}
	// Ahead of idle: a quota wait is a turn that has not finished. Ahead of
	// busy too, so the reason a mission is making no progress is visible in
	// `agenc mission ls` rather than hidden behind an ordinary "busy".
	if w.quotaBlocked {
		return "quota_blocked"
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

// watchWorkspaceRemoteRefs watches the mission repo's
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

	eventCh := make(chan notify.EventInfo, 256)
	if err := notify.Watch(refsDirpath, eventCh, notify.Create|notify.Write); err != nil {
		w.logger.Warn("Failed to watch remote refs directory", "dir", refsDirpath, "error", err)
		return
	}
	defer notify.Stop(eventCh)

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
		case event := <-eventCh:
			if filepath.Base(event.Path()) != defaultBranch {
				continue
			}
			if event.Event()&(notify.Create|notify.Write) == 0 {
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
	// Verify authentication before spawning Claude. EnsureClaudeAuth is
	// conditional: it returns nil if a token is configured (State-X fallback) or
	// if no token is configured (native auth — Claude will surface its own error
	// if credentials are missing). See config.EnsureClaudeAuth for rationale.
	if err := config.EnsureClaudeAuth(w.agencDirpath); err != nil {
		return stacktrace.Propagate(err, "authentication check failed before headless spawn")
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
	// Write the operational-settings file before building the command, since
	// BuildClaudeCmd unconditionally passes `--settings <op-settings file>`
	// under State Y — the file must exist for every spawn shape, headless
	// included, or Claude errors on an absent settings path.
	if err := claudeconfig.WriteMissionOpSettings(w.agencDirpath, w.missionID); err != nil {
		return nil, stacktrace.Propagate(err, "failed to write operational settings for headless spawn")
	}

	var args []string
	if isResume {
		// Resume with continuation flag and print mode
		args = []string{"-c", "--print", "-p", w.initialPrompt}
	} else {
		// New conversation with print mode
		args = []string{"--print", "-p", w.initialPrompt}
	}

	return mission.BuildClaudeCmd(w.agencDirpath, w.missionID, w.agentDirpath, w.claudeSpawnArgs, args)
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
