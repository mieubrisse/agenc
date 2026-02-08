package cmd

import (
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var tmuxAttachCmd = &cobra.Command{
	Use:   attachCmdStr,
	Short: "Attach to the AgenC tmux session, creating it if needed",
	Long: `Attach to the AgenC tmux session. If the session doesn't exist, it is
created with 'agenc mission new' as the initial command.

The session sets AGENC_TMUX=1 and propagates AGENC_DIRPATH so all windows
share the same agenc configuration.`,
	Args: cobra.NoArgs,
	RunE: runTmuxAttach,
}

func init() {
	tmuxCmd.AddCommand(tmuxAttachCmd)
}

func runTmuxAttach(cmd *cobra.Command, args []string) error {
	// Prevent nested attach
	if os.Getenv(agencTmuxEnvVar) == "1" {
		printInsideSessionError()
		return nil
	}

	if err := checkTmuxVersion(); err != nil {
		return err
	}

	agencBinaryPath, err := resolveAgencBinaryPath()
	if err != nil {
		return err
	}

	if !tmuxSessionExists(tmuxSessionName) {
		if err := createTmuxSession(agencBinaryPath); err != nil {
			return err
		}
	}

	// Attach to the session. If the session was destroyed (e.g. user cancelled
	// the picker before we attached), exit cleanly.
	attachCmd := exec.Command("tmux", "attach-session", "-t", tmuxSessionName)
	attachCmd.Stdin = os.Stdin
	attachCmd.Stdout = os.Stdout
	attachCmd.Stderr = os.Stderr

	err = attachCmd.Run()
	if err != nil {
		// "session not found" means the initial command exited before we attached.
		// This is expected when the user cancels the repo picker quickly.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			if !tmuxSessionExists(tmuxSessionName) {
				return nil
			}
		}
		return stacktrace.Propagate(err, "failed to attach to tmux session")
	}

	return nil
}

// createTmuxSession creates the agenc tmux session with 'agenc mission new'
// as the initial command.
func createTmuxSession(agencBinaryPath string) error {
	// Build the initial command with inline env vars. tmux runs the command
	// through a shell, so VAR=val syntax works. We must embed the env vars in
	// the command string because set-environment only affects windows created
	// AFTER it's called — the initial window created by new-session wouldn't
	// inherit them.
	initialCmd := agencTmuxEnvVar + "=1"
	dirpathValue := os.Getenv(agencDirpathEnvVar)
	if dirpathValue != "" {
		initialCmd += " " + agencDirpathEnvVar + "=" + shellQuote(dirpathValue)
	}
	initialCmd += " " + agencBinaryPath + " " + missionCmdStr + " " + newCmdStr

	newSessionCmd := exec.Command("tmux",
		"new-session",
		"-d",
		"-s", tmuxSessionName,
		initialCmd,
	)
	if err := newSessionCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to create tmux session")
	}

	// Set session environment variables so that subsequent windows (created via
	// tmux new-window) also inherit them.
	if err := setTmuxSessionEnv(agencTmuxEnvVar, "1"); err != nil {
		return err
	}
	if dirpathValue != "" {
		if err := setTmuxSessionEnv(agencDirpathEnvVar, dirpathValue); err != nil {
			return err
		}
	}

	return nil
}

// shellQuote wraps a string in single quotes for safe use in a shell command,
// escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

// setTmuxSessionEnv sets an environment variable on the agenc tmux session.
func setTmuxSessionEnv(key string, value string) error {
	err := exec.Command("tmux", "set-environment", "-t", tmuxSessionName, key, value).Run()
	if err != nil {
		return stacktrace.Propagate(err, "failed to set tmux session environment variable %s", key)
	}
	return nil
}

// isInsideAgencTmux returns true if the current process is running inside
// the AgenC tmux session.
func isInsideAgencTmux() bool {
	return strings.TrimSpace(os.Getenv(agencTmuxEnvVar)) == "1"
}
