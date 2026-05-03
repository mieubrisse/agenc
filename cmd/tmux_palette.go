package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/odyssey/agenc/internal/config"
	"github.com/spf13/cobra"
)

var tmuxPaletteCmd = &cobra.Command{
	Use:   paletteCmdStr,
	Short: "Open the AgenC command palette (runs inside a tmux display-popup)",
	Long: `Presents an fzf-based command picker inside a tmux display-popup.
On selection, the chosen command is dispatched to the tmux server via
run-shell -b. Commands are self-contained strings that include their own
tmux primitives when needed. Output is redirected to a log file to prevent
run-shell from echoing it into the active pane.

On cancel (Ctrl-C or Esc), the popup closes with no action.

This command is designed to be invoked by the palette keybinding
(prefix + a, k).`,
	Args: cobra.NoArgs,
	RunE: runTmuxPalette,
}

func init() {
	tmuxCmd.AddCommand(tmuxPaletteCmd)
}

// buildPaletteEntries returns the resolved palette entries from config,
// followed by "Open <repo>" entries for each repo in the library.
// Only entries with a non-empty Title are included in the palette.
// Mission-scoped entries are excluded when callingMissionUUID is empty (i.e.
// the palette was opened from a pane that is not running a mission).
// On config read failure, returns an error.
func buildPaletteEntries(callingMissionUUID string) ([]config.ResolvedPaletteCommand, error) {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get agenc dirpath")
	}

	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read config for palette commands")
	}

	resolved := cfg.GetResolvedPaletteCommands()

	var entries []config.ResolvedPaletteCommand
	for _, cmd := range resolved {
		if cmd.Title == "" {
			continue
		}
		// Hide mission-scoped commands when not in a mission pane
		if cmd.IsMissionScoped() && callingMissionUUID == "" {
			continue
		}
		entries = append(entries, cmd)
	}

	// Append "Open <repo>" entries for each repo in the library.
	// These appear at the bottom of the palette, after all command entries.
	// Repos are sorted by configuration tier: (title+emoji) > (title) > (emoji) > (neither),
	// then alphabetically within each tier.
	repoEntries := listRepoLibrary(agencDirpath)

	type repoDisplayEntry struct {
		repoName    string
		emoji       string
		displayName string
		tier        int
	}

	var repoDisplayEntries []repoDisplayEntry
	for _, repoEntry := range repoEntries {
		repoEmoji := ""
		repoTitle := ""
		if cfg != nil {
			repoEmoji = cfg.GetRepoEmoji(repoEntry.RepoName)
			repoTitle = cfg.GetRepoTitle(repoEntry.RepoName)
		}

		displayName := plainGitRepoName(repoEntry.RepoName)
		if repoTitle != "" {
			displayName = repoTitle
		}

		displayEmoji := "📦"
		if repoEmoji != "" {
			displayEmoji = repoEmoji
		}

		// Tier: 0 = title+emoji, 1 = title only, 2 = emoji only, 3 = neither
		tier := 3
		hasTitle := repoTitle != ""
		hasEmoji := repoEmoji != ""
		if hasTitle && hasEmoji {
			tier = 0
		} else if hasTitle {
			tier = 1
		} else if hasEmoji {
			tier = 2
		}

		repoDisplayEntries = append(repoDisplayEntries, repoDisplayEntry{
			repoName:    repoEntry.RepoName,
			emoji:       displayEmoji,
			displayName: displayName,
			tier:        tier,
		})
	}

	sort.Slice(repoDisplayEntries, func(i, j int) bool {
		if repoDisplayEntries[i].tier != repoDisplayEntries[j].tier {
			return repoDisplayEntries[i].tier < repoDisplayEntries[j].tier
		}
		return repoDisplayEntries[i].displayName < repoDisplayEntries[j].displayName
	})

	for _, rde := range repoDisplayEntries {
		paletteTitle := fmt.Sprintf("%s  Open %s", rde.emoji, rde.displayName)
		command := fmt.Sprintf("agenc mission new %s", rde.repoName)

		entries = append(entries, config.ResolvedPaletteCommand{
			Name:    "open-repo-" + rde.repoName,
			Title:   paletteTitle,
			Command: command,
		})
	}

	return entries, nil
}

// plainDisplayTitle returns the plain-text (no ANSI) title as it appears in
// the palette, including the keybinding suffix when present. This is used to
// match against fzf output, which strips ANSI codes.
func plainDisplayTitle(entry config.ResolvedPaletteCommand) string {
	title := stripVariationSelectors(entry.Title)
	if kb := entry.FormatKeybinding(); kb != "" {
		title += fmt.Sprintf(" (%s)", kb)
	}
	return title
}

// formatPaletteEntryLine formats a palette entry for fzf display. Entries with
// a description get "Label (prefix → a → key)  —  Description"; entries
// without get "Label (prefix → a → key)" only. The keybinding is shown in blue.
func formatPaletteEntryLine(entry config.ResolvedPaletteCommand) string {
	stripped := stripVariationSelectors(entry.Title)
	boldLabel := fmt.Sprintf("%s%s%s", ansiBold, stripped, ansiReset)

	keybindingSuffix := ""
	if kb := entry.FormatKeybinding(); kb != "" {
		keybindingSuffix = fmt.Sprintf(" %s(%s)%s", ansiLightBlue, kb, ansiReset)
	}

	if entry.Description == "" {
		return boldLabel + keybindingSuffix
	}
	return fmt.Sprintf("%s%s  %s—  %s%s", boldLabel, keybindingSuffix, ansiDarkGray, entry.Description, ansiReset)
}

func runTmuxPalette(cmd *cobra.Command, args []string) error {
	if !isInsideTmux() {
		return stacktrace.NewError("must be run inside a tmux session")
	}

	callingMissionUUID := os.Getenv(config.CallingMissionUUIDEnvVar)
	entries, err := buildPaletteEntries(callingMissionUUID)
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

	// Run fzf for selection.
	// --delimiter/--nth restrict matching to the title portion only (everything
	// before the em-dash separator) so descriptions don't pollute search results.
	fzfCmd := exec.Command("fzf",
		"--ansi",
		"--no-multi",
		"--prompt=  ",
		"--layout=reverse",
		"--no-info",
		"--delimiter", "—",
		"--nth", "1",
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

	// Find the matching palette entry (compare against the plain display title
	// since fzf strips ANSI codes from its output)
	var selectedEntry *config.ResolvedPaletteCommand
	for i := range entries {
		if plainDisplayTitle(entries[i]) == selectedTitle {
			selectedEntry = &entries[i]
			break
		}
	}
	if selectedEntry == nil {
		return stacktrace.NewError("unknown palette selection: %q", selectedTitle)
	}

	fullCommand := buildPaletteDispatchCommand(*selectedEntry, callingMissionUUID)

	runShellCmd := exec.Command("tmux", "run-shell", "-b", fullCommand)
	runShellCmd.Stdout = os.Stdout
	runShellCmd.Stderr = os.Stderr
	if err := runShellCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to dispatch palette command via tmux run-shell")
	}

	return nil
}

// buildPaletteDispatchCommand assembles the full shell command for a palette
// entry, including env exports and output redirection. It handles two contexts:
//   - run-shell (no pane): exports AGENC_CALLING_PANE_ID so CLI commands can
//     tell the server which session to use
//   - display-popup (temporary pane): injects -e flag to pass the calling pane
//     ID into the popup's environment, since TMUX_PANE inside the popup refers
//     to the temporary popup pane
func buildPaletteDispatchCommand(entry config.ResolvedPaletteCommand, callingMissionUUID string) string {
	var envPrefix string
	agencDirpath, err := config.GetAgencDirpath()
	if err == nil && config.GetNamespaceSuffix(agencDirpath) != "" {
		envPrefix = fmt.Sprintf("export AGENC_DIRPATH=%s; ", agencDirpath)
		if config.IsTestEnv() {
			envPrefix += "export AGENC_TEST_ENV=1; "
		}
	}

	callingPane := os.Getenv("AGENC_CALLING_PANE_ID")
	if callingPane != "" {
		envPrefix += fmt.Sprintf("export AGENC_CALLING_PANE_ID=%s; ", callingPane)
	}
	if entry.IsMissionScoped() && callingMissionUUID != "" {
		envPrefix += fmt.Sprintf("export AGENC_CALLING_MISSION_UUID=%s; ", callingMissionUUID)
	}

	fullCommand := envPrefix + entry.Command

	// For display-popup commands, pass the calling pane ID into the popup's
	// environment via -e. Without this, AGENC_CALLING_PANE_ID from the
	// run-shell env doesn't propagate into the popup.
	if callingPane != "" && strings.Contains(fullCommand, "display-popup") {
		fullCommand = strings.Replace(fullCommand, "display-popup",
			fmt.Sprintf("display-popup -e AGENC_CALLING_PANE_ID=%s", callingPane), 1)
	}

	// Redirect output to the palette log file so tmux run-shell doesn't
	// echo it into the active pane.
	agencDirpathForLog, _ := config.GetAgencDirpath()
	logFilepath := config.GetPaletteLogFilepath(agencDirpathForLog)
	_ = os.MkdirAll(filepath.Dir(logFilepath), 0755)
	fullCommand += fmt.Sprintf(" >> %s 2>&1", logFilepath)

	return fullCommand
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
