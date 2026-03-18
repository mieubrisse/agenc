## agenc mission attach

Attach a mission to the current tmux session

### Synopsis

Attach a mission to the current tmux session.

Links the mission's tmux window into your session and focuses it.
If the mission is already linked, just focuses the window.
Stopped missions are automatically resumed; archived missions are unarchived first.

Without arguments, opens an interactive fzf picker showing all missions.
With arguments, accepts a mission ID (short 8-char hex or full UUID).

```
agenc mission attach [mission-id] [flags]
```

### Options

```
  -h, --help       help for attach
      --no-focus   don't focus the mission's tmux window after attaching
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions

