package wrapper

import (
	"context"
	"fmt"
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
	missionDirpath  string
	agentDirpath    string
	templateDirpath string

	state     WrapperState
	claudeCmd *exec.Cmd

	// Channels for internal communication between goroutines and the main loop.
	// All are buffered with capacity 1 and use non-blocking sends to avoid
	// goroutine leaks.
	templateChanged chan string   // receives new commit hash when template changes
	claudeStateIdle chan struct{} // notified when claude-state becomes "idle"
	claudeExited    chan error    // receives the exit error from cmd.Wait()
}

// NewWrapper creates a new Wrapper for the given mission.
func NewWrapper(agencDirpath string, missionID string, agentTemplate string) *Wrapper {
	return &Wrapper{
		agencDirpath:    agencDirpath,
		missionID:       missionID,
		agentTemplate:   agentTemplate,
		missionDirpath:  config.GetMissionDirpath(agencDirpath, missionID),
		agentDirpath:    config.GetMissionAgentDirpath(agencDirpath, missionID),
		templateDirpath: config.GetAgentTemplateDirpath(agencDirpath, agentTemplate),
		state:           StateRunning,
		templateChanged: make(chan string, 1),
		claudeStateIdle: make(chan struct{}, 1),
		claudeExited:    make(chan error, 1),
	}
}

// Run executes the wrapper lifecycle. For a new mission, pass the prompt and
// isResume=false. For a resume, pass an empty prompt and isResume=true.
// Run blocks until Claude exits naturally or the wrapper shuts down.
func (w *Wrapper) Run(prompt string, isResume bool) error {
	// Write wrapper PID
	pidFilepath := config.GetMissionPIDFilepath(w.agencDirpath, w.missionID)
	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write wrapper PID file")
	}
	defer os.Remove(pidFilepath)

	// Write initial claude-state as "busy"
	claudeStateFilepath := config.GetMissionClaudeStateFilepath(w.agencDirpath, w.missionID)
	if err := os.WriteFile(claudeStateFilepath, []byte("busy"), 0644); err != nil {
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

	// Start background watchers if we have a template to watch
	if w.agentTemplate != "" {
		go w.watchClaudeState(ctx)
		go w.pollTemplateChanges(ctx)
	}

	// Spawn initial Claude process
	var err error
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

		case newHash := <-w.templateChanged:
			// Template has changed -- rsync and decide whether to restart
			if err := mission.RsyncTemplate(w.templateDirpath, w.agentDirpath); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to rsync template update: %v\n", err)
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
		fmt.Fprintf(os.Stderr, "Warning: failed to create fsnotify watcher: %v\n", err)
		return
	}
	defer watcher.Close()

	if err := watcher.Add(w.missionDirpath); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to watch mission directory: %v\n", err)
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
			fmt.Fprintf(os.Stderr, "Warning: fsnotify error: %v\n", err)
		}
	}
}

// pollTemplateChanges polls the template repo every 10 seconds for changes
// to the main branch commit hash. When a change is detected, it sends the
// new hash on the templateChanged channel.
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
				case w.templateChanged <- currentHash:
				default:
				}
			}
		}
	}
}
