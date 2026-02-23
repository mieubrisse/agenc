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
	Long: `Set a configuration key.

Supported keys:
  claudeCodeOAuthToken                       Claude Code OAuth token (stored in secure token file, not config.yml)
  defaultModel                                 Default Claude model for missions (e.g., "opus", "sonnet", "claude-opus-4-6")
  paletteTmuxKeybinding                      Raw bind-key args for the command palette (default: "-T agenc k")
  tmuxWindowTitle.busyBackgroundColor        Background color for window tab when Claude is working (default: "colour018", empty = disable)
  tmuxWindowTitle.busyForegroundColor        Foreground color for window tab when Claude is working (default: "", empty = disable)
  tmuxWindowTitle.attentionBackgroundColor   Background color for window tab when Claude needs attention (default: "colour136", empty = disable)
  tmuxWindowTitle.attentionForegroundColor   Foreground color for window tab when Claude needs attention (default: "", empty = disable)

The paletteTmuxKeybinding value is inserted verbatim after "bind-key" in the
tmux config. By default ("-T agenc k") it lives in the agenc key table, reached
via prefix + a. To make a global keybinding that works anywhere without a prefix,
use "-n BINDING". For example:

  agenc config set paletteTmuxKeybinding "-n C-y"

This binds Ctrl-y globally so the palette opens with a single keystroke.

Window coloring examples:

  # Set background to red when Claude is busy
  agenc config set tmuxWindowTitle.busyBackgroundColor red

  # Set foreground to white when Claude is busy
  agenc config set tmuxWindowTitle.busyForegroundColor white

  # Use tmux color numbers (see 'tmux list-colors')
  agenc config set tmuxWindowTitle.attentionBackgroundColor colour220

  # Disable busy background coloring
  agenc config set tmuxWindowTitle.busyBackgroundColor ""

  # Disable attention foreground coloring
  agenc config set tmuxWindowTitle.attentionForegroundColor ""`,
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

	key := args[0]
	value := args[1]

	// claudeCodeOAuthToken is stored in a dedicated token file, not config.yml.
	if key == "claudeCodeOAuthToken" {
		if err := config.WriteOAuthToken(agencDirpath, value); err != nil {
			return stacktrace.Propagate(err, "failed to write OAuth token")
		}
		if value == "" {
			fmt.Println("claudeCodeOAuthToken cleared (token file removed)")
		} else {
			fmt.Println("claudeCodeOAuthToken = (set)")
		}
		return nil
	}

	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

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
	case "defaultModel":
		cfg.DefaultModel = value
		return nil
	case "paletteTmuxKeybinding":
		cfg.PaletteTmuxKeybinding = value
		return nil
	case "tmuxWindowTitle.busyBackgroundColor",
		"tmuxWindowTitle.busyForegroundColor",
		"tmuxWindowTitle.attentionBackgroundColor",
		"tmuxWindowTitle.attentionForegroundColor":
		setTmuxWindowTitleField(cfg, key, &value)
		return nil
	default:
		return stacktrace.NewError(
			"unknown config key '%s'; supported keys: %s",
			key, formatSupportedKeys(),
		)
	}
}

// setTmuxWindowTitleField sets a field on the TmuxWindowTitle config,
// initializing the struct if needed.
func setTmuxWindowTitleField(cfg *config.AgencConfig, key string, value *string) {
	if cfg.TmuxWindowTitle == nil {
		cfg.TmuxWindowTitle = &config.TmuxWindowTitleConfig{}
	}
	switch key {
	case "tmuxWindowTitle.busyBackgroundColor":
		cfg.TmuxWindowTitle.BusyBackgroundColor = value
	case "tmuxWindowTitle.busyForegroundColor":
		cfg.TmuxWindowTitle.BusyForegroundColor = value
	case "tmuxWindowTitle.attentionBackgroundColor":
		cfg.TmuxWindowTitle.AttentionBackgroundColor = value
	case "tmuxWindowTitle.attentionForegroundColor":
		cfg.TmuxWindowTitle.AttentionForegroundColor = value
	}
}
