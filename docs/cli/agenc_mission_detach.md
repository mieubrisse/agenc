## agenc mission detach

Detach a mission from the current tmux session

### Synopsis

Detach a mission from the current tmux session.

Unlinks the mission's tmux window from your session. The mission keeps
running in the pool and can be re-attached later with 'agenc mission attach'.

Without arguments, opens an interactive fzf picker showing missions
linked to the current session.
With arguments, accepts a mission ID (short 8-char hex or full UUID).

```
agenc mission detach [mission-id] [flags]
```

### Options

```
  -h, --help   help for detach
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions

