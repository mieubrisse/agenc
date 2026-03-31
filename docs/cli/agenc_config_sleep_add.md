## agenc config sleep add

Add a sleep mode window

### Synopsis

Add a time window during which mission and cron creation is blocked.

Examples:
  agenc config sleep add --days mon,tue,wed,thu --start 22:00 --end 06:00
  agenc config sleep add --days fri,sat --start 23:00 --end 07:00

```
agenc config sleep add [flags]
```

### Options

```
      --days string    comma-separated day names (mon,tue,wed,thu,fri,sat,sun) (required)
      --end string     end time in HH:MM format (required)
  -h, --help           help for add
      --start string   start time in HH:MM format (required)
```

### SEE ALSO

* [agenc config sleep](agenc_config_sleep.md)	 - Manage sleep mode windows

