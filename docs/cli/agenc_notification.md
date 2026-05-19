## agenc notification

List, read, and post AgenC notifications

### Synopsis

Notifications surface events that need user awareness — most commonly,
sync conflicts in writeable copies, but extensible to anything an agent or
subsystem wants to flag. Notifications are append-only: they are created once
and either remain unread or get marked as read. They are never deleted.

### Options

```
  -h, --help   help for notification
```

### SEE ALSO

* [agenc](agenc.md)	 - The AgenC — agent mission management CLI
* [agenc notification ls](agenc_notification_ls.md)	 - List notifications (default: unread only)
* [agenc notification manage](agenc_notification_manage.md)	 - Interactive notification picker — ENTER attaches to the linked mission
* [agenc notification new](agenc_notification_new.md)	 - Create a new notification (typically for agents)
* [agenc notification read](agenc_notification_read.md)	 - Mark a notification as read
* [agenc notification show](agenc_notification_show.md)	 - Print the full body of a notification

