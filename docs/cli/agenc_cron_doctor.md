## agenc cron doctor

Audit scheduled cron jobs for silent failure

### Synopsis

Audit every enabled cron job and report the ways it can silently stop delivering.

A cron loop that dies emits no signal on its own. This command checks the three
places a failure hides:

  - launchd has no job registered, so the cron can never fire
  - launchd fires on schedule but the process dies before AgenC runs, writing
    nothing to the cron log and creating no mission
  - the mission starts and dies before doing any work, leaving a run record
    that looks exactly like a healthy one

Exits 2 when findings exist, 0 when everything is delivering.

Examples:
  agenc cron doctor
  agenc cron doctor --repair --notify


```
agenc cron doctor [flags]
```

### Options

```
  -h, --help     help for doctor
      --notify   post an AgenC notification when findings exist
      --repair   reload launchd jobs whose registration has gone bad
```

### SEE ALSO

* [agenc cron](agenc_cron.md)	 - Manage scheduled cron jobs

