package cmd

import (
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

const (
	tmuxWindowNewDetachFlagStr  = "detach"
	tmuxWindowNewAdjacentFlagStr = "adjacent"
)

var tmuxWindowNewCmd = &cobra.Command{
	Use:   newCmdStr + " -- <command> [args...]",
	Short: "Create a new window in the AgenC tmux session and run a command",
	Long: `Create a new window in the AgenC tmux session and run a command inside it.
By default, the new window is appended at the end of the window list. Use
--adjacent (-a) to insert it next to the current window instead.

When the command exits, the pane closes and tmux auto-selects an adjacent window.

Use --detach (-d) to create the window in the background without switching
focus to it.

Must be run from inside the AgenC tmux session. Use -- to separate the
command from agenc flags.

Example:
  agenc tmux window new -- agenc mission new mieubrisse/agenc
  agenc tmux window new -a -d -- agenc mission new mieubrisse/agenc`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTmuxWindowNew,
}

func init() {
	tmuxWindowNewCmd.Flags().BoolP(tmuxWindowNewAdjacentFlagStr, "a", false, "Insert the new window adjacent to the current window instead of at the end")
	tmuxWindowNewCmd.Flags().BoolP(tmuxWindowNewDetachFlagStr, "d", false, "Create the window without switching focus to it")
	tmuxWindowCmd.AddCommand(tmuxWindowNewCmd)
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

	adjacent, err := cmd.Flags().GetBool(tmuxWindowNewAdjacentFlagStr)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get --%s flag", tmuxWindowNewAdjacentFlagStr)
	}

	detach, err := cmd.Flags().GetBool(tmuxWindowNewDetachFlagStr)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get --%s flag", tmuxWindowNewDetachFlagStr)
	}

	// Build the command string for the new window. We pass the user's command
	// directly (without shell wrapping) so that tmux's shell can exec into it.
	// This is critical: if the command is a simple command (no ; or &&), the
	// shell execs and the target process becomes the process group leader.
	// tmux reads pane_current_path from the process group leader's CWD, so
	// os.Chdir() in the wrapper will be visible to tmux.
	userCommand := buildShellCommand(args)
	tmuxDebugLog("userCommand=%q", userCommand)

	// Create a new window in the session's active window.
	// tmux resolves the active window from the current client context,
	// which is correct even from run-shell and display-popup invocations.
	tmuxArgs := []string{
		"new-window",
	}
	if adjacent {
		tmuxArgs = append(tmuxArgs, "-a")
	}
	if detach {
		tmuxArgs = append(tmuxArgs, "-d")
	}
	tmuxArgs = append(tmuxArgs, userCommand)
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
