package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/tmux"
)

var configPaletteCommandRmCmd = &cobra.Command{
	Use:   rmCmdStr + " <name>",
	Short: "Remove a palette command",
	Long: `Remove a palette command from config.yml.

For custom commands, removes the entry entirely.
For built-in commands, removes the config override and restores defaults.`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigPaletteCommandRm,
}

func init() {
	configPaletteCommandCmd.AddCommand(configPaletteCommandRmCmd)
}

func runConfigPaletteCommandRm(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	_, existsInConfig := cfg.PaletteCommands[name]
	if !existsInConfig {
		return stacktrace.NewError("palette command '%s' not found in config", name)
	}

	delete(cfg.PaletteCommands, name)

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	if config.IsBuiltinPaletteCommand(name) {
		fmt.Printf("Restored builtin '%s' to defaults\n", name)
	} else {
		fmt.Printf("Removed palette command '%s'\n", name)
	}

	if err := tmux.RefreshKeybindings(agencDirpath); err != nil {
		fmt.Printf("Warning: failed to reload tmux keybindings: %v\n", err)
	}

	return nil
}
