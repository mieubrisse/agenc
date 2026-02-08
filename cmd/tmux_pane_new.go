package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

const (
	horizontalFlagName = "horizontal"
)

var tmuxPaneNewCmd = &cobra.Command{
	Use:   newCmdStr + " -- <command> [args...]",
	Short: "Create a new pane in the current window and run a command",
	Long: `Create a new pane in the current tmux window by splitting it, and run a
command inside the new pane. When the command exits, the pane closes and
tmux focuses back to the parent pane.

By default the split is vertical (top/bottom). Use --horizontal for a
side-by-side split.

Must be run from inside the AgenC tmux session. Use -- to separate the
command from agenc flags.

The --parent-pane flag allows specifying the parent pane explicitly, which
is needed for tmux keybindings where $TMUX_PANE is not available. Use
#{pane_id} in tmux config to pass the current pane.

Example:
  agenc tmux pane new -- agenc mission new mieubrisse/agenc
  agenc tmux pane new --horizontal -- agenc mission new
  agenc tmux pane new --parent-pane "#{pane_id}" -- agenc mission new`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTmuxPaneNew,
}

func init() {
	tmuxPaneCmd.AddCommand(tmuxPaneNewCmd)
	tmuxPaneNewCmd.Flags().String(parentPaneFlagName, "", "Parent pane ID to return focus to on exit (defaults to $TMUX_PANE)")
	tmuxPaneNewCmd.Flags().BoolP(horizontalFlagName, "H", false, "Split horizontally (side-by-side) instead of vertically (top/bottom)")
}

func runTmuxPaneNew(cmd *cobra.Command, args []string) error {
	if !isInsideAgencTmux() {
		return stacktrace.NewError("must be run inside the AgenC tmux session (AGENC_TMUX != 1)")
	}

	// Determine the parent pane: explicit flag takes priority over $TMUX_PANE.
	parentPaneID, _ := cmd.Flags().GetString(parentPaneFlagName)
	if parentPaneID == "" {
		parentPaneID = os.Getenv("TMUX_PANE")
	}
	if parentPaneID == "" {
		return stacktrace.NewError("could not determine parent pane; pass --parent-pane or ensure $TMUX_PANE is set")
	}

	horizontal, _ := cmd.Flags().GetBool(horizontalFlagName)

	// Build the command string for the new pane. We wrap the user's command
	// in a shell snippet that returns focus to the parent pane on exit,
	// regardless of how the command exits.
	userCommand := buildShellCommand(args)
	wrappedCommand := fmt.Sprintf(
		`%s; tmux select-pane -t %s 2>/dev/null`,
		userCommand, parentPaneID,
	)

	// Split the current window to create a new pane
	tmuxArgs := []string{"split-window"}
	if horizontal {
		tmuxArgs = append(tmuxArgs, "-h")
	}
	tmuxArgs = append(tmuxArgs,
		"-t", parentPaneID,
		"-e", agencParentPaneEnvVar+"="+parentPaneID,
		wrappedCommand,
	)

	splitCmd := exec.Command("tmux", tmuxArgs...)
	splitCmd.Stdout = os.Stdout
	splitCmd.Stderr = os.Stderr

	if err := splitCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to create new tmux pane")
	}

	return nil
}
