package cmd

import (
	"os"
	"os/exec"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

const (
	verticalFlagName = "vertical"
)

var tmuxPaneNewCmd = &cobra.Command{
	Use:   newCmdStr + " -- <command> [args...]",
	Short: "Create a new pane in the current window and run a command",
	Long: `Create a new pane in the current tmux window by splitting it, and run a
command inside the new pane. When the command exits, the pane closes and
tmux focuses back to the parent pane.

By default the split is vertical (side-by-side). Use --vertical for a
top/bottom split.

Must be run from inside the AgenC tmux session. Use -- to separate the
command from agenc flags.

The --parent-pane flag allows specifying the parent pane explicitly, which
is needed for tmux keybindings where $TMUX_PANE is not available. Use
#{pane_id} in tmux config to pass the current pane.

Example:
  agenc tmux pane new -- agenc mission new mieubrisse/agenc
  agenc tmux pane new --vertical -- agenc mission new
  agenc tmux pane new --parent-pane "#{pane_id}" -- agenc mission new`,
	Args: cobra.MinimumNArgs(1),
	RunE: runTmuxPaneNew,
}

func init() {
	tmuxPaneCmd.AddCommand(tmuxPaneNewCmd)
	tmuxPaneNewCmd.Flags().String(parentPaneFlagName, "", "Parent pane ID to return focus to on exit (defaults to $TMUX_PANE)")
	tmuxPaneNewCmd.Flags().BoolP(verticalFlagName, "V", false, "Split vertically (top/bottom) instead of the default side-by-side")
}

func runTmuxPaneNew(cmd *cobra.Command, args []string) error {
	tmuxDebugLog("=== tmux pane new ===")
	tmuxDebugLog("args: %v", args)
	tmuxDebugLog("AGENC_TMUX=%q", os.Getenv(agencTmuxEnvVar))
	tmuxDebugLog("TMUX_PANE=%q", os.Getenv("TMUX_PANE"))
	tmuxDebugLog("PATH=%q", os.Getenv("PATH"))

	if !isInsideAgencTmux() {
		tmuxDebugLog("FAIL: isInsideAgencTmux() returned false")
		return stacktrace.NewError("must be run inside the AgenC tmux session (AGENC_TMUX != 1)")
	}

	// Determine the parent pane: explicit flag takes priority over $TMUX_PANE.
	parentPaneID, _ := cmd.Flags().GetString(parentPaneFlagName)
	if parentPaneID == "" {
		parentPaneID = os.Getenv("TMUX_PANE")
	}
	tmuxDebugLog("parentPaneID=%q", parentPaneID)
	if parentPaneID == "" {
		tmuxDebugLog("FAIL: empty parentPaneID")
		return stacktrace.NewError("could not determine parent pane; pass --parent-pane or ensure $TMUX_PANE is set")
	}

	vertical, _ := cmd.Flags().GetBool(verticalFlagName)
	tmuxDebugLog("vertical=%v", vertical)

	// Pass the user's command directly (no shell wrapping) so the shell can
	// exec into it. See tmux_window_new.go for the rationale.
	userCommand := buildShellCommand(args)
	tmuxDebugLog("userCommand=%q", userCommand)

	// Split the current window to create a new pane.
	// Default is side-by-side (-h); --vertical omits -h for top/bottom.
	tmuxArgs := []string{"split-window"}
	if !vertical {
		tmuxArgs = append(tmuxArgs, "-h")
	}
	tmuxArgs = append(tmuxArgs,
		"-t", parentPaneID,
		"-e", agencParentPaneEnvVar+"="+parentPaneID,
		userCommand,
	)
	tmuxDebugLog("tmux args: %v", tmuxArgs)

	splitCmd := exec.Command("tmux", tmuxArgs...)
	splitCmd.Stdout = os.Stdout
	splitCmd.Stderr = os.Stderr

	if err := splitCmd.Run(); err != nil {
		tmuxDebugLog("FAIL: split-window: %v", err)
		return stacktrace.Propagate(err, "failed to create new tmux pane")
	}

	tmuxDebugLog("SUCCESS: pane created")
	return nil
}
