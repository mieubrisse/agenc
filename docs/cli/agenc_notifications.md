## agenc notifications

List, read, and create AgenC notifications

### Synopsis

Notifications surface events that need user awareness — most commonly,
sync conflicts in writeable copies, but extensible to anything an agent or
subsystem wants to flag. Notifications are append-only: they are created once
and either remain unread or get marked as read. They are never deleted.

### Options

```
  -h, --help   help for notifications
```

### SEE ALSO

* [agenc](agenc.md)	 - The AgenC — agent mission management CLI
* [agenc notifications create](agenc_notifications_create.md)	 - Create a new notification (typically for agents)
* [agenc notifications ls](agenc_notifications_ls.md)	 - List notifications (default: unread only)
* [agenc notifications read](agenc_notifications_read.md)	 - Mark a notification as read
* [agenc notifications show](agenc_notifications_show.md)	 - Print the full body of a notification

