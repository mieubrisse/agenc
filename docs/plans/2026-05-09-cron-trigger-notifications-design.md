Cron-Trigger Notifications and Notification Manage Picker
==========================================================

Bead: agenc-wfdb (notifications half — the EOD-review cron is out of scope here).

Goal
----

When a cron-triggered mission is created, surface it as a notification so the
user sees that scheduled work is available without having to poll. Provide an
interactive `agenc notifications manage` picker that lists notifications,
previews each, and on `ENTER` attaches to the linked mission.

Stated user goal: *"I know when cron-triggered missions are available, which
frees me up to start scheduling more stuff and trust that I'll see it."*

Non-Goals
---------

- Read/unread workflow in the picker (path documented; deferred).
- Multi-select bulk actions in the picker.
- The end-of-day-review cron itself.
- Filtering or searching by kind in the picker.
- TUI implementation (fzf is the chosen path; TUI would be a future swap if
  the picker accumulates more than ~5 actions or needs richer interaction).

Architecture
------------

Four components:

### 1. Schema migration

Add a nullable `mission_id TEXT` column to the `notifications` table. Existing
rows persist with `NULL`. Following the existing additive-ALTER pattern in
`internal/database/migrations.go` (e.g., `addKnownFileSizeColumnSQL`):

```
ALTER TABLE notifications ADD COLUMN mission_id TEXT;
```

The column is nullable because most notification kinds (e.g.,
`writeable_copy.conflict`) have no associated mission. New cron-triggered
notifications populate it; future kinds opt in as relevant.

### 2. Cron auto-notification (server-side)

In `handleCreateMission` (`internal/server/missions.go`), after the mission
record is created and `spawnWrapper` runs, branch on `req.Source == "cron"`.
Parse `req.SourceMetadata` for the `cron_name` field. Build a notification:

- `Kind = "cron.triggered"`
- `Title = "Cron triggered: <cron_name>"` (fallback to `req.SourceID` if name
  missing or metadata malformed)
- `MissionID = &missionRecord.ID`
- `BodyMarkdown` = minimal: cron name, trigger source (scheduled vs manual,
  read from `source_metadata.trigger`), mission short ID, repo (if any), and
  a single-line prompt preview

Call `s.db.CreateNotification` synchronously. On error, log via `s.logger` and
return `201 Created` for the mission anyway. **Notification creation must
never fail the mission request** — the mission has already been created and
the wrapper spawned.

### 3. `agenc notifications manage` (CLI / picker)

New CLI command in `cmd/notifications_manage.go`:

1. Reuse existing `serverClient()` to fetch all notifications via the existing
   `ListNotifications` API, sorted by `created_at DESC` (already the default).
2. Short-circuit if the list is empty: print
   `"No notifications. Try scheduling a cron — see \`agenc cron\`."` and
   exit 0.
3. Build tabular fzf input. Columns shown: `<created>  <kind>  <mission>
   <title>`. The first column of the fzf input is the notification short ID
   (hidden from display via `--with-nth 2..`) — used by `--preview` and the
   exit-time selection. The mission column shows the mission short ID or `—`
   so eligibility for `ENTER` is at-a-glance.
4. Run fzf with:
   - `--ansi` (for our own coloring of the mission column)
   - `--with-nth 2..`
   - `--header 'ENTER attach │ ESC cancel'`
   - `--preview 'agenc notifications show {1}'`
   - `--preview-window 'right:60%:wrap'` (subject to manual UX tweak)
5. On exit code 0: parse `{1}` from selected line, fetch the notification, and
   if `MissionID` is non-nil, exec `agenc mission attach <mission_id>` (replace
   the picker process). If `MissionID` is nil, print
   `"Notification has no linked mission"` and exit 0.
6. On exit code 130/1 (cancel): exit 0 silently, matching the existing fzf
   helper convention.

The picker is implemented as a standalone fzf invocation, not by extending
`runFzfPicker`, because preview integration is specific to this view and the
generic helper has no preview support today. If a future picker (e.g.,
`missions manage`) needs the same shape, extract a shared helper then.

Read/unread keybinds (`r`, `u`) are deferred. The path forward is
`--bind 'r:execute-silent(agenc notifications read {1})+reload-sync(<list-cmd>)'`
plus a new `agenc notifications unread` CLI and `MarkNotificationUnread` DB
function. Stable `created_at DESC` sort means the cursor stays on the
toggled row across reloads.

### 4. Tmux palette: replace "Show Notifications" entry

In `internal/config/agenc_config.go`, replace the existing
`🔔 Show Notifications` palette entry (which spawned an Adjutant agent) with
`🔔 Notification Center`, dispatching `agenc notifications manage`. Existing
user configs persist whatever they have; the change affects fresh defaults
only.

The unread-count banner currently displayed by `tmux_palette.go`
(`"pick \"Show Notifications\" to review"`) is updated to reference
`"Notification Center"`.

Edge Cases and Error Handling
-----------------------------

| Edge case | Handler |
|-----------|---------|
| `source_metadata` empty or invalid JSON | Title falls back to `Cron triggered: <source_id>` |
| `cron_name` missing from metadata | Same fallback |
| `cron_name` contains control chars / ANSI | New helper `sanitizeNotificationTitle(s)` strips `\r\n\t` and `\x1b[...m` sequences, truncates to 200 chars; applied at write time. Defense-in-depth — server is trusted but cron config is user-edited |
| Empty notification list | CLI prints the empty message before invoking fzf |
| `spawnWrapper` failed but mission record exists | Notification is still created; ENTER → `mission attach` will surface the wrapper failure cleanly |
| Mission archived after notification was created | `mission attach` already handles archived missions; surface its error verbatim |
| Mission deleted entirely | `mission attach` returns "mission not found"; surface error |
| Notification insert fails after mission create | Server logs; mission request still returns 201. Never fail the mission request |
| `fzf` not in PATH | `exec.LookPath("fzf")` check at the top of the manage command |
| Server unreachable | Existing `serverClient()` connect error; reuse CLI pattern |
| Migration on DB with existing rows | Additive `ALTER TABLE … ADD COLUMN` — existing rows get NULL |
| Picker run outside tmux | Out of scope (`mission attach` is tmux-only — assume tmux for `notifications manage` too) |
| Concurrent cron triggers | SQLite single-writer serializes; each notification gets a unique row |
| Cursor reset on future reload | Use `reload-sync` plus stable sort; verified — non-issue |

Testing
-------

**Unit tests (`internal/database/notifications_test.go`):**

- `TestCreateNotification_WithMissionID` — roundtrip a notification with a
  non-nil `MissionID`.
- `TestCreateNotification_WithoutMissionID` — verify nil persists and reads
  back as nil.

**Server unit tests (`internal/server/missions_test.go`, create if absent):**

- `TestHandleCreateMission_CronSourceCreatesNotification` — POST with
  `source=cron`, valid `source_metadata`; verify a `cron.triggered`
  notification with `mission_id == created mission ID` exists.
- `TestHandleCreateMission_NonCronSourceNoNotification` — POST with empty
  source or `source=user`; verify no notification was created.
- `TestHandleCreateMission_CronWithMalformedMetadata` — `source_metadata` is
  invalid JSON; notification still created with title falling back to
  `source_id`.
- `TestHandleCreateMission_CronWithMissingCronName` — `source_metadata` valid
  JSON but no `cron_name`; same fallback.

**Migration test (`internal/database/migrations_test.go`):**

- Apply migration to a fresh DB and to a DB with pre-existing notification
  rows; verify `mission_id` column exists and pre-existing rows have NULL.

**E2E (`scripts/e2e-test.sh`):**

- `cron run creates a notification` — create a cron via
  `agenc-test cron new`, run it via `agenc-test cron run <name>`, then
  `agenc-test notifications ls --kind cron.triggered` returns a row with
  `mission_id` populated.
- `notifications manage with no notifications prints empty message` —
  `agenc-test notifications manage` (run with stdin not a TTY) prints the
  empty-list message and exits 0.

**Manual testing (per project CLAUDE.md tmux-integration rule):**

- Tmux palette "Notification Center" entry launches `notifications manage`
  in `display-popup`.
- ENTER on a notification with `mission_id` attaches to that mission.
- ENTER on a notification with no `mission_id` prints the no-mission message
  and exits.
- Preview pane renders the body of the highlighted notification.

Out-of-Scope Confirmation
-------------------------

- `agenc notifications unread` CLI / `MarkNotificationUnread` DB function.
- `r` / `u` keybinds in fzf.
- Multi-select bulk actions.
- The EOD-review cron itself (the other half of `agenc-wfdb`).
- Kind/text filtering inside the picker.
