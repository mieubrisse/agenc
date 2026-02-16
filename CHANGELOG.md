Changelog
=========

Breaking Changes
----------------

### Rename openShell to sideShell

The builtin palette command "openShell" has been renamed to "sideShell" to better reflect its behavior of opening a shell in a side pane.

**Migration:**

If you have customized `paletteCommands.openShell` in your config.yml, rename the key to `sideShell`:

```yaml
# Before
paletteCommands:
  openShell:
    title: "My Custom Shell"

# After
paletteCommands:
  sideShell:
    title: "My Custom Shell"
```

The display title has also changed from "🐚  Open Shell" to "🐚  Side Shell". The keybinding (`ctrl-p`) remains unchanged.
