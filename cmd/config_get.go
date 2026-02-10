package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

// supportedConfigKeys lists all keys accepted by 'config get' and 'config set'.
var supportedConfigKeys = []string{"doAutoConfirm", "paletteTmuxKeybinding", "tmuxAgencFilepath"}

var configGetCmd = &cobra.Command{
	Use:   getCmdStr + " <key>",
	Short: "Get a config value",
	Long: `Get the current value of a configuration key.

Prints "unset" if the key has not been explicitly set in config.yml.

Supported keys:
  doAutoConfirm          Skip confirmation in 'agenc do' (bool)
  paletteTmuxKeybinding  Raw bind-key args for the command palette (default: "-T agenc k")
  tmuxAgencFilepath      Path to agenc binary used in tmux keybindings (string)`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigGet,
}

func init() {
	configCmd.AddCommand(configGetCmd)
}

func runConfigGet(cmd *cobra.Command, args []string) error {
	agencDirpath, err := getAgencContext()
	if err != nil {
		return err
	}

	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	key := args[0]
	value, err := getConfigValue(cfg, key)
	if err != nil {
		return err
	}

	fmt.Println(value)
	return nil
}

// getConfigValue returns the string representation of a config key's current
// value, or "unset" if the key has not been explicitly set in the config file.
func getConfigValue(cfg *config.AgencConfig, key string) (string, error) {
	switch key {
	case "doAutoConfirm":
		if !cfg.DoAutoConfirm {
			return "unset", nil
		}
		return "true", nil
	case "paletteTmuxKeybinding":
		if cfg.PaletteTmuxKeybinding == "" {
			return "unset", nil
		}
		return cfg.PaletteTmuxKeybinding, nil
	case "tmuxAgencFilepath":
		if cfg.TmuxAgencFilepath == "" {
			return "unset", nil
		}
		return cfg.TmuxAgencFilepath, nil
	default:
		return "", stacktrace.NewError(
			"unknown config key '%s'; supported keys: %s",
			key, formatSupportedKeys(),
		)
	}
}

// formatSupportedKeys returns a comma-separated list of supported config keys.
func formatSupportedKeys() string {
	result := ""
	for i, key := range supportedConfigKeys {
		if i > 0 {
			result += ", "
		}
		result += key
	}
	return result
}
