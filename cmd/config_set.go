package cmd

import (
	"fmt"
	"strconv"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/tmux"
)

var configSetCmd = &cobra.Command{
	Use:   setCmdStr + " <key> <value>",
	Short: "Set a config value",
	Long: `Set a configuration key in config.yml.

Supported keys:
  paletteTmuxKeybinding        Raw bind-key args for the command palette (default: "-T agenc k")
  tmuxWindowBusyColor          Tmux color for window tab when Claude is working (default: "colour018")
  tmuxWindowAttentionColor     Tmux color for window tab when Claude needs attention (default: "colour136")
  tmuxWindowColoringEnabled    Enable/disable window tab coloring (default: true)

The paletteTmuxKeybinding value is inserted verbatim after "bind-key" in the
tmux config. By default ("-T agenc k") it lives in the agenc key table, reached
via prefix + a. To make a global keybinding that works anywhere without a prefix,
use "-n BINDING". For example:

  agenc config set paletteTmuxKeybinding "-n C-y"

This binds Ctrl-y globally so the palette opens with a single keystroke.

Window coloring examples:

  # Disable window tab coloring entirely
  agenc config set tmuxWindowColoringEnabled false

  # Use red when Claude is busy
  agenc config set tmuxWindowBusyColor red

  # Use tmux color numbers (see 'tmux list-colors')
  agenc config set tmuxWindowAttentionColor colour220`,
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

	if isTmuxKeybindingKey(key) {
		if err := tmux.RefreshKeybindings(agencDirpath); err != nil {
			fmt.Printf("Warning: failed to reload tmux keybindings: %v\n", err)
		}
	}

	return nil
}

// isTmuxKeybindingKey returns true if the config key affects tmux keybindings
// and should trigger a keybindings refresh.
func isTmuxKeybindingKey(key string) bool {
	return key == "paletteTmuxKeybinding"
}

// setConfigValue applies a string value to the named config key, performing
// type conversion and validation as needed.
func setConfigValue(cfg *config.AgencConfig, key, value string) error {
	switch key {
	case "paletteTmuxKeybinding":
		cfg.PaletteTmuxKeybinding = value
		return nil
	case "tmuxWindowBusyColor":
		cfg.TmuxWindowBusyColor = value
		return nil
	case "tmuxWindowAttentionColor":
		cfg.TmuxWindowAttentionColor = value
		return nil
	case "tmuxWindowColoringEnabled":
		b, err := strconv.ParseBool(value)
		if err != nil {
			return stacktrace.NewError("invalid value '%s' for %s; must be true or false", value, key)
		}
		cfg.TmuxWindowColoringEnabled = &b
		return nil
	default:
		return stacktrace.NewError(
			"unknown config key '%s'; supported keys: %s",
			key, formatSupportedKeys(),
		)
	}
}
