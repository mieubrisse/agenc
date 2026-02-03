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
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

const (
	templatePollInterval       = 10 * time.Second
	globalConfigDebouncePeriod = 500 * time.Millisecond
	repoRefDebouncePeriod      = 5 * time.Second
)

// WrapperState represents the current state of the mission wrapper.
type WrapperState int

const (
	StateRunning        WrapperState = iota // Claude alive, no restart needed
	StateRestartPending                     // Config changed, waiting for idle
	StateRestarting                         // Killing Claude, about to relaunch
)

// Wrapper manages a Claude child process for a single mission, watching for
// template changes and gracefully restarting Claude when idle.
type Wrapper struct {
	agencDirpath    string
	missionID       string
	agentTemplate   string
	gitRepoName     string
	missionDirpath  string
	agentDirpath    string
	templateDirpath string

	state     WrapperState
	claudeCmd *exec.Cmd
	logger    *slog.Logger

	// Channels for internal communication between goroutines and the main loop.
	// All are buffered with capacity 1 and use non-blocking sends to avoid
	// goroutine leaks.
	configChanged      chan string   // receives new commit hash when template changes
	globalConfigChanged chan struct{} // notified when settings.json or CLAUDE.md change
	claudeStateIdle    chan struct{} // notified when claude-state becomes "idle"
	claudeExited       chan error    // receives the exit error from cmd.Wait()
}

// NewWrapper creates a new Wrapper for the given mission.
func NewWrapper(agencDirpath string, missionID string, agentTemplate string, gitRepoName string) *Wrapper {
	return &Wrapper{
		agencDirpath:    agencDirpath,
		missionID:       missionID,
		agentTemplate:   agentTemplate,
		gitRepoName:     gitRepoName,
		missionDirpath:  config.GetMissionDirpath(agencDirpath, missionID),
		agentDirpath:    config.GetMissionAgentDirpath(agencDirpath, missionID),
		templateDirpath: config.GetRepoDirpath(agencDirpath, agentTemplate),
		state:              StateRunning,
		configChanged:      make(chan string, 1),
		globalConfigChanged: make(chan struct{}, 1),
		claudeStateIdle:    make(chan struct{}, 1),
		claudeExited:       make(chan error, 1),
	}
}

// Run executes the wrapper lifecycle. For a new mission, pass the prompt and
// isResume=false. For a resume, pass an empty prompt and isResume=true.
// Run blocks until Claude exits naturally or the wrapper shuts down.
func (w *Wrapper) Run(prompt string, isResume bool) error {
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

	// Catch SIGINT and SIGTERM to prevent Go's default termination.
	// Claude is in the same process group, so terminal Ctrl-C reaches it
	// directly. We catch the signal here just to keep the wrapper alive
	// until Claude exits.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	// Start background watchers for config and state changes
	go w.watchClaudeState(ctx)
	go w.watchGlobalConfig(ctx)
	if w.agentTemplate != "" {
		go w.pollTemplateChanges(ctx)
	}
	if w.gitRepoName != "" {
		go w.watchWorkspaceRemoteRefs(ctx)
	}

	// Spawn initial Claude process
	if isResume {
		w.claudeCmd, err = mission.SpawnClaudeResume(w.agencDirpath, w.missionID, w.agentDirpath)
	} else {
		w.claudeCmd, err = mission.SpawnClaude(w.agencDirpath, w.missionID, w.agentDirpath, prompt)
	}
	if err != nil {
		return stacktrace.Propagate(err, "failed to spawn claude")
	}

	// Wait on Claude in a background goroutine
	go func() {
		w.claudeExited <- w.claudeCmd.Wait()
	}()

	// Main event loop
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
			return nil

		case <-w.claudeExited:
			if w.state == StateRestarting {
				// Expected exit from our SIGINT -- relaunch with -c
				w.logger.Info("Reloading Claude session after config change")
				w.claudeCmd, err = mission.SpawnClaudeResume(w.agencDirpath, w.missionID, w.agentDirpath)
				if err != nil {
					return stacktrace.Propagate(err, "failed to respawn claude after restart")
				}
				go func() {
					w.claudeExited <- w.claudeCmd.Wait()
				}()
				w.state = StateRunning
				continue
			}
			// Natural exit -- wrapper exits
			return nil

		case newHash := <-w.configChanged:
			// Config has changed -- rsync template and decide whether to restart
			if err := mission.RsyncTemplate(w.templateDirpath, w.agentDirpath); err != nil {
				w.logger.Warn("Failed to rsync template update", "error", err)
				continue
			}
			commitFilepath := config.GetMissionTemplateCommitFilepath(w.agencDirpath, w.missionID)
			_ = os.WriteFile(commitFilepath, []byte(newHash), 0644)

			claudeState := w.readClaudeState()
			if claudeState == "idle" {
				w.state = StateRestarting
				_ = w.claudeCmd.Process.Signal(syscall.SIGINT)
			} else {
				w.state = StateRestartPending
			}

		case <-w.globalConfigChanged:
			if w.state != StateRunning {
				continue // restart already in progress
			}
			w.logger.Info("Global Claude config changed, scheduling restart")
			claudeState := w.readClaudeState()
			if claudeState == "idle" {
				w.state = StateRestarting
				_ = w.claudeCmd.Process.Signal(syscall.SIGINT)
			} else {
				w.state = StateRestartPending
			}

		case <-w.claudeStateIdle:
			if w.state == StateRestartPending {
				w.state = StateRestarting
				_ = w.claudeCmd.Process.Signal(syscall.SIGINT)
			}
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
				if state == "idle" {
					select {
					case w.claudeStateIdle <- struct{}{}:
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

// watchGlobalConfig uses fsnotify to watch the global Claude config directory
// (~/.agenc/claude/) for changes to settings.json or CLAUDE.md. When either
// file changes, it debounces for 500ms (to coalesce the two writes the daemon
// makes in a single sync cycle) and then notifies the main loop via the
// globalConfigChanged channel.
func (w *Wrapper) watchGlobalConfig(ctx context.Context) {
	globalClaudeDirpath := config.GetGlobalClaudeDirpath(w.agencDirpath)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.logger.Warn("Failed to create fsnotify watcher for global config", "error", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(globalClaudeDirpath); err != nil {
		w.logger.Warn("Failed to watch global Claude config directory", "dir", globalClaudeDirpath, "error", err)
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
			basename := filepath.Base(event.Name)
			if basename != config.GlobalSettingsFilename && basename != config.GlobalClaudeMdFilename {
				continue
			}
			if !(event.Has(fsnotify.Write) || event.Has(fsnotify.Create)) {
				continue
			}
			// Reset (or start) the debounce timer
			if !debounceTimer.Stop() && timerActive {
				<-debounceTimer.C
			}
			debounceTimer.Reset(globalConfigDebouncePeriod)
			timerActive = true
		case <-debounceTimer.C:
			timerActive = false
			select {
			case w.globalConfigChanged <- struct{}{}:
			default:
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			w.logger.Warn("fsnotify error watching global config", "error", err)
		}
	}
}

// watchWorkspaceRemoteRefs uses fsnotify to watch the workspace repo's
// .git/refs/remotes/origin/ directory for changes to the default branch ref.
// When the ref changes (e.g. after `git push origin main`), force-updates the
// repo library clone so other missions get fresh copies.
func (w *Wrapper) watchWorkspaceRemoteRefs(ctx context.Context) {
	workspaceDirpath := config.GetMissionWorkspaceDirpath(w.agencDirpath, w.missionID)
	repoShortName := filepath.Base(w.gitRepoName)
	workspaceRepoDirpath := filepath.Join(workspaceDirpath, repoShortName)

	defaultBranch, err := mission.GetDefaultBranch(workspaceRepoDirpath)
	if err != nil {
		w.logger.Warn("Failed to determine default branch for workspace repo", "error", err)
		return
	}

	refsDirpath := filepath.Join(workspaceRepoDirpath, ".git", "refs", "remotes", "origin")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.logger.Warn("Failed to create fsnotify watcher for workspace remote refs", "error", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(refsDirpath); err != nil {
		w.logger.Warn("Failed to watch workspace remote refs directory", "dir", refsDirpath, "error", err)
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
			w.logger.Info("Workspace remote ref changed, updating repo library", "repo", w.gitRepoName)
			if err := mission.ForceUpdateRepo(repoLibraryDirpath); err != nil {
				w.logger.Warn("Failed to force-update repo library", "repo", w.gitRepoName, "error", err)
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			w.logger.Warn("fsnotify error watching workspace remote refs", "error", err)
		}
	}
}

// pollTemplateChanges polls the template repo every 10 seconds for changes
// to the main branch commit hash. When a change is detected, it sends the
// new hash on the configChanged channel.
func (w *Wrapper) pollTemplateChanges(ctx context.Context) {
	ticker := time.NewTicker(templatePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentHash, err := mission.ReadTemplateCommitHash(w.templateDirpath)
			if err != nil {
				continue
			}

			commitFilepath := config.GetMissionTemplateCommitFilepath(w.agencDirpath, w.missionID)
			storedData, err := os.ReadFile(commitFilepath)
			if err != nil {
				continue
			}
			storedHash := strings.TrimSpace(string(storedData))

			if currentHash != storedHash {
				select {
				case w.configChanged <- currentHash:
				default:
				}
			}
		}
	}
}
