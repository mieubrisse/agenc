## agenc cron health

Show which cron jobs have stopped producing missions

### Synopsis

Show which cron jobs have stopped producing missions.

A cron that stops running says nothing on its own — launchd can fail to spawn it
before agenc ever executes, which writes no log, creates no mission and posts no
notification. The server watches for that absence and sends a notification when a
cron has skipped two of its own scheduled cycles. This command shows the same
picture on demand, and never notifies or changes anything.

Examples:
  agenc cron health


```
agenc cron health [flags]
```

### Options

```
  -h, --help   help for health
```

### SEE ALSO

* [agenc cron](agenc_cron.md)	 - Manage scheduled cron jobs

