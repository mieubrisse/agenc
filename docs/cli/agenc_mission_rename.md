## agenc mission rename

Rename the active session's window title for a mission

### Synopsis

Rename the active session's window title for a mission.

This is a convenience command that resolves the mission's active session and
renames it. If no mission-id is provided, uses $AGENC_CALLING_MISSION_UUID.
If no title is provided, prompts for input interactively.

Example:
  agenc mission rename                          # uses env var, prompts for title
  agenc mission rename abc12345 "My Feature"    # explicit mission and title

```
agenc mission rename [mission-id] [title] [flags]
```

### Options

```
  -h, --help   help for rename
```

### SEE ALSO

* [agenc mission](agenc_mission.md)	 - Manage agent missions

