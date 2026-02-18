package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/mieubrisse/stacktrace"
	agentmux "github.com/odyssey/agenc/internal/tmux"
)

const (
	tmuxSessionName = "agenc"

	// Environment variable propagated to tmux sessions so all windows share the same agenc configuration.
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
	fmt.Println("Already inside a tmux session. Use standard tmux commands to navigate.")
}

// isInsideTmux returns true if the current process is running inside any
// tmux session (i.e. the $TMUX environment variable is set).
func isInsideTmux() bool {
	return os.Getenv("TMUX") != ""
}
