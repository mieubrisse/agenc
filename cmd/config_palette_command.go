package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var configPaletteCommandCmd = &cobra.Command{
	Use:   paletteCommandCmdStr,
	Short: "Manage palette commands",
	Long: fmt.Sprintf(`Manage palette commands defined in config.yml.

Palette commands appear in the tmux command palette (prefix + a, k) and can
optionally be assigned tmux keybindings. Both built-in and custom commands
can be listed, added, updated, and removed.

%v

  paletteCommands:
    # Override a builtin keybinding
    showNotifications:
      tmuxKeybinding: "C-j"

    # Disable a builtin
    nukeMissions:
      disabled: true

    # Custom command with keybinding (in AgenC table: prefix + a, f)
    dotfiles:
      title: "📁 Open dotfiles"
      command: "agenc mission new mieubrisse/dotfiles"
      tmuxKeybinding: "f"

    # Global keybinding (root table, no prefix needed: Ctrl-s)
    stopThisMission:
      title: "🛑 Stop Mission"
      command: "agenc mission stop $AGENC_CALLING_MISSION_UUID"
      tmuxKeybinding: "-n C-s"
`, configYAMLExampleMarker),
}

func init() {
	configCmd.AddCommand(configPaletteCommandCmd)
}
