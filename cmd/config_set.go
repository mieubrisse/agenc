package cmd

import (
	"fmt"

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
  paletteTmuxKeybinding                Raw bind-key args for the command palette (default: "-T agenc k")
  tmuxWindowBusyBackgroundColor        Background color for window tab when Claude is working (default: "colour018", empty = disable)
  tmuxWindowBusyForegroundColor        Foreground color for window tab when Claude is working (default: "", empty = disable)
  tmuxWindowAttentionBackgroundColor   Background color for window tab when Claude needs attention (default: "colour136", empty = disable)
  tmuxWindowAttentionForegroundColor   Foreground color for window tab when Claude needs attention (default: "", empty = disable)

The paletteTmuxKeybinding value is inserted verbatim after "bind-key" in the
tmux config. By default ("-T agenc k") it lives in the agenc key table, reached
via prefix + a. To make a global keybinding that works anywhere without a prefix,
use "-n BINDING". For example:

  agenc config set paletteTmuxKeybinding "-n C-y"

This binds Ctrl-y globally so the palette opens with a single keystroke.

Window coloring examples:

  # Set background to red when Claude is busy
  agenc config set tmuxWindowBusyBackgroundColor red

  # Set foreground to white when Claude is busy
  agenc config set tmuxWindowBusyForegroundColor white

  # Use tmux color numbers (see 'tmux list-colors')
  agenc config set tmuxWindowAttentionBackgroundColor colour220

  # Disable busy background coloring
  agenc config set tmuxWindowBusyBackgroundColor ""

  # Disable attention foreground coloring
  agenc config set tmuxWindowAttentionForegroundColor ""`,
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
	case "tmuxWindowBusyBackgroundColor":
		cfg.TmuxWindowBusyBackgroundColor = &value
		return nil
	case "tmuxWindowBusyForegroundColor":
		cfg.TmuxWindowBusyForegroundColor = &value
		return nil
	case "tmuxWindowAttentionBackgroundColor":
		cfg.TmuxWindowAttentionBackgroundColor = &value
		return nil
	case "tmuxWindowAttentionForegroundColor":
		cfg.TmuxWindowAttentionForegroundColor = &value
		return nil
	default:
		return stacktrace.NewError(
			"unknown config key '%s'; supported keys: %s",
			key, formatSupportedKeys(),
		)
	}
}
