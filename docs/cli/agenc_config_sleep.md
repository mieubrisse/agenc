## agenc config sleep

Manage sleep mode windows

### Synopsis

Manage sleep mode time windows that block mission and cron creation.

When sleep mode is active, the server rejects new mission creation (except
cron-triggered missions). Configure time windows with day-of-week and
HH:MM start/end times. Overnight windows are supported.

### Options

```
  -h, --help   help for sleep
```

### SEE ALSO

* [agenc config](agenc_config.md)	 - Manage agenc configuration
* [agenc config sleep add](agenc_config_sleep_add.md)	 - Add a sleep mode window
* [agenc config sleep ls](agenc_config_sleep_ls.md)	 - List sleep mode windows
* [agenc config sleep rm](agenc_config_sleep_rm.md)	 - Remove a sleep mode window

