package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var configCustomCommandUpdateCmd = &cobra.Command{
	Use:   updateCmdStr + " <name>",
	Short: "Update a custom palette command",
	Long: `Update a custom palette command in config.yml.

At least one of --args or --palette-name must be provided.

Example:
  agenc config custom-command update dotfiles --palette-name="🛠️ Open dotfiles"
  agenc config custom-command update dotfiles --args="mission resume dotfiles"
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigCustomCommandUpdate,
}

func init() {
	configCustomCommandCmd.AddCommand(configCustomCommandUpdateCmd)
	configCustomCommandUpdateCmd.Flags().String(customCommandArgsFlagName, "", "agenc subcommand arguments")
	configCustomCommandUpdateCmd.Flags().String(customCommandPaletteNameFlagName, "", "label shown in the palette picker")
}

func runConfigCustomCommandUpdate(cmd *cobra.Command, args []string) error {
	name := args[0]

	argsChanged := cmd.Flags().Changed(customCommandArgsFlagName)
	paletteNameChanged := cmd.Flags().Changed(customCommandPaletteNameFlagName)

	if !argsChanged && !paletteNameChanged {
		return stacktrace.NewError("at least one of --%s or --%s must be provided",
			customCommandArgsFlagName, customCommandPaletteNameFlagName)
	}

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	existing, exists := cfg.CustomCommands[name]
	if !exists {
		return stacktrace.NewError("custom command '%s' not found", name)
	}

	if argsChanged {
		newArgs, err := cmd.Flags().GetString(customCommandArgsFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", customCommandArgsFlagName)
		}
		if newArgs == "" {
			return stacktrace.NewError("--%s cannot be empty", customCommandArgsFlagName)
		}
		existing.Args = newArgs
	}

	if paletteNameChanged {
		newPaletteName, err := cmd.Flags().GetString(customCommandPaletteNameFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", customCommandPaletteNameFlagName)
		}
		if newPaletteName == "" {
			return stacktrace.NewError("--%s cannot be empty", customCommandPaletteNameFlagName)
		}
		for otherName, otherCmd := range cfg.CustomCommands {
			if otherName != name && otherCmd.PaletteName == newPaletteName {
				return stacktrace.NewError("palette name '%s' is already used by custom command '%s'", newPaletteName, otherName)
			}
		}
		existing.PaletteName = newPaletteName
	}

	cfg.CustomCommands[name] = existing

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Updated custom command '%s'\n", name)
	return nil
}
