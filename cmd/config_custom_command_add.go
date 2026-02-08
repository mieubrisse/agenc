package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var configCustomCommandAddCmd = &cobra.Command{
	Use:   addCmdStr + " <name>",
	Short: "Add a custom palette command",
	Long: `Add a custom palette command to config.yml.

The name is used as the config key and must start with a letter, containing
only letters, numbers, hyphens, and underscores (max 64 characters).

Example:
  agenc config custom-command add dotfiles \
    --args="mission new github.com/mieubrisse/dotfiles" \
    --palette-name="Open dotfiles"
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigCustomCommandAdd,
}

func init() {
	configCustomCommandCmd.AddCommand(configCustomCommandAddCmd)
	configCustomCommandAddCmd.Flags().String(customCommandArgsFlagName, "", "agenc subcommand arguments (required)")
	configCustomCommandAddCmd.Flags().String(customCommandPaletteNameFlagName, "", "label shown in the palette picker (required)")
	_ = configCustomCommandAddCmd.MarkFlagRequired(customCommandArgsFlagName)
	_ = configCustomCommandAddCmd.MarkFlagRequired(customCommandPaletteNameFlagName)
}

func runConfigCustomCommandAdd(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := config.ValidateCustomCommandName(name); err != nil {
		return err
	}

	argsFlag, err := cmd.Flags().GetString(customCommandArgsFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", customCommandArgsFlagName)
	}
	if argsFlag == "" {
		return stacktrace.NewError("--%s cannot be empty", customCommandArgsFlagName)
	}

	paletteName, err := cmd.Flags().GetString(customCommandPaletteNameFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", customCommandPaletteNameFlagName)
	}
	if paletteName == "" {
		return stacktrace.NewError("--%s cannot be empty", customCommandPaletteNameFlagName)
	}

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	if _, exists := cfg.CustomCommands[name]; exists {
		return stacktrace.NewError("custom command '%s' already exists; use '%s %s %s %s %s' to modify it",
			name, agencCmdStr, configCmdStr, customCommandCmdStr, updateCmdStr, name)
	}

	for existingName, existingCmd := range cfg.CustomCommands {
		if existingCmd.PaletteName == paletteName {
			return stacktrace.NewError("palette name '%s' is already used by custom command '%s'", paletteName, existingName)
		}
	}

	if cfg.CustomCommands == nil {
		cfg.CustomCommands = make(map[string]config.CustomCommandConfig)
	}
	cfg.CustomCommands[name] = config.CustomCommandConfig{
		Args:        argsFlag,
		PaletteName: paletteName,
	}

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Added custom command '%s'\n", name)
	return nil
}
