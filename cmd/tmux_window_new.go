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

The --parent-pane flag allows specifying the parent pane explicitly, which
is needed for tmux keybindings where $TMUX_PANE is not available. Use
#{pane_id} in tmux config to pass the current pane.

Example:
  agenc tmux window new -- agenc mission new mieubrisse/agenc
  agenc tmux window new --parent-pane "#{pane_id}" -- agenc mission new`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTmuxWindowNew,
}

func init() {
	tmuxWindowCmd.AddCommand(tmuxWindowNewCmd)
	tmuxWindowNewCmd.Flags().String(parentPaneFlagName, "", "Parent pane ID to return focus to on exit (defaults to $TMUX_PANE)")
}

func runTmuxWindowNew(cmd *cobra.Command, args []string) error {
	tmuxDebugLog("=== tmux window new ===")
	tmuxDebugLog("args: %v", args)
	tmuxDebugLog("AGENC_TMUX=%q", os.Getenv(agencTmuxEnvVar))
	tmuxDebugLog("TMUX_PANE=%q", os.Getenv("TMUX_PANE"))
	tmuxDebugLog("PATH=%q", os.Getenv("PATH"))

	if !isInsideAgencTmux() {
		tmuxDebugLog("FAIL: isInsideAgencTmux() returned false")
		return stacktrace.NewError("must be run inside the AgenC tmux session (AGENC_TMUX != 1)")
	}

	// Determine the parent pane: explicit flag takes priority over $TMUX_PANE.
	// The flag is needed for tmux keybindings where $TMUX_PANE isn't available
	// but #{pane_id} can be expanded by tmux.
	parentPaneID, _ := cmd.Flags().GetString(parentPaneFlagName)
	if parentPaneID == "" {
		parentPaneID = os.Getenv("TMUX_PANE")
	}
	tmuxDebugLog("parentPaneID=%q", parentPaneID)
	if parentPaneID == "" {
		tmuxDebugLog("FAIL: empty parentPaneID")
		return stacktrace.NewError("could not determine parent pane; pass --parent-pane or ensure $TMUX_PANE is set")
	}

	// Get the parent's window ID and current working directory via tmux
	windowIDOutput, err := exec.Command("tmux", "display-message", "-t", parentPaneID, "-p", "#{window_id}").Output()
	if err != nil {
		tmuxDebugLog("FAIL: display-message for pane %s: %v", parentPaneID, err)
		return stacktrace.Propagate(err, "failed to get window ID for pane %s", parentPaneID)
	}
	parentWindowID := strings.TrimSpace(string(windowIDOutput))
	tmuxDebugLog("parentWindowID=%q", parentWindowID)

	parentDirpath := getParentPaneDirpath(parentPaneID)
	tmuxDebugLog("parentDirpath=%q", parentDirpath)

	// Build the command string for the new window. We wrap the user's command
	// in a shell snippet that always returns focus to the parent pane on exit,
	// regardless of how the command exits (success, failure, cancelled picker,
	// signal, etc.). The parent pane's window ID is looked up dynamically at
	// exit time in case it moved.
	userCommand := buildShellCommand(args)
	wrappedCommand := fmt.Sprintf(
		`%s; _pw=$(tmux display-message -t %s -p '#{window_id}' 2>/dev/null) && tmux select-window -t "$_pw" 2>/dev/null; tmux select-pane -t %s 2>/dev/null`,
		userCommand, parentPaneID, parentPaneID,
	)
	tmuxDebugLog("wrappedCommand=%q", wrappedCommand)

	// Create a new window adjacent to the parent's window
	tmuxArgs := []string{
		"new-window",
		"-a",
		"-t", parentWindowID,
		"-e", agencParentPaneEnvVar + "=" + parentPaneID,
	}
	if parentDirpath != "" {
		tmuxArgs = append(tmuxArgs, "-c", parentDirpath)
	}
	tmuxArgs = append(tmuxArgs, wrappedCommand)
	tmuxDebugLog("tmux args: %v", tmuxArgs)

	newWindowCmd := exec.Command("tmux", tmuxArgs...)
	newWindowCmd.Stdout = os.Stdout
	newWindowCmd.Stderr = os.Stderr

	if err := newWindowCmd.Run(); err != nil {
		tmuxDebugLog("FAIL: new-window: %v", err)
		return stacktrace.Propagate(err, "failed to create new tmux window")
	}

	tmuxDebugLog("SUCCESS: window created")
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
