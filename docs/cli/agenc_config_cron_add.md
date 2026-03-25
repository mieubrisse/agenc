## agenc config cron add

Add a new cron job

### Synopsis

Add a new cron job to config.yml.

The name must be unique and follow the naming rules (letters, numbers, hyphens,
underscores; max 64 characters). Both --schedule and --prompt are required.

Schedule expressions use 5 fields: minute hour day month weekday.
Only simple integer values and '*' (any) are supported. Ranges (1-5),
lists (1,3,5), step values (*/15), and named days (SUN) are not supported
because macOS launchd cannot represent them.

Examples:
  agenc config cron add daily-report \
    --schedule="0 9 * * *" \
    --prompt="Generate the daily status report" \
    --repo=github.com/owner/my-repo

  agenc config cron add weekly-cleanup \
    --schedule="0 0 * * 0" \
    --prompt="Clean up old temporary files" \
    --overlap=skip


```
agenc config cron add <name> [flags]
```

### Options

```
      --description string   human-readable description (optional)
  -h, --help                 help for add
      --prompt string        initial prompt for the Claude mission (required)
      --repo string          repository to clone (e.g., github.com/owner/repo) (optional)
      --schedule string      cron schedule expression (e.g., '0 9 * * *') (required)
```

### SEE ALSO

* [agenc config cron](agenc_config_cron.md)	 - Manage cron job configuration

