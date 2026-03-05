## agenc mission send-keys

Send keystrokes to a running mission's tmux pane

### Synopsis

Send keystrokes to a running mission's tmux pane via tmux send-keys.

Keys are passed through to tmux verbatim — use tmux key names for special keys:
  Enter, Escape, C-c, C-d, Space, Tab, Up, Down, Left, Right, etc.

Examples:
  agenc mission send-keys abc123 "hello world" Enter
  agenc mission send-keys abc123 C-c
  echo "fix the bug" | agenc mission send-keys abc123
  echo "fix the bug" | agenc mission send-keys abc123 Enter

```
agenc mission send-keys <mission-id> [keys...] [flags]
```

### Options

```
  -h, --help   help for send-keys
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions

