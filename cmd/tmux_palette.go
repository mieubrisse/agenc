package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

// paletteEntry defines a single entry in the command palette.
type paletteEntry struct {
	// label is the user-visible name shown in the fzf picker.
	label string

	// description is shown alongside the label for context.
	description string

	// commandArgs is the argument list appended after the resolved agenc binary
	// path when dispatching via `agenc tmux window new -- <binary> <args...>`.
	commandArgs []string
}

var paletteEntries = []paletteEntry{
	{
		label:       "New mission",
		description: "Create a new mission and launch Claude",
		commandArgs: []string{missionCmdStr, newCmdStr},
	},
	{
		label:       "Resume mission",
		description: "Resume a stopped mission",
		commandArgs: []string{missionCmdStr, resumeCmdStr},
	},
}

var tmuxPaletteCmd = &cobra.Command{
	Use:   paletteCmdStr,
	Short: "Open the AgenC command palette (runs inside a tmux display-popup)",
	Long: `Presents an fzf-based command picker inside a tmux display-popup.
On selection, the chosen command runs in a new tmux window. On cancel
(Ctrl-C or Esc), the popup closes with no action.

This command is designed to be invoked by the palette keybinding
(prefix + a, k) and requires --parent-pane.`,
	Args: cobra.NoArgs,
	RunE: runTmuxPalette,
}

func init() {
	tmuxCmd.AddCommand(tmuxPaletteCmd)
	tmuxPaletteCmd.Flags().String(parentPaneFlagName, "", "Parent pane ID (required, passed from keybinding via #{pane_id})")
	_ = tmuxPaletteCmd.MarkFlagRequired(parentPaneFlagName)
}

func runTmuxPalette(cmd *cobra.Command, args []string) error {
	if !isInsideAgencTmux() {
		return stacktrace.NewError("must be run inside the AgenC tmux session (AGENC_TMUX != 1)")
	}

	parentPaneID, _ := cmd.Flags().GetString(parentPaneFlagName)

	// Build fzf input: one line per entry, formatted as "Label  —  Description"
	var fzfInput strings.Builder
	for _, entry := range paletteEntries {
		fmt.Fprintf(&fzfInput, "%s  —  %s\n", entry.label, entry.description)
	}

	// Run fzf for selection
	fzfCmd := exec.Command("fzf",
		"--ansi",
		"--no-multi",
		"--header=AgenC Command Palette",
		"--prompt=  ",
		"--layout=reverse",
		"--no-info",
	)
	fzfCmd.Stdin = strings.NewReader(fzfInput.String())
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		// fzf exits with code 130 on Ctrl-C/Esc — treat as clean cancel
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 130 {
			return nil
		}
		// fzf exits with code 1 when no match — also treat as cancel
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil
		}
		return stacktrace.Propagate(err, "fzf selection failed")
	}

	// Parse selection: extract the label (everything before "  —  ")
	selectedLine := strings.TrimSpace(string(output))
	selectedLabel := selectedLine
	if idx := strings.Index(selectedLine, "  —  "); idx >= 0 {
		selectedLabel = selectedLine[:idx]
	}

	// Find the matching palette entry
	var selectedEntry *paletteEntry
	for i := range paletteEntries {
		if paletteEntries[i].label == selectedLabel {
			selectedEntry = &paletteEntries[i]
			break
		}
	}
	if selectedEntry == nil {
		return stacktrace.NewError("unknown palette selection: %q", selectedLabel)
	}

	// Resolve the agenc binary path for reliable invocation
	binaryFilepath, err := resolveAgencBinaryPath()
	if err != nil {
		return err
	}

	// Dispatch: exec `<binary> tmux window new --parent-pane <pane> -- <binary> <args...>`
	windowNewArgs := []string{
		binaryFilepath,
		tmuxCmdStr, windowCmdStr, newCmdStr,
		"--" + parentPaneFlagName, parentPaneID,
		"--",
		binaryFilepath,
	}
	windowNewArgs = append(windowNewArgs, selectedEntry.commandArgs...)

	tmuxDebugLog("palette: execing %v", windowNewArgs)

	execCmd := exec.Command(windowNewArgs[0], windowNewArgs[1:]...)
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	if err := execCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to execute palette selection")
	}

	return nil
}
