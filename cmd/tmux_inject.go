package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/odyssey/agenc/internal/config"
	agentmux "github.com/odyssey/agenc/internal/tmux"
	"github.com/spf13/cobra"
)

const (
	sentinelBegin = "# >>> AgenC keybindings >>>"
	sentinelEnd   = "# <<< AgenC keybindings <<<"
)

var tmuxInjectCmd = &cobra.Command{
	Use:   injectCmdStr,
	Short: "Install AgenC tmux keybindings",
	Long: `Write an AgenC-managed keybindings file and add a source-file directive to
your tmux.conf. If a tmux server is running, the keybindings are sourced
immediately.

All keybindings live under the "agenc" key table, activated with prefix + a:
  prefix + a, k  — open command palette
  prefix + a, n  — new mission in a new tmux window
  prefix + a, p  — new mission in a side-by-side pane`,
	Args: cobra.NoArgs,
	RunE: runTmuxInject,
}

func init() {
	tmuxCmd.AddCommand(tmuxInjectCmd)
}

func runTmuxInject(cmd *cobra.Command, args []string) error {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve agenc directory")
	}

	keybindingsFilepath := config.GetTmuxKeybindingsFilepath(agencDirpath)

	// Detect tmux version for version-gated keybindings (e.g. display-popup).
	// On error, fall back to (0, 0) — palette keybinding is omitted but all
	// other keybindings are still emitted.
	tmuxMajor, tmuxMinor, _ := agentmux.DetectVersion()

	// Read config for the tmuxAgencFilepath override and palette commands.
	agencBinary := "agenc"
	var keybindings []agentmux.CustomKeybinding
	if cfg, _, cfgErr := config.ReadAgencConfig(agencDirpath); cfgErr == nil {
		agencBinary = cfg.GetTmuxAgencBinary()
		resolved := cfg.GetResolvedPaletteCommands()
		for _, entry := range resolved {
			if entry.TmuxKeybinding == "" {
				continue
			}
			comment := fmt.Sprintf("%s (prefix + a, %s)", entry.Name, entry.TmuxKeybinding)
			if entry.Title != "" {
				comment = fmt.Sprintf("%s — %s (prefix + a, %s)", entry.Name, entry.Title, entry.TmuxKeybinding)
			}
			keybindings = append(keybindings, agentmux.CustomKeybinding{
				Key:     entry.TmuxKeybinding,
				Command: entry.Command,
				Comment: comment,
			})
		}
	}

	if err := agentmux.WriteKeybindingsFile(keybindingsFilepath, tmuxMajor, tmuxMinor, agencBinary, keybindings); err != nil {
		return err
	}
	fmt.Printf("Wrote keybindings to %s\n", keybindingsFilepath)

	// Use ~ in the source-file directive so tmux.conf is portable across machines
	displayFilepath := contractHomePath(keybindingsFilepath)

	if err := injectTmuxConfSourceLine(displayFilepath); err != nil {
		return err
	}

	if err := agentmux.SourceKeybindings(keybindingsFilepath); err != nil {
		fmt.Printf("Warning: %v\n", err)
	} else {
		fmt.Println("Sourced keybindings into running tmux server")
	}

	return nil
}

// contractHomePath replaces a leading $HOME prefix with ~ for use in
// config files that should be portable across machines.
func contractHomePath(path string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if strings.HasPrefix(path, homeDir) {
		return "~" + path[len(homeDir):]
	}
	return path
}


// findTmuxConfFilepath locates the user's tmux.conf. It checks ~/.tmux.conf
// first, then ~/.config/tmux/tmux.conf. Returns the path, whether the file
// currently exists, and any error.
func findTmuxConfFilepath() (string, bool, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", false, stacktrace.Propagate(err, "failed to determine home directory")
	}

	candidates := []string{
		filepath.Join(homeDir, ".tmux.conf"),
		filepath.Join(homeDir, ".config", "tmux", "tmux.conf"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true, nil
		}
	}

	// Neither exists — default to ~/.tmux.conf
	return candidates[0], false, nil
}

// buildSentinelBlock returns the sentinel-wrapped source-file directive.
// The path written into the directive uses displayFilepath (which may contain ~)
// so the resulting tmux.conf is portable across machines.
func buildSentinelBlock(displayFilepath string) string {
	return fmt.Sprintf("%s\nsource-file %s\n%s", sentinelBegin, displayFilepath, sentinelEnd)
}

// injectTmuxConfSourceLine idempotently adds or updates a sentinel-wrapped
// source-file directive in the user's tmux.conf. displayFilepath is the
// portable form (with ~ instead of $HOME) written into the directive.
func injectTmuxConfSourceLine(displayFilepath string) error {
	tmuxConfFilepath, exists, err := findTmuxConfFilepath()
	if err != nil {
		return err
	}

	sentinelBlock := buildSentinelBlock(displayFilepath)

	if !exists {
		// Create the file with just the sentinel block
		if err := os.WriteFile(tmuxConfFilepath, []byte(sentinelBlock+"\n"), 0644); err != nil {
			return stacktrace.Propagate(err, "failed to create '%s'", tmuxConfFilepath)
		}
		fmt.Printf("Created %s with source-file directive\n", tmuxConfFilepath)
		return nil
	}

	content, err := os.ReadFile(tmuxConfFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read '%s'", tmuxConfFilepath)
	}
	fileContent := string(content)

	beginIdx := strings.Index(fileContent, sentinelBegin)
	endIdx := strings.Index(fileContent, sentinelEnd)

	if beginIdx >= 0 && endIdx >= 0 {
		// Sentinel block exists — check if it's identical
		existingBlock := fileContent[beginIdx : endIdx+len(sentinelEnd)]
		if existingBlock == sentinelBlock {
			fmt.Printf("Already configured in %s\n", tmuxConfFilepath)
			return nil
		}

		// Different path — replace the block
		newContent := fileContent[:beginIdx] + sentinelBlock + fileContent[endIdx+len(sentinelEnd):]
		if err := os.WriteFile(tmuxConfFilepath, []byte(newContent), 0644); err != nil {
			return stacktrace.Propagate(err, "failed to update '%s'", tmuxConfFilepath)
		}
		fmt.Printf("Updated source-file directive in %s\n", tmuxConfFilepath)
		return nil
	}

	// No sentinel block — append to file
	appendContent := fileContent
	if !strings.HasSuffix(appendContent, "\n") {
		appendContent += "\n"
	}
	appendContent += "\n" + sentinelBlock + "\n"

	if err := os.WriteFile(tmuxConfFilepath, []byte(appendContent), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to update '%s'", tmuxConfFilepath)
	}
	fmt.Printf("Added source-file directive to %s\n", tmuxConfFilepath)
	return nil
}
