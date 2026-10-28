## agenc tmux attach

Attach to the AgenC tmux session, creating it if needed

### Synopsis

Attach to the AgenC tmux session. If the session doesn't exist, it is
created with 'agenc mission new' as the initial command.

A tmux server must already be running. AgenC will not start one, because on
macOS whichever process creates the tmux server becomes the privacy identity
for every pane inside it, including your own shells. Run 'tmux' in a terminal
window first.

The session propagates AGENC_DIRPATH so all windows
share the same agenc configuration.

```
agenc tmux attach [flags]
```

### Options

```
  -h, --help   help for attach
```

### SEE ALSO

* [agenc tmux](agenc_tmux.md)	 - Manage the AgenC tmux session

