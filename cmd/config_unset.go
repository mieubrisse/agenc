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
  paletteTmuxKeybinding        Raw bind-key args for the command palette (default: "-T agenc k")
  defaultGitHubUser            Default GitHub username for shorthand repo references
  tmuxWindowBusyColor          Tmux color for window tab when Claude is working (default: "colour018", empty = disable)
  tmuxWindowAttentionColor     Tmux color for window tab when Claude needs attention (default: "colour136", empty = disable)`,
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
	case "paletteTmuxKeybinding":
		cfg.PaletteTmuxKeybinding = ""
		return nil
	case "defaultGitHubUser":
		cfg.DefaultGitHubUser = ""
		return nil
	case "tmuxWindowBusyColor":
		cfg.TmuxWindowBusyColor = nil
		return nil
	case "tmuxWindowAttentionColor":
		cfg.TmuxWindowAttentionColor = nil
		return nil
	default:
		return stacktrace.NewError(
			"unknown config key '%s'; supported keys: %s",
			key, formatSupportedKeys(),
		)
	}
}
