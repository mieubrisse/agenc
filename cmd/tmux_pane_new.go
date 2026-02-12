package cmd

import (
	"os"
	"os/exec"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

const (
	paneNewBelowFlagName  = "below"
	paneNewDetachFlagName = "detach"
)

var tmuxPaneNewCmd = &cobra.Command{
	Use:   newCmdStr + " -- <command> [args...]",
	Short: "Create a new pane in the current window and run a command",
	Long: `Create a new pane in the current tmux window by splitting it, and run a
command inside the new pane. When the command exits, the pane closes.

By default the new pane opens to the right. Use --below for a top/bottom
split. Use --detach to create the pane without switching focus to it.

The new pane inherits the current pane's working directory.

Must be run from inside the AgenC tmux session. Use -- to separate the
command from agenc flags.

Example:
  agenc tmux pane new -- agenc mission new mieubrisse/agenc
  agenc tmux pane new --below -- agenc mission new
  agenc tmux pane new --detach -- tail -f /tmp/log`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTmuxPaneNew,
}

func init() {
	tmuxPaneCmd.AddCommand(tmuxPaneNewCmd)
	tmuxPaneNewCmd.Flags().BoolP(paneNewBelowFlagName, "b", false, "Place the new pane below instead of to the right")
	tmuxPaneNewCmd.Flags().BoolP(paneNewDetachFlagName, "d", false, "Create the pane without switching focus to it")
}

func runTmuxPaneNew(cmd *cobra.Command, args []string) error {
	if !isInsideAgencTmux() {
		return stacktrace.NewError("must be run inside the AgenC tmux session (AGENC_TMUX != 1)")
	}

	below, _ := cmd.Flags().GetBool(paneNewBelowFlagName)
	detach, _ := cmd.Flags().GetBool(paneNewDetachFlagName)

	// Pass the user's command directly (no shell wrapping) so the shell can
	// exec into it. See tmux_window_new.go for the rationale.
	userCommand := buildShellCommand(args)

	// Split the current window to create a new pane.
	// Default is side-by-side (-h); --below omits -h for top/bottom.
	// -c #{pane_current_path} inherits the current pane's working directory.
	// tmux splits the active pane by default, which is correct for both
	// direct invocation and keybindings.
	tmuxArgs := []string{"split-window", "-c", "#{pane_current_path}"}
	if !below {
		tmuxArgs = append(tmuxArgs, "-h")
	}
	if detach {
		tmuxArgs = append(tmuxArgs, "-d")
	}
	tmuxArgs = append(tmuxArgs, userCommand)

	splitCmd := exec.Command("tmux", tmuxArgs...)
	splitCmd.Stdout = os.Stdout
	splitCmd.Stderr = os.Stderr

	if err := splitCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to create new tmux pane")
	}

	return nil
}
