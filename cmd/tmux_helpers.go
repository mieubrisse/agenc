package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
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

	// Environment variable set per-pane for return-on-exit behavior.
	agencParentPaneEnvVar = "AGENC_PARENT_PANE"

	// Minimum tmux version required (new-window -e flag).
	minTmuxMajor = 3
	minTmuxMinor = 0
)

// parseTmuxVersion extracts the major and minor version numbers from
// the output of `tmux -V` (e.g. "tmux 3.4" or "tmux 3.3a").
func parseTmuxVersion(versionStr string) (major int, minor int, err error) {
	versionStr = strings.TrimSpace(versionStr)
	// Typical format: "tmux 3.4" or "tmux 3.3a"
	parts := strings.Fields(versionStr)
	if len(parts) < 2 {
		return 0, 0, stacktrace.NewError("unexpected tmux -V output: %q", versionStr)
	}
	versionPart := parts[1]

	// Strip any trailing non-numeric characters (e.g. "3.3a" -> "3.3")
	dotIdx := strings.Index(versionPart, ".")
	if dotIdx < 0 {
		major, err = strconv.Atoi(versionPart)
		if err != nil {
			return 0, 0, stacktrace.Propagate(err, "failed to parse tmux major version from %q", versionPart)
		}
		return major, 0, nil
	}

	major, err = strconv.Atoi(versionPart[:dotIdx])
	if err != nil {
		return 0, 0, stacktrace.Propagate(err, "failed to parse tmux major version from %q", versionPart)
	}

	minorStr := versionPart[dotIdx+1:]
	// Strip trailing non-digit characters (e.g. "3a" -> "3")
	trimmed := strings.TrimRight(minorStr, "abcdefghijklmnopqrstuvwxyz")
	if trimmed == "" {
		return major, 0, nil
	}
	minor, err = strconv.Atoi(trimmed)
	if err != nil {
		return 0, 0, stacktrace.Propagate(err, "failed to parse tmux minor version from %q", minorStr)
	}

	return major, minor, nil
}

// checkTmuxVersion verifies that the installed tmux version meets the minimum
// requirement. Returns an error if tmux is not found or the version is too old.
func checkTmuxVersion() error {
	output, err := exec.Command("tmux", "-V").Output()
	if err != nil {
		return stacktrace.NewError("tmux is not installed or not in PATH; tmux >= %d.%d is required", minTmuxMajor, minTmuxMinor)
	}

	major, minor, err := parseTmuxVersion(string(output))
	if err != nil {
		return stacktrace.Propagate(err, "failed to parse tmux version")
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
