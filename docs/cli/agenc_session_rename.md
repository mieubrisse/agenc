## agenc session rename

Rename a session's window title

### Synopsis

Rename a session's window title.

Sets the agenc_custom_title on the session, which controls the tmux window name.
If no title is provided, prompts for input interactively.
An empty title clears the custom title, falling back to the auto-resolved title.

Example:
  agenc session rename 18749fb5-02ba-4b19-b989-4e18fbf8ea92 "My Feature Work"
  agenc session rename 18749fb5-02ba-4b19-b989-4e18fbf8ea92    # prompts for title

```
agenc session rename <session-id> [title] [flags]
```

### Options

```
  -h, --help   help for rename
```

### SEE ALSO

* [agenc session](agenc_session.md)	 - Manage Claude Code sessions

