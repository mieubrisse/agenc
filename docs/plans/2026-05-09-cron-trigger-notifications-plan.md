Cron-Trigger Notifications and Manage Picker — Implementation Plan
====================================================================

> **For Claude:** REQUIRED SUB-SKILL: Use `superpowers:executing-plans` to implement this plan task-by-task.

**Goal:** When a cron-triggered mission is created, automatically create a `cron.triggered` notification linked to the new mission. Add `agenc notifications manage` — an fzf picker that lists notifications, previews them, and on ENTER attaches to the linked mission.

**Architecture:** Server-side branch in `handleCreateMission` writes a notification record after a `source=cron` mission is spawned. New nullable `mission_id` column on `notifications` is the attach target. New CLI under `cmd/notifications_manage.go` runs a standalone fzf invocation with a preview pane. Tmux palette entry "Notification Center" replaces the existing Adjutant-spawning "Show Notifications" entry.

**Tech Stack:** Go, SQLite (existing schema), Cobra (existing CLI), `fzf` (already integrated via `cmd/fzf_picker.go`).

**Design doc:** `docs/plans/2026-05-09-cron-trigger-notifications-design.md`

**Bead:** `agenc-wfdb` (notifications half — the EOD-review cron is a separate follow-up).

Sequencing rationale
--------------------

Tasks are ordered so each one is testable on its own and builds toward the user-visible feature. Schema first → DB layer → server API surface → cron auto-create → CLI surface (`--mission-id` flag) → manage picker → palette integration → E2E sweep. Commit after every task.

**Build invocation:** Whenever a task says `make check` or `make build`, the Bash tool needs `dangerouslyDisableSandbox: true` (per project CLAUDE.md — Go's build cache lives outside the default sandbox).

---

Task 1: Schema migration — add `mission_id` column
---------------------------------------------------

**Files:**
- Modify: `internal/database/migrations.go` (add migration entry + SQL constant)
- Modify: `internal/database/database.go` (register migration in the migration list)
- Test: `internal/database/migrations_test.go` (if a test pattern for ALTER migrations already exists; otherwise rely on the table-shape check downstream)

**Step 1: Inspect the existing migration pattern**

Open `internal/database/migrations.go` and find `addKnownFileSizeColumnSQL` and the function that runs it (`migrateAddKnownFileSizeColumn` or similar). Mirror that pattern for the new column.

**Step 2: Add the SQL constant**

Inside the constant block in `internal/database/migrations.go`:

```go
addNotificationsMissionIDColumnSQL = `ALTER TABLE notifications ADD COLUMN mission_id TEXT;`
```

**Step 3: Add the migration function**

Mirror the existing `migrateAdd*Column` pattern. Function name: `migrateAddNotificationsMissionIDColumn`. It must be idempotent — check for column existence first using `PRAGMA table_info(notifications)` and only `ALTER TABLE` if absent. (Look at `migrateAddKnownFileSizeColumn` for the exact pattern to copy.)

**Step 4: Register the migration**

In `internal/database/database.go`, append a new entry to the migrations slice:

```go
{migrateAddNotificationsMissionIDColumn, "add mission_id column to notifications"},
```

It must come AFTER `migrateCreateNotificationsTable`.

**Step 5: Write a roundtrip test**

In `internal/database/notifications_test.go` add:

```go
func TestCreateNotification_WithMissionID(t *testing.T) {
    db := newTestDB(t)
    missionID := "11111111-2222-3333-4444-555555555555"
    n := &Notification{
        ID:           "aaaaaaaa-0000-0000-0000-000000000099",
        Kind:         "cron.triggered",
        Title:        "Cron triggered: foo",
        BodyMarkdown: "body",
        MissionID:    &missionID,
    }
    if err := db.CreateNotification(n); err != nil {
        t.Fatalf("CreateNotification failed: %v", err)
    }
    got, err := db.GetNotification(n.ID)
    if err != nil {
        t.Fatalf("GetNotification failed: %v", err)
    }
    if got.MissionID == nil || *got.MissionID != missionID {
        t.Fatalf("MissionID roundtrip failed: got %v want %v", got.MissionID, missionID)
    }
}

func TestCreateNotification_WithoutMissionID_PersistsNil(t *testing.T) {
    db := newTestDB(t)
    n := &Notification{
        ID:           "aaaaaaaa-0000-0000-0000-000000000098",
        Kind:         "k",
        Title:        "no mission",
        BodyMarkdown: "x",
    }
    if err := db.CreateNotification(n); err != nil {
        t.Fatalf("CreateNotification failed: %v", err)
    }
    got, err := db.GetNotification(n.ID)
    if err != nil {
        t.Fatalf("GetNotification failed: %v", err)
    }
    if got.MissionID != nil {
        t.Fatalf("expected nil MissionID, got %v", *got.MissionID)
    }
}
```

These tests will FAIL to compile until Task 2 adds `MissionID` to the struct — that's expected. They drive Task 2.

**Step 6: Run tests — confirm compile failure**

Run: `make check` (with `dangerouslyDisableSandbox: true`)

Expected: compile error referencing `Notification.MissionID` undefined. This drives the next task.

**Step 7: Commit**

```bash
git add internal/database/migrations.go internal/database/database.go internal/database/notifications_test.go
git commit -m "Add mission_id column migration to notifications"
git pull --rebase && git push
```

(The commit will fail to compile — fine. Task 2 makes it green. If you'd rather have green commits, hold this commit until Task 2 is also done and bundle them.)

---

Task 2: Notification model — add `MissionID` field, update SQL and scanners
----------------------------------------------------------------------------

**Files:**
- Modify: `internal/database/notifications.go` (struct, INSERT, SELECT)
- Modify: `internal/database/scanners.go` (scanNotification, scanNotificationFromRows)

**Step 1: Add field to struct**

In `internal/database/notifications.go`, add `MissionID *string` to the `Notification` struct (pointer, nullable):

```go
type Notification struct {
    ID           string
    Kind         string
    SourceRepo   string
    MissionID    *string  // attach target — nil for notifications without a linked mission
    Title        string
    BodyMarkdown string
    CreatedAt    time.Time
    ReadAt       *time.Time
}
```

**Step 2: Update INSERT to write mission_id**

In `CreateNotification`, build a `sql.NullString` for `MissionID` (parallel to `SourceRepo`) and add it to the INSERT column list and value placeholders:

```go
var missionID sql.NullString
if n.MissionID != nil && *n.MissionID != "" {
    missionID = sql.NullString{String: *n.MissionID, Valid: true}
}
_, err := db.conn.Exec(
    "INSERT INTO notifications (id, kind, source_repo, mission_id, title, body_markdown, created_at, read_at) VALUES (?, ?, ?, ?, ?, ?, ?, NULL)",
    n.ID, n.Kind, sourceRepo, missionID, n.Title, n.BodyMarkdown, n.CreatedAt.UTC().Format(time.RFC3339),
)
```

**Step 3: Update SELECT in `GetNotification` and `ListNotifications`**

In `internal/database/notifications.go` and `internal/database/queries.go`, add `mission_id` to the SELECT column list of every query that reads notifications. Match the column order in scanners.

**Step 4: Update scanners**

In `internal/database/scanners.go`, update `scanNotification` and `scanNotificationFromRows` (and the helper they share) to scan the new column into a `sql.NullString` and assign to `n.MissionID` when valid.

**Step 5: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)

Expected: all notification roundtrip tests pass, including the two new ones from Task 1. If migration tests fail because the migration didn't run on the test DB, double-check `database.go` registration order.

**Step 6: Commit**

```bash
git add internal/database/notifications.go internal/database/scanners.go internal/database/queries.go
git commit -m "Wire MissionID through Notification model, SQL, and scanners"
git pull --rebase && git push
```

---

Task 3: Server API — accept and return `mission_id`
----------------------------------------------------

**Files:**
- Modify: `internal/server/notifications_handlers.go` (request and response struct + handler)
- Modify: `internal/server/client.go` (CreateNotification and any List/Get response struct mirrors)

**Step 1: Add field to request and response**

In `internal/server/notifications_handlers.go`:

```go
type CreateNotificationRequest struct {
    Kind         string `json:"kind"`
    SourceRepo   string `json:"source_repo,omitempty"`
    MissionID    string `json:"mission_id,omitempty"`
    Title        string `json:"title"`
    BodyMarkdown string `json:"body_markdown"`
}

type NotificationResponse struct {
    ID           string `json:"id"`
    Kind         string `json:"kind"`
    SourceRepo   string `json:"source_repo,omitempty"`
    MissionID    string `json:"mission_id,omitempty"`
    Title        string `json:"title"`
    BodyMarkdown string `json:"body_markdown"`
    CreatedAt    string `json:"created_at"`
    ReadAt       string `json:"read_at,omitempty"`
}
```

**Step 2: Update `toNotificationResponse`**

Map `n.MissionID` (nullable pointer) into the response (empty string when nil).

**Step 3: Update `handleCreateNotification`**

Pass `req.MissionID` into the database struct (as `*string`, with nil when empty).

**Step 4: Update the client**

In `internal/server/client.go`, mirror the request/response field on `CreateNotificationRequest` and any response types (e.g., the `Notification`-shaped response used by `ListNotifications`/`GetNotification`).

**Step 5: Add handler test**

In whichever file currently houses notification handler tests (search for `TestCreateNotification` in `internal/server/`), add a test asserting that `MissionID` from request roundtrips through the API.

**Step 6: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)

Expected: all green.

**Step 7: Commit**

```bash
git add internal/server/notifications_handlers.go internal/server/client.go internal/server/<test-file>.go
git commit -m "Plumb mission_id through notifications API"
git pull --rebase && git push
```

---

Task 4: `agenc notifications create --mission-id` flag
-------------------------------------------------------

**Files:**
- Modify: `cmd/notifications_create.go`

**Step 1: Add the flag**

Add `--mission-id` to the cobra command:

```go
notificationsCreateCmd.Flags().String(missionIDFlagName, "", "link this notification to a mission (UUID or short ID)")
```

If `missionIDFlagName` doesn't exist in `cmd/command_str_consts.go`, add it.

**Step 2: Resolve and pass through**

In the command's `RunE`, if the flag is non-empty, resolve via `client.ResolveMissionID(value)` (look up the existing pattern — there's a server endpoint or client helper). Pass the resolved full UUID to the `CreateNotification` request as `MissionID`.

**Step 3: Manual smoke**

```bash
make build  # with dangerouslyDisableSandbox: true
./_build/agenc-test notifications create --kind=test --title=hi --mission-id=<some-mission-short-id> --body=body
./_build/agenc-test notifications ls --kind=test
```

Expected: notification appears in the list. (`notifications ls` doesn't yet display mission_id; that's not a regression — manage picker is the consumer.)

**Step 4: Commit**

```bash
git add cmd/notifications_create.go cmd/command_str_consts.go
git commit -m "Add --mission-id flag to agenc notifications create"
git pull --rebase && git push
```

---

Task 5: Title sanitizer helper
-------------------------------

**Files:**
- Create: `internal/server/notifications_helpers.go`
- Test: `internal/server/notifications_helpers_test.go`

**Step 1: Write the failing test**

```go
package server

import "testing"

func TestSanitizeNotificationTitle_StripsControlChars(t *testing.T) {
    got := sanitizeNotificationTitle("hello\nworld\ttabbed\rcr")
    want := "hello world tabbed cr"
    if got != want {
        t.Fatalf("got %q, want %q", got, want)
    }
}

func TestSanitizeNotificationTitle_StripsANSI(t *testing.T) {
    got := sanitizeNotificationTitle("\x1b[31mred\x1b[0m text")
    want := "red text"
    if got != want {
        t.Fatalf("got %q, want %q", got, want)
    }
}

func TestSanitizeNotificationTitle_TruncatesLongInput(t *testing.T) {
    in := strings.Repeat("a", 500)
    got := sanitizeNotificationTitle(in)
    if len(got) > 200 {
        t.Fatalf("expected <=200 runes, got %d", len(got))
    }
}
```

**Step 2: Run — confirm failure**

Run: `go test ./internal/server/ -run TestSanitizeNotificationTitle` (with `dangerouslyDisableSandbox: true`)

Expected: FAIL with "undefined: sanitizeNotificationTitle".

**Step 3: Implement minimally**

```go
package server

import (
    "regexp"
    "strings"
)

var ansiRegexp = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// sanitizeNotificationTitle strips control characters and ANSI sequences,
// collapses runs of whitespace to single spaces, and truncates to 200 chars.
// Used at the cron-notification write site as defense-in-depth — cron names
// are user-edited config strings.
func sanitizeNotificationTitle(s string) string {
    s = ansiRegexp.ReplaceAllString(s, "")
    s = strings.Map(func(r rune) rune {
        if r == '\n' || r == '\r' || r == '\t' {
            return ' '
        }
        return r
    }, s)
    if len(s) > 200 {
        s = s[:200]
    }
    return s
}
```

**Step 4: Run — confirm pass**

Run: `go test ./internal/server/ -run TestSanitizeNotificationTitle -v`

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/server/notifications_helpers.go internal/server/notifications_helpers_test.go
git commit -m "Add sanitizeNotificationTitle helper"
git pull --rebase && git push
```

---

Task 6: Cron auto-notification in `handleCreateMission`
--------------------------------------------------------

**Files:**
- Modify: `internal/server/missions.go` (`handleCreateMission`)
- Test: `internal/server/missions_test.go`

**Step 1: Write the failing tests**

In `internal/server/missions_test.go` add (use whatever test setup helper that file already uses — search for an existing `TestHandleCreateMission` for the boilerplate):

```go
func TestHandleCreateMission_CronSourceCreatesNotification(t *testing.T) {
    // Standard server test setup ...
    body := `{"prompt":"do the thing","headless":true,"source":"cron","source_id":"cron-id-1","source_metadata":"{\"cron_name\":\"daily-review\"}"}`
    // POST /missions, decode response, capture mission ID ...

    notifs, err := s.db.ListNotifications(database.ListNotificationsParams{Kind: "cron.triggered"})
    // assert exactly one, MissionID == created mission ID, Title contains "daily-review"
}

func TestHandleCreateMission_NonCronSourceNoNotification(t *testing.T) {
    // POST with source="" or source="user" ...
    notifs, _ := s.db.ListNotifications(database.ListNotificationsParams{Kind: "cron.triggered"})
    if len(notifs) != 0 {
        t.Fatalf("expected no cron.triggered notifications, got %d", len(notifs))
    }
}

func TestHandleCreateMission_CronWithMalformedMetadata(t *testing.T) {
    // source_metadata="{not valid json"
    // assert notification created and Title contains source_id (fallback)
}

func TestHandleCreateMission_CronWithMissingCronName(t *testing.T) {
    // source_metadata=`{"trigger":"manual"}` — valid JSON, no cron_name key
    // assert notification created and Title contains source_id (fallback)
}
```

**Step 2: Run — confirm failure**

Run: `go test ./internal/server/ -run TestHandleCreateMission_Cron -v` (with `dangerouslyDisableSandbox: true`)

Expected: FAIL — no notification is created.

**Step 3: Implement the cron-notification branch**

In `handleCreateMission` (`internal/server/missions.go`), AFTER `s.spawnWrapper(...)` and BEFORE `writeJSON(...)`, insert:

```go
if req.Source == "cron" {
    s.createCronTriggeredNotification(missionRecord, req)
}
```

Add the helper at the bottom of `missions.go`:

```go
// createCronTriggeredNotification is best-effort: failures are logged and
// the mission request still succeeds.
func (s *Server) createCronTriggeredNotification(missionRecord *database.Mission, req CreateMissionRequest) {
    cronName, trigger := parseCronMetadata(req.SourceMetadata)
    titleSubject := cronName
    if titleSubject == "" {
        titleSubject = req.SourceID
    }
    title := sanitizeNotificationTitle("Cron triggered: " + titleSubject)

    var bodyParts []string
    if cronName != "" {
        bodyParts = append(bodyParts, "**Cron:** "+cronName)
    }
    if req.SourceID != "" {
        bodyParts = append(bodyParts, "**Cron ID:** "+req.SourceID)
    }
    triggerLabel := "scheduled"
    if trigger == "manual" {
        triggerLabel = "manual"
    }
    bodyParts = append(bodyParts, "**Trigger:** "+triggerLabel)
    bodyParts = append(bodyParts, "**Mission:** "+missionRecord.ShortID)
    if missionRecord.GitRepo != "" {
        bodyParts = append(bodyParts, "**Repo:** "+missionRecord.GitRepo)
    }
    if req.Prompt != "" {
        preview := req.Prompt
        if len(preview) > 200 {
            preview = preview[:200] + "…"
        }
        bodyParts = append(bodyParts, "**Prompt:**\n\n"+preview)
    }
    body := strings.Join(bodyParts, "\n\n")

    missionID := missionRecord.ID
    n := &database.Notification{
        ID:           uuid.New().String(),
        Kind:         "cron.triggered",
        Title:        title,
        BodyMarkdown: body,
        MissionID:    &missionID,
    }
    if err := s.db.CreateNotification(n); err != nil {
        s.logger.Printf("failed to create cron-triggered notification for mission %s: %v", missionRecord.ShortID, err)
    }
}

// parseCronMetadata extracts cron_name and trigger fields from source_metadata.
// Returns ("", "") when metadata is empty or malformed; the caller falls back
// to source_id for the title.
func parseCronMetadata(metadataJSON string) (cronName, trigger string) {
    if metadataJSON == "" {
        return "", ""
    }
    var m map[string]string
    if err := json.Unmarshal([]byte(metadataJSON), &m); err != nil {
        return "", ""
    }
    return m["cron_name"], m["trigger"]
}
```

**Step 4: Run — confirm pass**

Run: `go test ./internal/server/ -run TestHandleCreateMission_Cron -v`

Expected: PASS for all four tests.

**Step 5: Run full check**

Run: `make check`

Expected: all green.

**Step 6: Commit**

```bash
git add internal/server/missions.go internal/server/missions_test.go
git commit -m "Auto-create cron.triggered notification on cron-spawned missions"
git pull --rebase && git push
```

---

Task 7: `agenc notifications manage` CLI
-----------------------------------------

**Files:**
- Create: `cmd/notifications_manage.go`
- Modify: `cmd/command_str_consts.go` (add `manageCmdStr` if not present)

**Step 1: Write the empty-list smoke test**

Add to `scripts/e2e-test.sh` (under a `--- Notifications Manage ---` section):

```bash
run_test_output_contains "notifications manage with no notifications prints empty message" \
    "No notifications" \
    "${agenc_test}" notifications manage
```

This must fail today because the command doesn't exist.

**Step 2: Implement the command**

Create `cmd/notifications_manage.go`. Skeleton:

```go
package cmd

import (
    "fmt"
    "os"
    "os/exec"
    "strings"

    "github.com/mattn/go-isatty"
    "github.com/mieubrisse/stacktrace"
    "github.com/spf13/cobra"

    "github.com/odyssey/agenc/internal/server"
    "github.com/odyssey/agenc/internal/tableprinter"
)

var notificationsManageCmd = &cobra.Command{
    Use:   "manage",
    Short: "Interactive notification picker — ENTER attaches to the linked mission",
    Long: `Open the Notification Center: an fzf picker over all notifications,
sorted by recency, with a preview pane for the body. Press ENTER on a row to
attach to its linked mission. Notifications without a linked mission are not
actionable from this view.`,
    Args: cobra.NoArgs,
    RunE: runNotificationsManage,
}

func init() {
    notificationsCmd.AddCommand(notificationsManageCmd)
}

func runNotificationsManage(cmd *cobra.Command, args []string) error {
    client, err := serverClient()
    if err != nil {
        return err
    }

    notifs, err := client.ListNotifications(server.ListNotificationsRequest{})
    if err != nil {
        return stacktrace.Propagate(err, "failed to list notifications")
    }
    if len(notifs) == 0 {
        fmt.Println("No notifications. Try scheduling a cron — see `agenc cron`.")
        return nil
    }

    if !isatty.IsTerminal(os.Stdin.Fd()) {
        return stacktrace.NewError("notifications manage requires an interactive terminal")
    }
    if _, err := exec.LookPath("fzf"); err != nil {
        return stacktrace.NewError("'fzf' binary not found in PATH")
    }

    fzfInput, shortIDByLine := buildManageFzfInput(notifs)

    execPath, err := os.Executable()
    if err != nil {
        return stacktrace.Propagate(err, "failed to get executable path")
    }

    fzfArgs := []string{
        "--ansi",
        "--with-nth", "2..",
        "--header-lines", "1",
        "--header", "ENTER attach │ ESC cancel",
        "--prompt", "Notification Center > ",
        "--preview", fmt.Sprintf("%s notifications show {1}", execPath),
        "--preview-window", "right:60%:wrap",
    }

    fzfCmd := exec.Command("fzf", fzfArgs...)
    fzfCmd.Stdin = strings.NewReader(fzfInput)
    fzfCmd.Stderr = os.Stderr

    output, err := fzfCmd.Output()
    if err != nil {
        if exitErr, ok := err.(*exec.ExitError); ok {
            switch exitErr.ExitCode() {
            case 1, 130:
                return nil // user cancelled
            }
        }
        return stacktrace.Propagate(err, "fzf selection failed")
    }

    selected := strings.TrimSpace(string(output))
    if selected == "" {
        return nil
    }
    shortID, _, _ := strings.Cut(selected, "\t")
    shortID = strings.TrimSpace(shortID)

    notif, err := client.GetNotification(shortID)
    if err != nil {
        return stacktrace.Propagate(err, "failed to fetch notification %s", shortID)
    }
    if notif.MissionID == "" {
        fmt.Println("Notification has no linked mission.")
        return nil
    }

    attachCmd := exec.Command(execPath, "mission", "attach", notif.MissionID)
    attachCmd.Stdin = os.Stdin
    attachCmd.Stdout = os.Stdout
    attachCmd.Stderr = os.Stderr
    return attachCmd.Run()

    _ = shortIDByLine // currently unused; leave hook in place for future read/unread
}

func buildManageFzfInput(notifs []server.NotificationResponse) (string, map[string]string) {
    var buf strings.Builder
    tbl := tableprinter.NewTable("ID", "Created", "Kind", "Mission", "Title").WithWriter(&buf)
    shortIDs := make([]string, 0, len(notifs))
    shortIDByLine := make(map[string]string, len(notifs))
    for _, n := range notifs {
        shortID := n.ID
        if len(shortID) > 8 {
            shortID = shortID[:8]
        }
        missionCell := "—"
        if n.MissionID != "" {
            mShort := n.MissionID
            if len(mShort) > 8 {
                mShort = mShort[:8]
            }
            missionCell = "\x1b[36m" + mShort + "\x1b[0m"
        }
        tbl.AddRow(shortID, n.CreatedAt, n.Kind, missionCell, n.Title)
        shortIDs = append(shortIDs, shortID)
        shortIDByLine[shortID] = n.ID
    }
    tbl.Print()

    lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
    var out strings.Builder
    // header line — fzf treats the first line as header (--header-lines 1)
    out.WriteString("HEADER\t")
    out.WriteString(lines[0])
    out.WriteByte('\n')
    for i, line := range lines[1:] {
        out.WriteString(shortIDs[i])
        out.WriteByte('\t')
        out.WriteString(line)
        out.WriteByte('\n')
    }
    return out.String(), shortIDByLine
}
```

Notes:
- The hidden first column is the notification short ID; `--with-nth 2..` hides it from display, while `{1}` in the preview command pulls it out.
- The hardcoded `\x1b[36m` cyan on the mission cell makes ENTER-eligible rows visually pop. ANSI is enabled via `--ansi`.
- Cancel paths (ESC, no match) return nil, not an error — matches existing fzf helpers.

**Step 3: Build, run E2E**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Run: `make e2e` (with `dangerouslyDisableSandbox: true`)

Expected: `notifications manage with no notifications` test passes.

**Step 4: Manual smoke**

```bash
./_build/agenc-test notifications create --kind=test --title="manual smoke" --body="hello"
./_build/agenc-test notifications manage
```

Expected: fzf opens with one row; preview pane shows the body; ENTER prints "Notification has no linked mission" (since none was set); ESC exits cleanly.

**Step 5: Commit**

```bash
git add cmd/notifications_manage.go cmd/command_str_consts.go scripts/e2e-test.sh
git commit -m "Add agenc notifications manage interactive picker"
git pull --rebase && git push
```

---

Task 8: Tmux palette — replace "Show Notifications" with "Notification Center"
-------------------------------------------------------------------------------

**Files:**
- Modify: `internal/config/agenc_config.go` (default palette entries)
- Modify: `cmd/tmux_palette.go` (banner text)

**Step 1: Update the default palette entry**

In `internal/config/agenc_config.go`, locate the entry whose Title is `"🔔  Show Notifications"` (line ~93). Replace it with:

```go
{
    Title:       StringPtr("🔔  Notification Center"),
    Description: StringPtr("Browse notifications and attach to linked missions"),
    Command:     StringPtr("agenc notifications manage"),
    // (preserve the same Hidden/Group fields the prior entry had)
},
```

**Step 2: Update the banner**

In `cmd/tmux_palette.go` line ~319, change:

```go
return fmt.Sprintf("\x1b[33m⚠ %d unread %s — pick \"Show Notifications\" to review\x1b[0m", count, noun)
```

to:

```go
return fmt.Sprintf("\x1b[33m⚠ %d unread %s — pick \"Notification Center\" to review\x1b[0m", count, noun)
```

**Step 3: Verify config tests still pass**

Run: `make check` (with `dangerouslyDisableSandbox: true`)

Expected: all green. If `agenc_config_test.go` snapshots the default palette, update the snapshot.

**Step 4: Manual verification (per CLAUDE.md tmux-integration rule)**

This step requires a live tmux session. Tell the user:

> 🚨 **MANUAL TEST NEEDED** 🚨
>
> 1. Open the tmux command palette (its keybinding)
> 2. Confirm the "🔔 Notification Center" entry is present and "Show Notifications" is gone
> 3. Pick the entry and confirm `notifications manage` opens in a popup

**Step 5: Commit**

```bash
git add internal/config/agenc_config.go cmd/tmux_palette.go internal/config/agenc_config_test.go
git commit -m "Replace tmux palette 'Show Notifications' with 'Notification Center'"
git pull --rebase && git push
```

---

Task 9: E2E — cron run creates a notification with mission_id
--------------------------------------------------------------

**Files:**
- Modify: `scripts/e2e-test.sh`

**Step 1: Add the test**

Under the existing cron section (or a new `--- Cron Notifications ---` section), add:

```bash
run_test "cron new succeeds" \
    0 \
    "${agenc_test}" cron new --name "e2e-notif-test" --schedule "0 0 * * *" --prompt "test prompt"

# Manually trigger and wait for the mission to be created
"${agenc_test}" cron run e2e-notif-test || true

# Verify a cron.triggered notification exists
run_test_output_contains "cron run creates cron.triggered notification" \
    "e2e-notif-test" \
    "${agenc_test}" notifications ls --kind cron.triggered
```

If `agenc cron new` requires additional flags or has a different schedule format in this codebase, adjust accordingly — the goal is "create a cron, run it, verify a notification exists with kind=cron.triggered."

**Step 2: Run E2E**

Run: `make e2e` (with `dangerouslyDisableSandbox: true`)

Expected: the new tests pass alongside the existing suite.

**Step 3: Commit**

```bash
git add scripts/e2e-test.sh
git commit -m "Add E2E test verifying cron run creates a notification"
git pull --rebase && git push
```

---

Task 10: Architecture doc update
---------------------------------

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Update the doc**

Per project CLAUDE.md, the architecture doc must describe schema changes and key patterns. Add a brief note to the `notifications` schema entry mentioning the `mission_id` column, and add a one-line note to the cron section describing the auto-notification side effect.

Keep it at the file-path/component level — no code snippets.

**Step 2: Commit**

```bash
git add docs/system-architecture.md
git commit -m "Document mission_id and cron auto-notification in architecture doc"
git pull --rebase && git push
```

---

Final verification
------------------

After Task 10, run a full sweep:

```bash
make check     # with dangerouslyDisableSandbox: true
make e2e       # with dangerouslyDisableSandbox: true
```

Both must be green. Then mark `agenc-wfdb` partially complete with a note about which half remains:

```bash
bd update agenc-wfdb --notes "Notifications half shipped (cron auto-notify + manage picker). EOD-review cron is the remaining half — track in a follow-up bead."
```

(Don't close `agenc-wfdb` yet — the EOD-review cron is its other half.)

---

What is intentionally NOT in this plan
---------------------------------------

- `agenc notifications unread` CLI / `MarkNotificationUnread` DB function.
- `r` / `u` keybinds in fzf (path is documented in the design doc).
- Multi-select bulk actions.
- The EOD-review cron itself.
- Filtering/searching by kind in the picker.
- TUI alternative.
- Updating other notification creation sites (writeable-copy, etc.) to populate `mission_id` — none of them have a linked mission today; revisit if/when they do.
