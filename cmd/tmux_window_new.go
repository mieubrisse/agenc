package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var tmuxWindowNewCmd = &cobra.Command{
	Use:   newCmdStr + " -- <command> [args...]",
	Short: "Create a new window in the AgenC tmux session and run a command",
	Long: `Create a new window in the AgenC tmux session and run a command inside it.
The new window is inserted adjacent to the current window. When the command
exits, the pane closes and tmux focuses back to the pane that spawned it.

Must be run from inside the AgenC tmux session. Use -- to separate the
command from agenc flags.

Example:
  agenc tmux window new -- agenc mission new mieubrisse/agenc
  agenc tmux window new -- agenc mission new --prompt "Fix the auth bug" mieubrisse/agenc`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTmuxWindowNew,
}

func init() {
	tmuxWindowCmd.AddCommand(tmuxWindowNewCmd)
}

func runTmuxWindowNew(cmd *cobra.Command, args []string) error {
	// Cobra strips "--" and puts everything after it into args. The
	// MinimumNArgs(1) validator already ensures we have a command.

	if !isInsideAgencTmux() {
		return stacktrace.NewError("must be run inside the AgenC tmux session (AGENC_TMUX != 1)")
	}

	// Get the current pane ID from the TMUX_PANE environment variable
	currentPaneID := os.Getenv("TMUX_PANE")
	if currentPaneID == "" {
		return stacktrace.NewError("TMUX_PANE environment variable not set; are you inside a tmux session?")
	}

	// Get the current window ID via tmux
	windowIDOutput, err := exec.Command("tmux", "display-message", "-t", currentPaneID, "-p", "#{window_id}").Output()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get current window ID")
	}
	currentWindowID := strings.TrimSpace(string(windowIDOutput))

	// Build the command string for the new window. We wrap the user's command
	// in a shell snippet that always returns focus to the parent pane on exit,
	// regardless of how the command exits (success, failure, cancelled picker,
	// signal, etc.). The parent pane's window ID is looked up dynamically at
	// exit time in case it moved.
	userCommand := buildShellCommand(args)
	wrappedCommand := fmt.Sprintf(
		`%s; _pw=$(tmux display-message -t %s -p '#{window_id}' 2>/dev/null) && tmux select-window -t "$_pw" 2>/dev/null; tmux select-pane -t %s 2>/dev/null`,
		userCommand, currentPaneID, currentPaneID,
	)

	// Create a new window adjacent to the current one
	tmuxArgs := []string{
		"new-window",
		"-a",
		"-t", currentWindowID,
		"-e", agencParentPaneEnvVar + "=" + currentPaneID,
		wrappedCommand,
	}

	newWindowCmd := exec.Command("tmux", tmuxArgs...)
	newWindowCmd.Stdout = os.Stdout
	newWindowCmd.Stderr = os.Stderr

	if err := newWindowCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to create new tmux window")
	}

	return nil
}

// buildShellCommand joins command arguments into a single shell command string,
// quoting arguments that contain spaces or special characters.
func buildShellCommand(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		if strings.ContainsAny(arg, " \t\n\"'\\$`|&;(){}[]<>?*~!#") {
			// Use single quotes, escaping any existing single quotes
			quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\"'\"'") + "'"
		} else {
			quoted[i] = arg
		}
	}
	return strings.Join(quoted, " ")
}
