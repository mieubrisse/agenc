## agenc notifications ls

List notifications (default: unread only)

### Synopsis

List notifications. By default only unread notifications are shown.
Use --all to see read notifications too. Use --repo or --kind to filter.

```
agenc notifications ls [flags]
```

### Options

```
      --all           include read notifications
  -h, --help          help for ls
      --kind string   filter by kind (e.g. writeable_copy.conflict)
      --repo string   filter by source repo (canonical name)
```

### SEE ALSO

* [agenc notifications](agenc_notifications.md)	 - List, read, and create AgenC notifications

