## agenc notification show

Print the full body of a notification

### Synopsis

Print the full Markdown body of a notification to stdout.

The body is sanitized of ANSI escape sequences before display so that a
malicious or malformed body cannot manipulate the terminal.

```
agenc notification show <id> [flags]
```

### Options

```
  -h, --help   help for show
```

### SEE ALSO

* [agenc notification](agenc_notification.md)	 - List, read, and post AgenC notifications

