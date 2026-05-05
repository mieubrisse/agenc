## agenc notifications create

Create a new notification (typically for agents)

### Synopsis

Create a new notification.

Body content can be supplied either via --body=<string> for short content or
via --body-file=<path> for longer content. Use --body-file=- to read the body
from stdin (handy for piping):

  cat conflict-report.md | agenc notifications create \
      --kind=writeable_copy.conflict --title="Rebase conflict" --body-file=-

```
agenc notifications create [flags]
```

### Options

```
      --body string          body content (mutually exclusive with --body-file)
      --body-file string     path to body content file; use - for stdin
  -h, --help                 help for create
      --kind string          kind tag (required, e.g. writeable_copy.conflict)
      --source-repo string   associated repo in canonical format (optional)
      --title string         one-line title (required)
```

### SEE ALSO

* [agenc notifications](agenc_notifications.md)	 - List, read, and create AgenC notifications

