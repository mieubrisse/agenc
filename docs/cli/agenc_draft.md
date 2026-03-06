## agenc draft

Open an editor to draft text and paste it into a tmux pane

### Synopsis

Opens $EDITOR (defaults to vim) with a temporary Markdown file. After the
editor exits, if the file has content, it is pasted into the specified tmux
pane using tmux load-buffer and paste-buffer. The temporary file is cleaned
up afterward.

This command is designed to be called by the Side Draft palette command,
which opens it in a horizontal tmux split alongside the target pane.

```
agenc draft <target-pane-id> [flags]
```

### Options

```
  -h, --help   help for draft
```

### SEE ALSO

* [agenc](agenc.md)	 - The AgenC — agent mission management CLI

