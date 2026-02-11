package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/tmux"
)

var configPaletteCommandUpdateCmd = &cobra.Command{
	Use:   updateCmdStr + " <name>",
	Short: "Update a palette command",
	Long: `Update a palette command in config.yml.

Works for both built-in and custom commands. For built-ins, creates or updates
the config override entry. At least one flag must be provided.

Example:
  agenc config paletteCommand update startMission --keybinding="C-n"
  agenc config paletteCommand update dotfiles --title="📁 Dotfiles" --keybinding="f"
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigPaletteCommandUpdate,
}

func init() {
	configPaletteCommandCmd.AddCommand(configPaletteCommandUpdateCmd)
	configPaletteCommandUpdateCmd.Flags().String(paletteCommandTitleFlagName, "", "title shown in the palette picker")
	configPaletteCommandUpdateCmd.Flags().String(paletteCommandCommandFlagName, "", "full command to execute")
	configPaletteCommandUpdateCmd.Flags().String(paletteCommandKeybindingFlagName, "", "tmux keybinding (e.g. \"f\" or \"C-y\")")
	configPaletteCommandUpdateCmd.Flags().String(paletteCommandDescriptionFlagName, "", "description shown alongside the title")
}

func runConfigPaletteCommandUpdate(cmd *cobra.Command, args []string) error {
	name := args[0]

	titleChanged := cmd.Flags().Changed(paletteCommandTitleFlagName)
	commandChanged := cmd.Flags().Changed(paletteCommandCommandFlagName)
	keybindingChanged := cmd.Flags().Changed(paletteCommandKeybindingFlagName)
	descriptionChanged := cmd.Flags().Changed(paletteCommandDescriptionFlagName)

	if !titleChanged && !commandChanged && !keybindingChanged && !descriptionChanged {
		return stacktrace.NewError("at least one of --%s, --%s, --%s, or --%s must be provided",
			paletteCommandTitleFlagName, paletteCommandCommandFlagName,
			paletteCommandKeybindingFlagName, paletteCommandDescriptionFlagName)
	}

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	isBuiltin := config.IsBuiltinPaletteCommand(name)
	existing, existsInConfig := cfg.PaletteCommands[name]

	// For non-builtins, must already exist in config
	if !isBuiltin && !existsInConfig {
		return stacktrace.NewError("palette command '%s' not found; use '%s %s %s %s' to add it",
			name, agencCmdStr, configCmdStr, paletteCommandCmdStr, addCmdStr)
	}

	// Apply changes to the config entry
	if titleChanged {
		newTitle, err := cmd.Flags().GetString(paletteCommandTitleFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", paletteCommandTitleFlagName)
		}
		existing.Title = newTitle
	}

	if commandChanged {
		newCommand, err := cmd.Flags().GetString(paletteCommandCommandFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", paletteCommandCommandFlagName)
		}
		existing.Command = newCommand
	}

	if keybindingChanged {
		newKeybinding, err := cmd.Flags().GetString(paletteCommandKeybindingFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", paletteCommandKeybindingFlagName)
		}
		existing.TmuxKeybinding = newKeybinding
	}

	if descriptionChanged {
		newDescription, err := cmd.Flags().GetString(paletteCommandDescriptionFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", paletteCommandDescriptionFlagName)
		}
		existing.Description = newDescription
	}

	if cfg.PaletteCommands == nil {
		cfg.PaletteCommands = make(map[string]config.PaletteCommandConfig)
	}
	cfg.PaletteCommands[name] = existing

	// Validate uniqueness before writing
	configFilepath := config.GetConfigFilepath(agencDirpath)
	resolved := cfg.GetResolvedPaletteCommands()
	if err := validateResolvedUniqueness(resolved, configFilepath); err != nil {
		return err
	}

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	if isBuiltin {
		fmt.Printf("Updated builtin palette command '%s'\n", name)
	} else {
		fmt.Printf("Updated palette command '%s'\n", name)
	}

	if err := tmux.RefreshKeybindings(agencDirpath); err != nil {
		fmt.Printf("Warning: failed to reload tmux keybindings: %v\n", err)
	}

	return nil
}
