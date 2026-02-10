package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/odyssey/agenc/internal/config"
	"github.com/spf13/cobra"
)

var tmuxPaletteCmd = &cobra.Command{
	Use:   paletteCmdStr,
	Short: "Open the AgenC command palette (runs inside a tmux display-popup)",
	Long: `Presents an fzf-based command picker inside a tmux display-popup.
On selection, the chosen command runs via sh -c. On cancel
(Ctrl-C or Esc), the popup closes with no action.

This command is designed to be invoked by the palette keybinding
(prefix + a, k).`,
	Args: cobra.NoArgs,
	RunE: runTmuxPalette,
}

func init() {
	tmuxCmd.AddCommand(tmuxPaletteCmd)
}

// buildPaletteEntries returns the resolved palette entries from config.
// Only entries with a non-empty Title are included in the palette.
// On config read failure, returns an error.
func buildPaletteEntries() ([]config.ResolvedPaletteCommand, error) {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get agenc dirpath")
	}

	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read config for palette commands")
	}

	resolved := cfg.GetResolvedPaletteCommands()

	// Filter: only entries with a title appear in the palette
	var entries []config.ResolvedPaletteCommand
	for _, cmd := range resolved {
		if cmd.Title != "" {
			entries = append(entries, cmd)
		}
	}

	return entries, nil
}

// formatPaletteEntryLine formats a palette entry for fzf display. Entries with
// a description get "Label  —  Description"; entries without get "Label" only.
func formatPaletteEntryLine(entry config.ResolvedPaletteCommand) string {
	stripped := stripVariationSelectors(entry.Title)
	boldLabel := fmt.Sprintf("%s%s%s", ansiBold, stripped, ansiReset)
	if entry.Description == "" {
		return boldLabel
	}
	return fmt.Sprintf("%s  %s—  %s%s", boldLabel, ansiDarkGray, entry.Description, ansiReset)
}

func runTmuxPalette(cmd *cobra.Command, args []string) error {
	if !isInsideAgencTmux() {
		return stacktrace.NewError("must be run inside the AgenC tmux session (AGENC_TMUX != 1)")
	}

	entries, err := buildPaletteEntries()
	if err != nil {
		return err
	}

	// Build fzf input: one line per entry.
	// Variation selectors (U+FE0F) are stripped so that emoji width is
	// consistent across tmux, the terminal, and fzf — preventing layout jitter.
	var fzfInput strings.Builder
	for _, entry := range entries {
		fmt.Fprintln(&fzfInput, formatPaletteEntryLine(entry))
	}

	// Run fzf for selection
	fzfCmd := exec.Command("fzf",
		"--ansi",
		"--no-multi",
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

	// Parse selection: extract the title (everything before "  —  ")
	selectedLine := strings.TrimSpace(string(output))
	selectedTitle := selectedLine
	if idx := strings.Index(selectedLine, "  —  "); idx >= 0 {
		selectedTitle = selectedLine[:idx]
	}

	// Find the matching palette entry (compare against stripped titles since
	// that's what fzf received)
	var selectedEntry *config.ResolvedPaletteCommand
	for i := range entries {
		if stripVariationSelectors(entries[i].Title) == selectedTitle {
			selectedEntry = &entries[i]
			break
		}
	}
	if selectedEntry == nil {
		return stacktrace.NewError("unknown palette selection: %q", selectedTitle)
	}

	// Dispatch: execute the command string via sh -c
	tmuxDebugLog("palette: executing %q", selectedEntry.Command)

	execCmd := exec.Command("sh", "-c", selectedEntry.Command)
	execCmd.Stdin = os.Stdin
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr

	if err := execCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to execute palette selection")
	}

	return nil
}

// stripVariationSelectors removes Unicode variation selectors (U+FE0E and
// U+FE0F) from a string. These invisible codepoints switch characters between
// text and emoji presentation, but terminals and tmux disagree on the resulting
// width, causing layout jitter in TUI programs like fzf.
func stripVariationSelectors(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\uFE0E' || r == '\uFE0F' {
			return -1 // drop
		}
		return r
	}, s)
}
