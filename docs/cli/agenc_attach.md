## agenc attach

Attach to the AgenC tmux session (alias for 'agenc tmux attach')

### Synopsis

Attach to the AgenC tmux session. If the session doesn't exist, it is
created with 'agenc mission new' as the initial command.

A tmux server must already be running. AgenC will not start one, because on
macOS whichever process creates the tmux server becomes the privacy identity
for every pane inside it, including your own shells. Start one from a terminal
window first.

The session propagates AGENC_DIRPATH so all windows
share the same agenc configuration.

```
agenc attach [flags]
```

### Options

```
  -h, --help   help for attach
```

### SEE ALSO

* [agenc](agenc.md)	 - The AgenC — agent mission management CLI

