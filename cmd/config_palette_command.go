package cmd

import (
	"github.com/spf13/cobra"
)

var configPaletteCommandCmd = &cobra.Command{
	Use:   paletteCommandCmdStr,
	Short: "Manage palette commands",
	Long: `Manage palette commands defined in config.yml.

Palette commands appear in the tmux command palette (prefix + a, k) and can
optionally be assigned tmux keybindings. Both built-in and custom commands
can be listed, added, updated, and removed.

Example config.yml:

  paletteCommands:
    # Override a builtin keybinding
    newMission:
      tmuxKeybinding: "C-n"

    # Disable a builtin
    nukeMissions: {}

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
`,
}

func init() {
	configCmd.AddCommand(configPaletteCommandCmd)
}
