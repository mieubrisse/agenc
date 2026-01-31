package wrapper

import (
	"context"
	"io"
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
	templatePollInterval = 10 * time.Second
)

// WrapperState represents the current state of the mission wrapper.
type WrapperState int

const (
	StateRunning        WrapperState = iota // Claude alive, no restart needed
	StateRestartPending                     // Template changed, waiting for idle
	StateRestarting                         // Killing Claude, about to relaunch
)

// Wrapper manages a Claude child process for a single mission, watching for
// template changes and gracefully restarting Claude when idle.
type Wrapper struct {
	agencDirpath    string
	missionID       string
	agentTemplate   string
	embeddedAgent   bool
	missionDirpath  string
	agentDirpath    string
	templateDirpath string

	state     WrapperState
	claudeCmd *exec.Cmd
	logger    *slog.Logger

	// Channels for internal communication between goroutines and the main loop.
	// All are buffered with capacity 1 and use non-blocking sends to avoid
	// goroutine leaks.
	configChanged   chan string   // receives new commit hash (template) or "" (embedded) when config changes
	claudeStateIdle chan struct{} // notified when claude-state becomes "idle"
	claudeExited    chan error    // receives the exit error from cmd.Wait()
}

// NewWrapper creates a new Wrapper for the given mission.
func NewWrapper(agencDirpath string, missionID string, agentTemplate string, embeddedAgent bool) *Wrapper {
	return &Wrapper{
		agencDirpath:    agencDirpath,
		missionID:       missionID,
		agentTemplate:   agentTemplate,
		embeddedAgent:   embeddedAgent,
		missionDirpath:  config.GetMissionDirpath(agencDirpath, missionID),
		agentDirpath:    config.GetMissionAgentDirpath(agencDirpath, missionID),
		templateDirpath: config.GetRepoDirpath(agencDirpath, agentTemplate),
		state:           StateRunning,
		configChanged:   make(chan string, 1),
		claudeStateIdle: make(chan struct{}, 1),
		claudeExited:    make(chan error, 1),
	}
}

// Run executes the wrapper lifecycle. For a new mission, pass the prompt and
// isResume=false. For a resume, pass an empty prompt and isResume=true.
// Run blocks until Claude exits naturally or the wrapper shuts down.
func (w *Wrapper) Run(prompt string, isResume bool) error {
	// Set up logger that writes to both stdout and a log file
	logFilepath := config.GetMissionWrapperLogFilepath(w.agencDirpath, w.missionID)
	logFile, err := os.OpenFile(logFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open wrapper log file")
	}
	defer logFile.Close()
	w.logger = slog.New(slog.NewTextHandler(io.MultiWriter(os.Stdout, logFile), nil))

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

	// Start background watchers for config changes
	if w.agentTemplate != "" {
		go w.watchClaudeState(ctx)
		go w.pollTemplateChanges(ctx)
	} else if w.embeddedAgent {
		go w.watchClaudeState(ctx)
		go w.watchEmbeddedConfig(ctx)
	}

	// Spawn initial Claude process
	if isResume {
		w.claudeCmd, err = mission.SpawnClaudeResume(w.agencDirpath, w.agentDirpath)
	} else {
		w.claudeCmd, err = mission.SpawnClaude(w.agencDirpath, w.agentDirpath, prompt)
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
				w.claudeCmd, err = mission.SpawnClaudeResume(w.agencDirpath, w.agentDirpath)
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
			// Config has changed -- rsync template (if applicable) and decide whether to restart
			if !w.embeddedAgent {
				if err := mission.RsyncTemplate(w.templateDirpath, w.agentDirpath); err != nil {
					w.logger.Warn("Failed to rsync template update", "error", err)
					continue
				}
				commitFilepath := config.GetMissionTemplateCommitFilepath(w.agencDirpath, w.missionID)
				_ = os.WriteFile(commitFilepath, []byte(newHash), 0644)
			}

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

// watchEmbeddedConfig uses fsnotify to watch for changes to Claude config
// files within the agent directory (the worktree root). Watched paths:
//   - CLAUDE.md and .mcp.json at the agent root
//   - .claude/ directory recursively (excluding settings.local.json)
//
// After the first qualifying event, waits 500ms for additional events before
// sending a single signal on configChanged.
func (w *Wrapper) watchEmbeddedConfig(ctx context.Context) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		w.logger.Warn("Failed to create fsnotify watcher for embedded config", "error", err)
		return
	}
	defer watcher.Close()

	// Watch the agent directory for CLAUDE.md and .mcp.json
	if err := watcher.Add(w.agentDirpath); err != nil {
		w.logger.Warn("Failed to watch agent directory", "error", err)
		return
	}

	// Watch .claude/ directory and all its subdirectories
	claudeDirpath := filepath.Join(w.agentDirpath, config.UserClaudeDirname)
	if err := addDirRecursive(watcher, claudeDirpath); err != nil {
		w.logger.Warn("Failed to watch .claude directory", "error", err)
		// Continue anyway — the root watch still works
	}

	const debounceDelay = 500 * time.Millisecond
	var debounceTimer *time.Timer

	for {
		select {
		case <-ctx.Done():
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			return

		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Dynamically watch new subdirectories under .claude/
			if event.Has(fsnotify.Create) {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					claudeDirpath := filepath.Join(w.agentDirpath, config.UserClaudeDirname)
					if relPath, relErr := filepath.Rel(claudeDirpath, event.Name); relErr == nil && !strings.HasPrefix(relPath, "..") {
						_ = watcher.Add(event.Name)
					}
					continue
				}
			}

			if !isEmbeddedConfigEvent(w.agentDirpath, event) {
				continue
			}

			// Debounce: reset timer on each qualifying event
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounceDelay, func() {
				select {
				case w.configChanged <- "":
				default:
				}
			})

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			w.logger.Warn("fsnotify error (embedded config)", "error", err)
		}
	}
}

// isEmbeddedConfigEvent returns true if the fsnotify event corresponds to a
// Claude config file that should trigger a reload. Directory events are
// handled by the caller before this function is called.
func isEmbeddedConfigEvent(agentDirpath string, event fsnotify.Event) bool {
	name := filepath.Base(event.Name)

	// Root-level files: CLAUDE.md and .mcp.json
	if filepath.Dir(event.Name) == agentDirpath {
		return name == "CLAUDE.md" || name == ".mcp.json"
	}

	// Files under .claude/ (excluding settings.local.json)
	claudeDirpath := filepath.Join(agentDirpath, config.UserClaudeDirname)
	relPath, err := filepath.Rel(claudeDirpath, event.Name)
	if err != nil || strings.HasPrefix(relPath, "..") {
		return false
	}
	if name == config.SettingsLocalFilename {
		return false
	}
	return true
}

// addDirRecursive adds the given directory and all its subdirectories to the
// fsnotify watcher. Silently skips directories that don't exist.
func addDirRecursive(watcher *fsnotify.Watcher, dirpath string) error {
	return filepath.WalkDir(dirpath, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return watcher.Add(path)
		}
		return nil
	})
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
