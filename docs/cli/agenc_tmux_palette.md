## agenc tmux palette

Open the AgenC command palette (runs inside a tmux display-popup)

### Synopsis

Presents an fzf-based command picker inside a tmux display-popup.
On selection, the chosen command is dispatched to the tmux server via
run-shell -b. Commands are self-contained strings that include their own
tmux primitives when needed. Output is redirected to a log file to prevent
run-shell from echoing it into the active pane.

On cancel (Ctrl-C or Esc), the popup closes with no action.

This command is designed to be invoked by the palette keybinding
(prefix + a, k).

```
agenc tmux palette [flags]
```

### Options

```
  -h, --help   help for palette
```

### SEE ALSO

* [agenc tmux](agenc_tmux.md)	 - Manage the AgenC tmux session

