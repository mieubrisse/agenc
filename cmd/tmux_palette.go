package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/odyssey/agenc/internal/config"
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
		label:       "🦀 Quick Claude",
		description: "Launch a blank mission instantly",
		commandArgs: []string{missionCmdStr, newCmdStr, "--" + blankFlagName},
	},
	{
		label:       "🚀 Start mission",
		description: "Create a new mission and launch Claude",
		commandArgs: []string{missionCmdStr, newCmdStr},
	},
	{
		label:       "🟢 Resume mission",
		description: "Resume a stopped mission",
		commandArgs: []string{missionCmdStr, resumeCmdStr},
	},
	{
		label:       "🛑 Stop mission",
		description: "Stop a running mission",
		commandArgs: []string{missionCmdStr, stopCmdStr},
	},
	{
		label:       "❌ Remove mission",
		description: "Remove a mission and its directory",
		commandArgs: []string{missionCmdStr, rmCmdStr},
	},
	{
		label:       "💥 Nuke missions",
		description: "Remove all archived missions",
		commandArgs: []string{missionCmdStr, nukeCmdStr},
	},
}

var tmuxPaletteCmd = &cobra.Command{
	Use:   paletteCmdStr,
	Short: "Open the AgenC command palette (runs inside a tmux display-popup)",
	Long: `Presents an fzf-based command picker inside a tmux display-popup.
On selection, the chosen command runs in a new tmux window. On cancel
(Ctrl-C or Esc), the popup closes with no action.

This command is designed to be invoked by the palette keybinding
(prefix + a, k).`,
	Args: cobra.NoArgs,
	RunE: runTmuxPalette,
}

func init() {
	tmuxCmd.AddCommand(tmuxPaletteCmd)
}

// buildPaletteEntries returns the built-in palette entries plus any user-defined
// custom commands from config.yml. On config read failure, logs the error and
// returns built-in entries only (graceful degradation).
func buildPaletteEntries() ([]paletteEntry, error) {
	// Start with a copy of the built-in entries
	entries := make([]paletteEntry, len(paletteEntries))
	copy(entries, paletteEntries)

	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		tmuxDebugLog("palette: failed to get agenc dirpath: %v", err)
		return entries, nil
	}

	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		tmuxDebugLog("palette: failed to read config: %v", err)
		return nil, stacktrace.Propagate(err, "failed to read config for palette custom commands")
	}

	if len(cfg.CustomCommands) == 0 {
		return entries, nil
	}

	// Build a set of existing labels (stripped) for collision detection
	existingLabels := make(map[string]bool, len(entries))
	for _, entry := range entries {
		existingLabels[stripVariationSelectors(entry.label)] = true
	}

	// Sort custom command keys for deterministic ordering
	names := make([]string, 0, len(cfg.CustomCommands))
	for name := range cfg.CustomCommands {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cmdCfg := cfg.CustomCommands[name]
		strippedLabel := stripVariationSelectors(cmdCfg.PaletteName)
		if existingLabels[strippedLabel] {
			return nil, stacktrace.NewError(
				"custom command '%s' paletteName '%s' collides with a built-in palette entry",
				name, cmdCfg.PaletteName,
			)
		}
		existingLabels[strippedLabel] = true

		entries = append(entries, paletteEntry{
			label:       cmdCfg.PaletteName,
			commandArgs: cmdCfg.GetArgs(),
		})
	}

	return entries, nil
}

// formatPaletteEntryLine formats a palette entry for fzf display. Entries with
// a description get "Label  —  Description"; entries without get "Label" only.
func formatPaletteEntryLine(entry paletteEntry) string {
	stripped := stripVariationSelectors(entry.label)
	if entry.description == "" {
		return stripped
	}
	return fmt.Sprintf("%s  %s—  %s%s", stripped, ansiDarkGray, entry.description, ansiReset)
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

	// Parse selection: extract the label (everything before "  —  ")
	selectedLine := strings.TrimSpace(string(output))
	selectedLabel := selectedLine
	if idx := strings.Index(selectedLine, "  —  "); idx >= 0 {
		selectedLabel = selectedLine[:idx]
	}

	// Find the matching palette entry (compare against stripped labels since
	// that's what fzf received)
	var selectedEntry *paletteEntry
	for i := range entries {
		if stripVariationSelectors(entries[i].label) == selectedLabel {
			selectedEntry = &entries[i]
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

	// Dispatch: exec `<binary> tmux window new -- <binary> <args...>`
	windowNewArgs := []string{
		binaryFilepath,
		tmuxCmdStr, windowCmdStr, newCmdStr,
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
