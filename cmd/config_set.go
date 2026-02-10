package cmd

import (
	"fmt"
	"strconv"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var configSetCmd = &cobra.Command{
	Use:   setCmdStr + " <key> <value>",
	Short: "Set a config value",
	Long: `Set a configuration key in config.yml.

Supported keys:
  doAutoConfirm          Skip confirmation in 'agenc do' (bool: true/false)
  paletteTmuxKeybinding  Tmux key for the command palette (string, default: k)
  tmuxAgencFilepath      Path to agenc binary used in tmux keybindings (string)`,
	Args: cobra.ExactArgs(2),
	RunE: runConfigSet,
}

func init() {
	configCmd.AddCommand(configSetCmd)
}

func runConfigSet(cmd *cobra.Command, args []string) error {
	agencDirpath, err := getAgencContext()
	if err != nil {
		return err
	}

	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	key := args[0]
	value := args[1]

	if err := setConfigValue(cfg, key, value); err != nil {
		return err
	}

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("%s = %s\n", key, value)
	return nil
}

// setConfigValue applies a string value to the named config key, performing
// type conversion and validation as needed.
func setConfigValue(cfg *config.AgencConfig, key, value string) error {
	switch key {
	case "doAutoConfirm":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return stacktrace.NewError("invalid value '%s' for %s; must be true or false", value, key)
		}
		cfg.DoAutoConfirm = b
		return nil
	case "paletteTmuxKeybinding":
		cfg.PaletteTmuxKeybinding = value
		return nil
	case "tmuxAgencFilepath":
		cfg.TmuxAgencFilepath = value
		return nil
	default:
		return stacktrace.NewError(
			"unknown config key '%s'; supported keys: %s",
			key, formatSupportedKeys(),
		)
	}
}
