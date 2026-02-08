package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/mieubrisse/stacktrace"
	agentmux "github.com/odyssey/agenc/internal/tmux"
)

const tmuxDebugLogFilepath = "/tmp/agenc-tmux.log"

// tmuxDebugLog appends a timestamped line to /tmp/agenc-tmux.log for diagnosing
// keybinding issues. Temporary — remove after debugging.
func tmuxDebugLog(format string, args ...any) {
	f, err := os.OpenFile(tmuxDebugLogFilepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "[%s] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), msg)
}

const (
	tmuxSessionName = "agenc"

	// Environment variables set on the tmux session.
	agencTmuxEnvVar    = "AGENC_TMUX"
	agencDirpathEnvVar = "AGENC_DIRPATH"

	// Minimum tmux version required (new-window -e flag).
	minTmuxMajor = 3
	minTmuxMinor = 0
)

// checkTmuxVersion verifies that the installed tmux version meets the minimum
// requirement. Returns an error if tmux is not found or the version is too old.
func checkTmuxVersion() error {
	major, minor, err := agentmux.DetectVersion()
	if err != nil {
		return stacktrace.NewError("tmux is not installed or not in PATH; tmux >= %d.%d is required", minTmuxMajor, minTmuxMinor)
	}

	if major < minTmuxMajor || (major == minTmuxMajor && minor < minTmuxMinor) {
		return stacktrace.NewError("tmux %d.%d found but >= %d.%d is required (needed for new-window -e flag)", major, minor, minTmuxMajor, minTmuxMinor)
	}

	return nil
}

// tmuxSessionExists checks whether the named tmux session exists.
func tmuxSessionExists(sessionName string) bool {
	err := exec.Command("tmux", "has-session", "-t", sessionName).Run()
	return err == nil
}

// resolveAgencBinaryPath returns the absolute path to the currently running
// agenc binary via os.Executable(). Used to ensure tmux windows can invoke
// agenc regardless of PATH differences inside tmux.
func resolveAgencBinaryPath() (string, error) {
	binaryPath, err := os.Executable()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to resolve agenc binary path")
	}
	return binaryPath, nil
}

// printInsideSessionError prints a message when the user tries to attach
// from inside the agenc session.
func printInsideSessionError() {
	fmt.Println("Already inside the agenc tmux session. Use standard tmux commands to navigate.")
}
