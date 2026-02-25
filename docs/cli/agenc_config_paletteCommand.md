## agenc config paletteCommand

Manage palette commands

### Synopsis

Manage palette commands defined in config.yml.

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
      command: "agenc tmux window new -- agenc mission new mieubrisse/dotfiles"
      tmuxKeybinding: "f"

    # Global keybinding (root table, no prefix needed: Ctrl-s)
    stopThisMission:
      title: "🛑 Stop Mission"
      command: "agenc mission stop $AGENC_CALLING_MISSION_UUID"
      tmuxKeybinding: "-n C-s"


### Options

```
  -h, --help   help for paletteCommand
```

### SEE ALSO

* [agenc config](agenc_config.md)	 - Manage agenc configuration
* [agenc config paletteCommand add](agenc_config_paletteCommand_add.md)	 - Add a custom palette command
* [agenc config paletteCommand ls](agenc_config_paletteCommand_ls.md)	 - List palette commands
* [agenc config paletteCommand rm](agenc_config_paletteCommand_rm.md)	 - Remove a palette command
* [agenc config paletteCommand update](agenc_config_paletteCommand_update.md)	 - Update a palette command

