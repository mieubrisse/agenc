package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/tableprinter"
)

var configPaletteCommandLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List palette commands",
	Long: `List all palette commands (built-in and custom).

Shows the resolved view after merging defaults with any config overrides.
Disabled built-ins are shown with "(disabled)" status.`,
	RunE: runConfigPaletteCommandLs,
}

func init() {
	configPaletteCommandCmd.AddCommand(configPaletteCommandLsCmd)
}

func runConfigPaletteCommandLs(cmd *cobra.Command, args []string) error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	resolved := cfg.GetResolvedPaletteCommands()

	// Also check for disabled builtins to show them
	type displayEntry struct {
		Name       string
		Title      string
		Keybinding string
		Command    string
		Source     string
	}

	var entries []displayEntry

	for _, cmd := range resolved {
		source := "custom"
		if cmd.IsBuiltin {
			source = "builtin"
		}
		keybinding := cmd.FormatKeybinding()
		entries = append(entries, displayEntry{
			Name:       cmd.Name,
			Title:      cmd.Title,
			Keybinding: keybinding,
			Command:    cmd.Command,
			Source:     source,
		})
	}

	// Add disabled builtins
	for _, name := range config.BuiltinPaletteCommandOrder() {
		override, hasOverride := cfg.PaletteCommands[name]
		if hasOverride && override.IsEmpty() {
			entries = append(entries, displayEntry{
				Name:   name,
				Title:  "(disabled)",
				Source: "builtin",
			})
		}
	}

	if len(entries) == 0 {
		fmt.Println("No palette commands configured.")
		return nil
	}

	tbl := tableprinter.NewTable("NAME", "TITLE", "KEYBINDING", "COMMAND", "SOURCE")
	for _, entry := range entries {
		command := entry.Command
		if len(command) > 60 {
			command = command[:57] + "..."
		}
		tbl.AddRow(entry.Name, entry.Title, entry.Keybinding, command, entry.Source)
	}

	tbl.Print()
	return nil
}
