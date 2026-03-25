## agenc cron logs print

Print cron job log content

### Synopsis

Print the log output for a cron job.

The argument can be a cron name (as defined in config.yml) or a cron UUID.

By default, prints the last 200 lines. Use --all for the full file.

Examples:
  agenc cron logs print daily-report
  agenc cron logs print daily-report --all
  agenc cron logs print abc-123

```
agenc cron logs print <name-or-id> [flags]
```

### Options

```
      --all    print entire log file instead of last 200 lines
  -h, --help   help for print
```

### SEE ALSO

* [agenc cron logs](agenc_cron_logs.md)	 - View cron job logs

