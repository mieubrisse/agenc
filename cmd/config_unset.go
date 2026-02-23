package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/tmux"
)

var configUnsetCmd = &cobra.Command{
	Use:   "unset <key>",
	Short: "Unset a config value",
	Long: `Unset a configuration key in config.yml, reverting it to the default value.

Supported keys:
  defaultModel                                 Default Claude model for missions (e.g., "opus", "sonnet", "claude-opus-4-6")
  paletteTmuxKeybinding                      Raw bind-key args for the command palette (default: "-T agenc k")
  tmuxWindowTitle.busyBackgroundColor        Background color for window tab when Claude is working (default: "colour018", empty = disable)
  tmuxWindowTitle.busyForegroundColor        Foreground color for window tab when Claude is working (default: "", empty = disable)
  tmuxWindowTitle.attentionBackgroundColor   Background color for window tab when Claude needs attention (default: "colour136", empty = disable)
  tmuxWindowTitle.attentionForegroundColor   Foreground color for window tab when Claude needs attention (default: "", empty = disable)`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigUnset,
}

func init() {
	configCmd.AddCommand(configUnsetCmd)
}

func runConfigUnset(cmd *cobra.Command, args []string) error {
	agencDirpath, err := getAgencContext()
	if err != nil {
		return err
	}

	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	key := args[0]

	if err := unsetConfigValue(cfg, key); err != nil {
		return err
	}

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("%s unset (reverted to default)\n", key)

	if isTmuxKeybindingKey(key) {
		if err := tmux.RefreshKeybindings(agencDirpath); err != nil {
			fmt.Printf("Warning: failed to reload tmux keybindings: %v\n", err)
		}
	}

	return nil
}

// unsetConfigValue removes a config value by setting it to the zero value.
func unsetConfigValue(cfg *config.AgencConfig, key string) error {
	switch key {
	case "defaultModel":
		cfg.DefaultModel = ""
		return nil
	case "paletteTmuxKeybinding":
		cfg.PaletteTmuxKeybinding = ""
		return nil
	case "tmuxWindowTitle.busyBackgroundColor",
		"tmuxWindowTitle.busyForegroundColor",
		"tmuxWindowTitle.attentionBackgroundColor",
		"tmuxWindowTitle.attentionForegroundColor":
		setTmuxWindowTitleField(cfg, key, nil)
		return nil
	default:
		return stacktrace.NewError(
			"unknown config key '%s'; supported keys: %s",
			key, formatSupportedKeys(),
		)
	}
}
