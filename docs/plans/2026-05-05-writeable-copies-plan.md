# Writeable Copies & Notifications Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a "writeable copy" concept to AgenC — a user-chosen path that hosts an additional clone of a repo, kept continuously synced with the same git remote (auto-commit + push on local edits, pull-and-rebase on remote changes). Surface sync conflicts and other agent-authored events through a new append-only notifications subsystem with CLI access and Adjutant integration.

**Architecture:** A new server-side reconcile worker drives writeable-copy sync. Three triggers feed it (working-tree fsnotify with 15s debounce, library-update fan-out, server startup). Conflicts halt the loop via a persisted pause row in DB plus an append-only notification, atomically inserted. Notifications are stored as Markdown rows; the body is sanitized of ANSI escapes only at display time. The repo library remains the master and only source for missions; writeable copies are peers that synchronize via the shared git remote. Full design at `docs/plans/2026-05-05-writeable-copies-design.md`.

**Tech Stack:** Go, SQLite (with WAL), Cobra CLI, fsnotify, unix-socket HTTP, tmux command palette.

**Sandbox note:** `make build` and `make check` require `dangerouslyDisableSandbox: true` per `CLAUDE.md`.

---

### Task 1: Database migration — notifications table

**Files:**
- Modify: `internal/database/migrations.go` (add SQL constants + migration step)

**Step 1: Add SQL constants**

Append to the SQL constant block in `internal/database/migrations.go`:

```go
const (
    createNotificationsTableSQL = `CREATE TABLE IF NOT EXISTS notifications (
    id              TEXT    PRIMARY KEY,
    kind            TEXT    NOT NULL,
    source_repo     TEXT,
    title           TEXT    NOT NULL,
    body_markdown   TEXT    NOT NULL,
    created_at      TEXT    NOT NULL,
    read_at         TEXT
);`
    createNotificationsUnreadIndexSQL = `CREATE INDEX IF NOT EXISTS idx_notifications_unread ON notifications(read_at) WHERE read_at IS NULL;`
)
```

**Step 2: Add migration step**

Find `getMigrationSteps()` and append:

```go
{description: "Create notifications table", run: func(conn *sql.DB) error {
    if _, err := conn.Exec(createNotificationsTableSQL); err != nil {
        return stacktrace.Propagate(err, "failed to create notifications table")
    }
    if _, err := conn.Exec(createNotificationsUnreadIndexSQL); err != nil {
        return stacktrace.Propagate(err, "failed to create notifications unread index")
    }
    return nil
}},
```

**Step 3: Build**

Run with `dangerouslyDisableSandbox: true`: `make build`
Expected: compiles cleanly.

**Step 4: Commit**

```
git add internal/database/migrations.go
git commit -m "Add notifications table migration"
```

---

### Task 2: Database migration — writeable_copy_pauses table

**Files:**
- Modify: `internal/database/migrations.go`

**Step 1: Add SQL constant**

```go
const createWriteableCopyPausesTableSQL = `CREATE TABLE IF NOT EXISTS writeable_copy_pauses (
    repo_name              TEXT    PRIMARY KEY,
    paused_at              TEXT    NOT NULL,
    paused_reason          TEXT    NOT NULL,
    local_head_at_pause    TEXT    NOT NULL,
    notification_id        TEXT    NOT NULL REFERENCES notifications(id)
);`
```

**Step 2: Append migration step** — same pattern as Task 1, with description "Create writeable_copy_pauses table".

**Step 3: Build with sandbox disabled.** Verify clean compile.

**Step 4: Commit**

```
git add internal/database/migrations.go
git commit -m "Add writeable_copy_pauses table migration"
```

---

### Task 3: Database — Notification model + CRUD

**Files:**
- Create: `internal/database/notifications.go`
- Create: `internal/database/notifications_test.go`

**Step 1: Write failing tests**

Create `internal/database/notifications_test.go`:

```go
package database

import (
    "testing"
    "time"
)

func TestCreateNotification(t *testing.T) {
    db := openTestDB(t)

    n := &Notification{
        ID:           "11111111-2222-3333-4444-555555555555",
        Kind:         "writeable_copy.conflict",
        SourceRepo:   "github.com/owner/repo",
        Title:        "Test",
        BodyMarkdown: "# Hello",
    }
    if err := db.CreateNotification(n); err != nil {
        t.Fatalf("CreateNotification failed: %v", err)
    }

    got, err := db.GetNotification(n.ID)
    if err != nil {
        t.Fatalf("GetNotification failed: %v", err)
    }
    if got.Title != "Test" || got.BodyMarkdown != "# Hello" || got.ReadAt != nil {
        t.Errorf("unexpected notification: %+v", got)
    }
}

func TestListNotifications_UnreadOnly(t *testing.T) {
    db := openTestDB(t)

    unread := &Notification{ID: "aaaaaaaa-0000-0000-0000-000000000001", Kind: "k", Title: "u", BodyMarkdown: "u"}
    read := &Notification{ID: "bbbbbbbb-0000-0000-0000-000000000002", Kind: "k", Title: "r", BodyMarkdown: "r"}
    if err := db.CreateNotification(unread); err != nil {
        t.Fatal(err)
    }
    if err := db.CreateNotification(read); err != nil {
        t.Fatal(err)
    }
    if err := db.MarkNotificationRead(read.ID); err != nil {
        t.Fatal(err)
    }

    list, err := db.ListNotifications(ListNotificationsParams{UnreadOnly: true})
    if err != nil {
        t.Fatal(err)
    }
    if len(list) != 1 || list[0].ID != unread.ID {
        t.Errorf("expected only unread, got %+v", list)
    }
}

func TestMarkNotificationRead_Idempotent(t *testing.T) {
    db := openTestDB(t)
    n := &Notification{ID: "cccccccc-0000-0000-0000-000000000003", Kind: "k", Title: "x", BodyMarkdown: "x"}
    if err := db.CreateNotification(n); err != nil {
        t.Fatal(err)
    }
    if err := db.MarkNotificationRead(n.ID); err != nil {
        t.Fatalf("first mark failed: %v", err)
    }
    first, _ := db.GetNotification(n.ID)
    time.Sleep(10 * time.Millisecond)
    if err := db.MarkNotificationRead(n.ID); err != nil {
        t.Fatalf("second mark failed: %v", err)
    }
    second, _ := db.GetNotification(n.ID)
    if !first.ReadAt.Equal(*second.ReadAt) {
        t.Errorf("read_at changed on idempotent mark: %v != %v", first.ReadAt, second.ReadAt)
    }
}
```

**Step 2: Run tests — expect compile failure** (no Notification type yet)

Run with `dangerouslyDisableSandbox: true`: `go test ./internal/database/ -run TestCreateNotification -v`
Expected: fails to compile.

**Step 3: Implement**

Create `internal/database/notifications.go`:

```go
package database

import (
    "database/sql"
    "time"

    "github.com/mieubrisse/stacktrace"
)

type Notification struct {
    ID           string
    Kind         string
    SourceRepo   string
    Title        string
    BodyMarkdown string
    CreatedAt    time.Time
    ReadAt       *time.Time
}

type ListNotificationsParams struct {
    UnreadOnly bool
    SourceRepo string
    Kind       string
}

func (db *DB) CreateNotification(n *Notification) error {
    if n.CreatedAt.IsZero() {
        n.CreatedAt = time.Now().UTC()
    }
    var sourceRepo sql.NullString
    if n.SourceRepo != "" {
        sourceRepo = sql.NullString{String: n.SourceRepo, Valid: true}
    }
    _, err := db.conn.Exec(
        "INSERT INTO notifications (id, kind, source_repo, title, body_markdown, created_at, read_at) VALUES (?, ?, ?, ?, ?, ?, NULL)",
        n.ID, n.Kind, sourceRepo, n.Title, n.BodyMarkdown, n.CreatedAt.Format(time.RFC3339Nano),
    )
    if err != nil {
        return stacktrace.Propagate(err, "failed to insert notification")
    }
    return nil
}

func (db *DB) GetNotification(id string) (*Notification, error) {
    row := db.conn.QueryRow("SELECT id, kind, source_repo, title, body_markdown, created_at, read_at FROM notifications WHERE id = ?", id)
    return scanNotification(row)
}

func (db *DB) ListNotifications(params ListNotificationsParams) ([]*Notification, error) {
    query := "SELECT id, kind, source_repo, title, body_markdown, created_at, read_at FROM notifications"
    args := []any{}
    conds := []string{}
    if params.UnreadOnly {
        conds = append(conds, "read_at IS NULL")
    }
    if params.SourceRepo != "" {
        conds = append(conds, "source_repo = ?")
        args = append(args, params.SourceRepo)
    }
    if params.Kind != "" {
        conds = append(conds, "kind = ?")
        args = append(args, params.Kind)
    }
    if len(conds) > 0 {
        query += " WHERE " + joinAnd(conds)
    }
    query += " ORDER BY created_at DESC"

    rows, err := db.conn.Query(query, args...)
    if err != nil {
        return nil, stacktrace.Propagate(err, "failed to list notifications")
    }
    defer rows.Close()

    var out []*Notification
    for rows.Next() {
        n, err := scanNotification(rows)
        if err != nil {
            return nil, err
        }
        out = append(out, n)
    }
    return out, nil
}

func (db *DB) MarkNotificationRead(id string) error {
    // Idempotent: only set read_at if currently NULL
    _, err := db.conn.Exec(
        "UPDATE notifications SET read_at = ? WHERE id = ? AND read_at IS NULL",
        time.Now().UTC().Format(time.RFC3339Nano), id,
    )
    if err != nil {
        return stacktrace.Propagate(err, "failed to mark notification read")
    }
    return nil
}

type scannerLike interface {
    Scan(...any) error
}

func scanNotification(s scannerLike) (*Notification, error) {
    var n Notification
    var sourceRepo sql.NullString
    var createdAt string
    var readAt sql.NullString
    err := s.Scan(&n.ID, &n.Kind, &sourceRepo, &n.Title, &n.BodyMarkdown, &createdAt, &readAt)
    if err != nil {
        if err == sql.ErrNoRows {
            return nil, stacktrace.NewError("notification not found")
        }
        return nil, stacktrace.Propagate(err, "failed to scan notification row")
    }
    n.SourceRepo = sourceRepo.String
    if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
        n.CreatedAt = t
    }
    if readAt.Valid {
        if t, err := time.Parse(time.RFC3339Nano, readAt.String); err == nil {
            n.ReadAt = &t
        }
    }
    return &n, nil
}

func joinAnd(parts []string) string {
    out := ""
    for i, p := range parts {
        if i > 0 {
            out += " AND "
        }
        out += p
    }
    return out
}
```

**Step 4: Run tests — expect pass**

Run with sandbox disabled: `go test ./internal/database/ -run "TestCreateNotification|TestListNotifications_UnreadOnly|TestMarkNotificationRead_Idempotent" -v`
Expected: PASS.

**Step 5: Commit**

```
git add internal/database/notifications.go internal/database/notifications_test.go
git commit -m "Add notifications database CRUD"
```

---

### Task 4: Database — WriteableCopyPause CRUD + atomic insert helper

**Files:**
- Create: `internal/database/writeable_copy_pauses.go`
- Create: `internal/database/writeable_copy_pauses_test.go`

**Step 1: Write failing tests**

```go
package database

import "testing"

func TestUpsertPauseAndNotificationAtomic(t *testing.T) {
    db := openTestDB(t)

    n := &Notification{ID: "11111111-0000-0000-0000-000000000001", Kind: "writeable_copy.conflict", Title: "x", BodyMarkdown: "x"}
    p := &WriteableCopyPause{
        RepoName:           "github.com/owner/repo",
        PausedReason:       "rebase_conflict",
        LocalHeadAtPause:   "abc123",
        NotificationID:     n.ID,
    }

    inserted, err := db.UpsertPauseAndNotification(p, n)
    if err != nil {
        t.Fatalf("first upsert failed: %v", err)
    }
    if !inserted {
        t.Errorf("expected first upsert to insert")
    }

    // Second call with same repo → no-op
    n2 := &Notification{ID: "22222222-0000-0000-0000-000000000002", Kind: "writeable_copy.conflict", Title: "y", BodyMarkdown: "y"}
    p2 := &WriteableCopyPause{RepoName: p.RepoName, PausedReason: "rebase_conflict", LocalHeadAtPause: "def456", NotificationID: n2.ID}
    inserted2, err := db.UpsertPauseAndNotification(p2, n2)
    if err != nil {
        t.Fatalf("second upsert failed: %v", err)
    }
    if inserted2 {
        t.Errorf("expected second upsert to be a no-op")
    }

    list, _ := db.ListNotifications(ListNotificationsParams{})
    if len(list) != 1 {
        t.Errorf("expected exactly one notification, got %d", len(list))
    }
}

func TestDeletePause(t *testing.T) {
    db := openTestDB(t)
    n := &Notification{ID: "33333333-0000-0000-0000-000000000003", Kind: "k", Title: "x", BodyMarkdown: "x"}
    p := &WriteableCopyPause{RepoName: "r", PausedReason: "x", LocalHeadAtPause: "h", NotificationID: n.ID}
    db.UpsertPauseAndNotification(p, n)

    if err := db.DeletePause("r"); err != nil {
        t.Fatal(err)
    }
    got, _ := db.GetPause("r")
    if got != nil {
        t.Errorf("expected pause cleared, got %+v", got)
    }
    // Notification should remain (append-only)
    notif, _ := db.GetNotification(n.ID)
    if notif == nil {
        t.Errorf("notification should not have been deleted")
    }
}
```

**Step 2: Run — expect compile failure.**

**Step 3: Implement**

Create `internal/database/writeable_copy_pauses.go`:

```go
package database

import (
    "database/sql"
    "time"

    "github.com/mieubrisse/stacktrace"
)

type WriteableCopyPause struct {
    RepoName         string
    PausedAt         time.Time
    PausedReason     string
    LocalHeadAtPause string
    NotificationID   string
}

// UpsertPauseAndNotification inserts a pause row and notification atomically.
// Returns (true, nil) if both were inserted, (false, nil) if a pause already
// existed for the repo (no-op — neither pause nor notification is written).
func (db *DB) UpsertPauseAndNotification(p *WriteableCopyPause, n *Notification) (bool, error) {
    tx, err := db.conn.Begin()
    if err != nil {
        return false, stacktrace.Propagate(err, "failed to begin transaction")
    }
    defer tx.Rollback()

    var existing string
    err = tx.QueryRow("SELECT repo_name FROM writeable_copy_pauses WHERE repo_name = ?", p.RepoName).Scan(&existing)
    if err == nil {
        return false, nil // already paused; skip
    }
    if err != sql.ErrNoRows {
        return false, stacktrace.Propagate(err, "failed to check for existing pause")
    }

    if n.CreatedAt.IsZero() {
        n.CreatedAt = time.Now().UTC()
    }
    var sourceRepo sql.NullString
    if n.SourceRepo != "" {
        sourceRepo = sql.NullString{String: n.SourceRepo, Valid: true}
    }
    if _, err := tx.Exec(
        "INSERT INTO notifications (id, kind, source_repo, title, body_markdown, created_at, read_at) VALUES (?, ?, ?, ?, ?, ?, NULL)",
        n.ID, n.Kind, sourceRepo, n.Title, n.BodyMarkdown, n.CreatedAt.Format(time.RFC3339Nano),
    ); err != nil {
        return false, stacktrace.Propagate(err, "failed to insert notification")
    }

    if p.PausedAt.IsZero() {
        p.PausedAt = time.Now().UTC()
    }
    if _, err := tx.Exec(
        "INSERT INTO writeable_copy_pauses (repo_name, paused_at, paused_reason, local_head_at_pause, notification_id) VALUES (?, ?, ?, ?, ?)",
        p.RepoName, p.PausedAt.Format(time.RFC3339Nano), p.PausedReason, p.LocalHeadAtPause, p.NotificationID,
    ); err != nil {
        return false, stacktrace.Propagate(err, "failed to insert pause")
    }

    if err := tx.Commit(); err != nil {
        return false, stacktrace.Propagate(err, "failed to commit pause transaction")
    }
    return true, nil
}

func (db *DB) GetPause(repoName string) (*WriteableCopyPause, error) {
    row := db.conn.QueryRow(
        "SELECT repo_name, paused_at, paused_reason, local_head_at_pause, notification_id FROM writeable_copy_pauses WHERE repo_name = ?",
        repoName,
    )
    var p WriteableCopyPause
    var pausedAt string
    err := row.Scan(&p.RepoName, &pausedAt, &p.PausedReason, &p.LocalHeadAtPause, &p.NotificationID)
    if err == sql.ErrNoRows {
        return nil, nil
    }
    if err != nil {
        return nil, stacktrace.Propagate(err, "failed to get pause")
    }
    if t, err := time.Parse(time.RFC3339Nano, pausedAt); err == nil {
        p.PausedAt = t
    }
    return &p, nil
}

func (db *DB) ListPauses() ([]*WriteableCopyPause, error) {
    rows, err := db.conn.Query("SELECT repo_name, paused_at, paused_reason, local_head_at_pause, notification_id FROM writeable_copy_pauses ORDER BY paused_at DESC")
    if err != nil {
        return nil, stacktrace.Propagate(err, "failed to list pauses")
    }
    defer rows.Close()
    var out []*WriteableCopyPause
    for rows.Next() {
        var p WriteableCopyPause
        var pausedAt string
        if err := rows.Scan(&p.RepoName, &pausedAt, &p.PausedReason, &p.LocalHeadAtPause, &p.NotificationID); err != nil {
            return nil, stacktrace.Propagate(err, "failed to scan pause row")
        }
        if t, err := time.Parse(time.RFC3339Nano, pausedAt); err == nil {
            p.PausedAt = t
        }
        out = append(out, &p)
    }
    return out, nil
}

func (db *DB) DeletePause(repoName string) error {
    _, err := db.conn.Exec("DELETE FROM writeable_copy_pauses WHERE repo_name = ?", repoName)
    if err != nil {
        return stacktrace.Propagate(err, "failed to delete pause")
    }
    return nil
}
```

**Step 4: Run tests — expect PASS.**

`go test ./internal/database/ -run "TestUpsertPauseAndNotificationAtomic|TestDeletePause" -v` (sandbox disabled)

**Step 5: Commit**

```
git add internal/database/writeable_copy_pauses.go internal/database/writeable_copy_pauses_test.go
git commit -m "Add writeable_copy_pauses CRUD with atomic notification insert"
```

---

### Task 5: Config — `WriteableCopy` field on RepoConfig + helpers

**Files:**
- Modify: `internal/config/agenc_config.go` (RepoConfig struct, near `AlwaysSynced` field)
- Modify: `internal/config/agenc_config_test.go`

**Step 1: Write failing test for accessor + coercion**

Append to `internal/config/agenc_config_test.go`:

```go
func TestWriteableCopyImpliesAlwaysSynced(t *testing.T) {
    cfg := &AgencConfig{
        RepoConfigs: map[string]*RepoConfig{
            "github.com/o/r": {WriteableCopy: "/tmp/foo", AlwaysSynced: false},
        },
    }
    cfg.NormalizeRepoConfigs()
    if !cfg.RepoConfigs["github.com/o/r"].AlwaysSynced {
        t.Error("expected AlwaysSynced to be coerced to true when WriteableCopy is set")
    }
}

func TestGetWriteableCopy(t *testing.T) {
    cfg := &AgencConfig{
        RepoConfigs: map[string]*RepoConfig{
            "github.com/o/r": {WriteableCopy: "/tmp/foo"},
            "github.com/o/q": {},
        },
    }
    if got, ok := cfg.GetWriteableCopy("github.com/o/r"); !ok || got != "/tmp/foo" {
        t.Errorf("expected /tmp/foo, got %q ok=%v", got, ok)
    }
    if _, ok := cfg.GetWriteableCopy("github.com/o/q"); ok {
        t.Error("expected no writeable copy for repo without one")
    }
}
```

**Step 2: Run — expect compile failure.**

**Step 3: Add field + helpers**

In `internal/config/agenc_config.go`, add to the `RepoConfig` struct (next to `AlwaysSynced`):

```go
WriteableCopy string `yaml:"writeableCopy,omitempty"`
```

Add helper methods on `*AgencConfig`:

```go
// GetWriteableCopy returns the writeable-copy path for a repo, if configured.
func (c *AgencConfig) GetWriteableCopy(repoName string) (string, bool) {
    if rc, ok := c.RepoConfigs[repoName]; ok && rc.WriteableCopy != "" {
        return rc.WriteableCopy, true
    }
    return "", false
}

// GetAllWriteableCopies returns a map of repo name → writeable-copy path for
// every repo with one configured.
func (c *AgencConfig) GetAllWriteableCopies() map[string]string {
    out := map[string]string{}
    for name, rc := range c.RepoConfigs {
        if rc != nil && rc.WriteableCopy != "" {
            out[name] = rc.WriteableCopy
        }
    }
    return out
}

// NormalizeRepoConfigs enforces invariants: a repo with a WriteableCopy must
// have AlwaysSynced=true. Call this after deserializing config from disk.
func (c *AgencConfig) NormalizeRepoConfigs() {
    for _, rc := range c.RepoConfigs {
        if rc != nil && rc.WriteableCopy != "" {
            rc.AlwaysSynced = true
        }
    }
}
```

Find `ReadAgencConfig` (the function that loads config from disk) and call `cfg.NormalizeRepoConfigs()` after deserialization, before returning.

**Step 4: Run tests — expect PASS.**

`go test ./internal/config/ -run "TestWriteableCopyImpliesAlwaysSynced|TestGetWriteableCopy" -v` (sandbox disabled)

**Step 5: Commit**

```
git add internal/config/agenc_config.go internal/config/agenc_config_test.go
git commit -m "Add WriteableCopy field to RepoConfig with implied AlwaysSynced"
```

---

### Task 6: Path validation — pure function

**Files:**
- Create: `internal/config/writeable_copy_path.go`
- Create: `internal/config/writeable_copy_path_test.go`

**Step 1: Write failing tests**

```go
package config

import (
    "os"
    "path/filepath"
    "testing"
)

func TestValidateWriteableCopyPath(t *testing.T) {
    tmp := t.TempDir()

    cases := []struct {
        name      string
        input     string
        agencDir  string
        existing  map[string]string // repo → existing writeable copy path
        wantErr   bool
        errSubstr string
    }{
        {"empty", "", tmp, nil, true, "empty"},
        {"relative", "foo/bar", tmp, nil, true, "relative"},
        {"under agenc", filepath.Join(tmp, "subdir"), tmp, nil, true, "under"},
        {"parent missing", "/nonexistent/parent/path", tmp, nil, true, "parent"},
        {"nested in another writeable copy", filepath.Join(tmp, "outer", "inner"), filepath.Dir(tmp), map[string]string{"github.com/o/r": filepath.Join(tmp, "outer")}, true, "writeable copy"},
        {"good path", filepath.Join(tmp, "good"), filepath.Dir(tmp), nil, false, ""},
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            // Ensure parent exists for "good path" case
            if !tc.wantErr {
                _ = os.MkdirAll(filepath.Dir(tc.input), 0755)
            }
            _, err := ValidateWriteableCopyPath(tc.input, tc.agencDir, tc.existing)
            if tc.wantErr && err == nil {
                t.Fatalf("expected error containing %q, got nil", tc.errSubstr)
            }
            if !tc.wantErr && err != nil {
                t.Fatalf("expected no error, got %v", err)
            }
        })
    }
}
```

**Step 2: Run — expect compile failure.**

**Step 3: Implement**

Create `internal/config/writeable_copy_path.go`:

```go
package config

import (
    "os"
    "path/filepath"
    "strings"

    "github.com/mieubrisse/stacktrace"
)

// ValidateWriteableCopyPath validates a user-supplied writeable-copy path and
// returns the canonical absolute, symlink-resolved path on success.
//
// existingWriteableCopies is a map of repo name → writeable-copy path for
// repos other than the one being validated. Callers must filter out the
// repo currently being configured before passing this map.
func ValidateWriteableCopyPath(input, agencDirpath string, existingWriteableCopies map[string]string) (string, error) {
    if strings.TrimSpace(input) == "" {
        return "", stacktrace.NewError("path is empty")
    }

    // Expand ~ to the user's home directory
    if strings.HasPrefix(input, "~") {
        home, err := os.UserHomeDir()
        if err != nil {
            return "", stacktrace.Propagate(err, "failed to resolve home directory")
        }
        input = filepath.Join(home, strings.TrimPrefix(input, "~"))
    }

    abs, err := filepath.Abs(input)
    if err != nil {
        return "", stacktrace.Propagate(err, "failed to make path absolute")
    }
    if !filepath.IsAbs(abs) {
        return "", stacktrace.NewError("path is relative after expansion: %q", input)
    }
    abs = filepath.Clean(abs)

    if isSubpath(abs, agencDirpath) {
        return "", stacktrace.NewError("path %q is under agenc directory %q; pick a path outside ~/.agenc/", abs, agencDirpath)
    }

    for repo, otherPath := range existingWriteableCopies {
        if isSubpath(abs, otherPath) || isSubpath(otherPath, abs) {
            return "", stacktrace.NewError("path %q overlaps with the writeable copy for %q at %q", abs, repo, otherPath)
        }
    }

    parent := filepath.Dir(abs)
    if _, err := os.Stat(parent); err != nil {
        return "", stacktrace.NewError("parent directory %q does not exist; create it before configuring a writeable copy", parent)
    }

    // Resolve symlinks if path exists; reject if symlink resolves into agenc dir
    if info, err := os.Lstat(abs); err == nil && info.Mode()&os.ModeSymlink != 0 {
        resolved, err := filepath.EvalSymlinks(abs)
        if err != nil {
            return "", stacktrace.Propagate(err, "failed to resolve symlink at %q", abs)
        }
        if isSubpath(resolved, agencDirpath) {
            return "", stacktrace.NewError("path %q is a symlink resolving into agenc directory; pick a non-symlinked path", abs)
        }
        abs = resolved
    }

    return abs, nil
}

// isSubpath reports whether candidate is inside (or equal to) parent.
func isSubpath(candidate, parent string) bool {
    candidate = filepath.Clean(candidate)
    parent = filepath.Clean(parent)
    if candidate == parent {
        return true
    }
    rel, err := filepath.Rel(parent, candidate)
    if err != nil {
        return false
    }
    return !strings.HasPrefix(rel, "..") && rel != "."
}
```

**Step 4: Run tests — expect PASS.**

`go test ./internal/config/ -run TestValidateWriteableCopyPath -v` (sandbox disabled)

**Step 5: Commit**

```
git add internal/config/writeable_copy_path.go internal/config/writeable_copy_path_test.go
git commit -m "Add path validation for writeable copies"
```

---

### Task 7: ANSI escape stripper for notification display

**Files:**
- Create: `internal/server/notifications_strip.go`
- Create: `internal/server/notifications_strip_test.go`

**Step 1: Write failing tests**

```go
package server

import "testing"

func TestStripANSI(t *testing.T) {
    cases := []struct {
        name string
        in   string
        want string
    }{
        {"no ansi", "hello world", "hello world"},
        {"color code", "\x1b[31mred\x1b[0m", "red"},
        {"cursor move", "before\x1b[2J\x1b[Hafter", "beforeafter"},
        {"OSC sequence", "\x1b]0;title\x07rest", "rest"},
        {"unicode preserved", "héllo 🐚", "héllo 🐚"},
        {"markdown preserved", "# Header\n\n**bold**", "# Header\n\n**bold**"},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            if got := StripANSI(tc.in); got != tc.want {
                t.Errorf("got %q, want %q", got, tc.want)
            }
        })
    }
}
```

**Step 2: Run — expect compile failure.**

**Step 3: Implement**

```go
package server

import "regexp"

// ansiCSI matches ANSI CSI escape sequences (cursor movement, color codes, etc.)
var ansiCSI = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

// ansiOSC matches ANSI OSC escape sequences (window title, hyperlinks, etc.)
// terminated by BEL (\x07) or ST (\x1b\\).
var ansiOSC = regexp.MustCompile(`\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)

// StripANSI removes ANSI escape sequences from a string. Used to sanitize
// notification body content at display time so terminal escape sequences
// in the stored body cannot affect the user's terminal.
func StripANSI(s string) string {
    s = ansiCSI.ReplaceAllString(s, "")
    s = ansiOSC.ReplaceAllString(s, "")
    return s
}
```

**Step 4: Run tests — expect PASS.**

**Step 5: Commit**

```
git add internal/server/notifications_strip.go internal/server/notifications_strip_test.go
git commit -m "Add ANSI escape stripping for notification display"
```

---

### Task 8: HTTP handlers — notifications endpoints

**Files:**
- Create: `internal/server/notifications_handlers.go`
- Create: `internal/server/notifications_handlers_test.go`
- Modify: `internal/server/server.go` (route registration)

**Step 1: Write failing tests** for `POST /notifications`, `GET /notifications`, `POST /notifications/{id}/read`.

Pattern: spin up an `httptest.NewServer` wrapping a handler that uses an in-memory test DB. Assert response codes, JSON shape, body cap (256KB), title-newline rejection.

**Step 2: Run — expect compile failure.**

**Step 3: Implement handlers**

Create `internal/server/notifications_handlers.go`:

```go
package server

import (
    "encoding/json"
    "fmt"
    "net/http"
    "strings"

    "github.com/google/uuid"
    "github.com/odyssey/agenc/internal/database"
)

const notificationBodyMaxBytes = 256 * 1024

type createNotificationRequest struct {
    Kind         string `json:"kind"`
    SourceRepo   string `json:"source_repo,omitempty"`
    Title        string `json:"title"`
    BodyMarkdown string `json:"body_markdown"`
}

func (s *Server) handleCreateNotification(w http.ResponseWriter, r *http.Request) {
    var req createNotificationRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
        return
    }
    if req.Kind == "" {
        writeJSONError(w, http.StatusBadRequest, "kind is required")
        return
    }
    if req.Title == "" {
        writeJSONError(w, http.StatusBadRequest, "title is required")
        return
    }
    if strings.ContainsAny(req.Title, "\r\n") {
        writeJSONError(w, http.StatusBadRequest, "title must not contain newlines")
        return
    }
    body := capBody(req.BodyMarkdown)

    n := &database.Notification{
        ID:           uuid.NewString(),
        Kind:         req.Kind,
        SourceRepo:   req.SourceRepo,
        Title:        req.Title,
        BodyMarkdown: body,
    }
    if err := s.db.CreateNotification(n); err != nil {
        writeJSONError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusCreated, n)
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
    params := database.ListNotificationsParams{
        UnreadOnly: r.URL.Query().Get("unread") == "true",
        SourceRepo: r.URL.Query().Get("repo"),
        Kind:       r.URL.Query().Get("kind"),
    }
    list, err := s.db.ListNotifications(params)
    if err != nil {
        writeJSONError(w, http.StatusInternalServerError, err.Error())
        return
    }
    writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
    id := strings.TrimPrefix(r.URL.Path, "/notifications/")
    id = strings.TrimSuffix(id, "/read")
    if id == "" {
        writeJSONError(w, http.StatusBadRequest, "notification id required")
        return
    }
    if err := s.db.MarkNotificationRead(id); err != nil {
        writeJSONError(w, http.StatusInternalServerError, err.Error())
        return
    }
    w.WriteHeader(http.StatusNoContent)
}

func capBody(s string) string {
    if len(s) <= notificationBodyMaxBytes {
        return s
    }
    truncated := s[:notificationBodyMaxBytes]
    return truncated + fmt.Sprintf("\n\n---\n*[truncated: original was %d bytes]*", len(s))
}
```

Register routes in `internal/server/server.go` alongside existing handlers.

**Step 4: Run tests — expect PASS.**

**Step 5: Commit**

```
git add internal/server/notifications_handlers.go internal/server/notifications_handlers_test.go internal/server/server.go
git commit -m "Add HTTP handlers for notifications"
```

---

### Task 9: Server client — notification methods

**Files:**
- Modify: `internal/server/client.go`

**Step 1:** Add three methods on `Client`: `CreateNotification(req)`, `ListNotifications(params)`, `MarkNotificationRead(id)`. Match existing client method patterns.

**Step 2: Build with sandbox disabled, expect clean compile.**

**Step 3: Commit**

```
git add internal/server/client.go
git commit -m "Add Client methods for notifications"
```

---

### Task 10: CLI — `agenc notifications ls`

**Files:**
- Create: `cmd/notifications.go` (parent command, root-level)
- Create: `cmd/notifications_ls.go`

**Step 1:** Create `notifications` parent Cobra command with `Use: "notifications"`, registered to `rootCmd`.

**Step 2:** Create `agenc notifications ls` with flags `--all` (bool), `--repo` (string), `--kind` (string). Default behavior: filters unread only.

Output format (table via `tableprinter`):

```
ID        WHEN         KIND                       SOURCE                              TITLE
a3b2c1d4  4m ago       writeable_copy.conflict    github.com/mieubrisse/dotfiles      Rebase conflict on dotfiles
```

Empty (unread): `No unread notifications.\n\nShow all (incl read): agenc notifications ls --all\n`
Empty (all): `No notifications.\n`

Footer when N > 0:
```
N unread notifications.

View full content:    agenc notifications show <id>
Mark as read:         agenc notifications read <id>
Show all (incl read): agenc notifications ls --all
```

**Step 3:** Build with sandbox disabled, manual smoke test:
```
./_build/agenc-test notifications ls
```
Expected: empty-state message.

**Step 4:** Commit
```
git add cmd/notifications.go cmd/notifications_ls.go
git commit -m "Add 'agenc notifications ls' CLI command"
```

---

### Task 11: CLI — `agenc notifications show`

**Files:**
- Create: `cmd/notifications_show.go`

**Step 1:** Implement: resolve short ID via DB lookup helper (similar to `database.ResolveMissionID`), fetch notification, write `StripANSI(notification.BodyMarkdown)` to stdout. **No** added headers, decoration, or trailing newline policy beyond what's in the body.

**Step 2:** Add a DB resolution helper `database.ResolveNotificationID(input)` that accepts either full UUID or 8-char prefix (mirror `ResolveMissionID`). Test with both ambiguous and unique prefixes.

**Step 3:** Build with sandbox disabled, manual smoke test (after Task 8 server endpoints are wired).

**Step 4:** Commit
```
git add cmd/notifications_show.go internal/database/notifications.go
git commit -m "Add 'agenc notifications show' CLI command"
```

---

### Task 12: CLI — `agenc notifications read`

**Files:**
- Create: `cmd/notifications_read.go`

**Step 1:** Implement: resolve short ID, call `client.MarkNotificationRead(id)`. Print `Marked notification '<short-id>' as read.\n`. Idempotent: if already read, print `Notification '<short-id>' was already marked as read.\n` (use a GET first to detect).

**Step 2:** Build, manual smoke.

**Step 3:** Commit
```
git add cmd/notifications_read.go
git commit -m "Add 'agenc notifications read' CLI command"
```

---

### Task 13: CLI — `agenc notifications create`

**Files:**
- Create: `cmd/notifications_create.go`

**Step 1:** Implement with flags:

- `--kind` (required string)
- `--title` (required string)
- `--source-repo` (optional string)
- `--body` (string) — mutually exclusive with `--body-file`
- `--body-file` (string) — `-` for stdin

If `--body-file=-` and stdin is a TTY (`isatty(os.Stdin)`), error out: `no input on stdin — pipe content or use --body`.

POST to `/notifications`. Print `Created notification '<short-id>'.`.

**Step 2:** Build, manual smoke.

**Step 3:** Commit
```
git add cmd/notifications_create.go
git commit -m "Add 'agenc notifications create' CLI command"
```

---

### Task 14: CLI — `agenc repo writeable-copy` parent + `set` (config-only)

**Files:**
- Create: `cmd/repo_writeable_copy.go` (parent)
- Create: `cmd/repo_writeable_copy_set.go`

**Step 1:** Parent command `Use: "writeable-copy"` registered as a subcommand of `repoCmd`.

**Step 2:** `set <repo> <path>` command.

Logic:
1. Validate repo is in canonical format (reuse `IsCanonicalRepoName`).
2. Read config; if repo not in `cfg.RepoConfigs` AND not in repo library → error with "Add it first: agenc repo add <repo>".
3. Compute canonical path via `ValidateWriteableCopyPath`.
4. Reject if any *other* repo already has a writeable copy at this path.
5. **For now: do not clone or start watchers.** Just write config (`rc.WriteableCopy = canonicalPath`). Print `Configured writeable copy for '<repo>' at <path>. Cloning and sync will start once the server picks up the config change.` (Server config watcher will pick it up — covered in later tasks.)

This is a deliberate split: the `set` CLI is config-only; the server is responsible for the side effects (clone, watch, reconcile).

**Step 3:** Build, smoke test.

**Step 4:** Commit
```
git add cmd/repo_writeable_copy.go cmd/repo_writeable_copy_set.go
git commit -m "Add 'agenc repo writeable-copy set' CLI command"
```

---

### Task 15: CLI — `agenc repo writeable-copy unset` and `ls`

**Files:**
- Create: `cmd/repo_writeable_copy_unset.go`
- Create: `cmd/repo_writeable_copy_ls.go`

**Step 1:** `unset <repo>`: clear `rc.WriteableCopy = ""`, write config. Print `Removed writeable-copy configuration for '<repo>'. The on-disk clone at <path> was NOT deleted.`

**Step 2:** `ls`: read config, query server for pause status (new endpoint coming in Task 17). Display table with REPO, PATH, STATUS, LAST SYNC. For empty case: helpful message pointing at `set`.

**Step 3:** Build, smoke test.

**Step 4:** Commit
```
git add cmd/repo_writeable_copy_unset.go cmd/repo_writeable_copy_ls.go
git commit -m "Add 'agenc repo writeable-copy unset/ls' CLI commands"
```

---

### Task 16: Reject `--always-synced=false` when writeable copy is set

**Files:**
- Modify: `cmd/config_repo_config_set.go`

**Step 1:** In the `applyBoolFlag` handler for `repoConfigAlwaysSyncedFlagName`, before assigning, check whether `rc.WriteableCopy != ""` AND `synced == false`. If so, return a stacktrace error matching the design doc copy.

**Step 2:** Add a unit test in `cmd/config_repo_config_set_test.go` (or appropriate existing test file).

**Step 3:** Build, run tests with sandbox disabled.

**Step 4:** Commit
```
git add cmd/config_repo_config_set.go cmd/config_repo_config_set_test.go
git commit -m "Reject disabling always-synced when writeable copy is set"
```

---

### Task 17: Server endpoint — list writeable copies with status

**Files:**
- Create: `internal/server/writeable_copies_handler.go`
- Modify: `internal/server/client.go`

**Step 1:** New endpoint `GET /writeable-copies` returning a JSON array of:
```json
{ "repo_name": "...", "path": "...", "status": "ok|paused|missing", "last_sync_at": "...", "pause_reason": "..." }
```

Server reads config for the list of writeable copies, queries `db.ListPauses()` for status, stat'd path for missing detection.

**Step 2:** Add `Client.ListWriteableCopies()`.

**Step 3:** Wire into `agenc repo writeable-copy ls` (modify Task 15 if needed).

**Step 4:** Build, smoke.

**Step 5:** Commit
```
git add internal/server/writeable_copies_handler.go internal/server/client.go
git commit -m "Add server endpoint for writeable-copy listing"
```

---

### Task 18: Git interface abstraction (mockable)

**Files:**
- Create: `internal/server/git_command.go`

**Step 1:** Define an interface `GitCommander` with methods used by the tick state machine: `Status(repoDir) (clean bool, conflicted []string, err error)`, `HEAD(repoDir) (string, error)`, `DefaultBranch(repoDir) (string, error)`, `OriginURL(repoDir) (string, error)`, `Fetch(repoDir) error`, `Add(repoDir) error`, `Commit(repoDir, msg string) error`, `PullRebaseAutostash(repoDir) (conflicted []string, err error)`, `RebaseAbort(repoDir) error`, `Push(repoDir) (rejected bool, authFailure bool, err error)`, `MergeFFOnly(repoDir, ref string) error`, `Clone(url, dir string) error`, `IsRebaseInProgress(repoDir) bool`, `IsMergeInProgress(repoDir) bool`, `IndexLockExists(repoDir) bool`.

**Step 2:** Provide `RealGit{}` that implements via `os/exec`. Use existing patterns from `internal/mission/repo.go`.

**Step 3:** Build, no behavior change yet.

**Step 4:** Commit
```
git add internal/server/git_command.go
git commit -m "Add GitCommander interface for writeable-copy sync"
```

---

### Task 19: Tick logic — sanity checks (table-driven test + impl)

**Files:**
- Create: `internal/server/writeable_copies.go` (start)
- Create: `internal/server/writeable_copies_test.go` (start)

**Step 1: Failing test** — table-driven test `TestSanityCheck` covering each rejection case (index lock, in-progress rebase, in-progress merge, conflict markers, wrong branch, corrupt git, missing path, origin URL drift). Use a fake `GitCommander` injected into the function.

**Step 2:** Implement `runSanityCheck(ctx, gc GitCommander, repoDir, expectedURL string) (skipTick bool, pauseReason string, err error)`. Returns `(true, "", nil)` for transient skip-without-notify cases; `(false, "<reason>", err)` for pause-and-notify cases.

**Step 3:** Run tests, expect PASS.

**Step 4:** Commit
```
git add internal/server/writeable_copies.go internal/server/writeable_copies_test.go
git commit -m "Add writeable-copy tick sanity checks"
```

---

### Task 20: Tick logic — commit-if-dirty

**Files:**
- Modify: `internal/server/writeable_copies.go`
- Modify: `internal/server/writeable_copies_test.go`

**Step 1: Failing test** — `TestCommitIfDirty` with fake git: dirty tree → expect `Add` then `Commit` called with `auto-sync: <host> @ <ts>` message format.

**Step 2:** Implement `commitIfDirty(gc, repoDir, hostname) error` that runs `git status`, and if dirty runs `git add -A` + `git commit -m "auto-sync: <host> @ <iso-utc>"`.

**Step 3:** Test + commit.

```
git commit -m "Add writeable-copy auto-commit logic"
```

---

### Task 21: Tick logic — fetch and reconcile

**Files:**
- Modify: `internal/server/writeable_copies.go`
- Modify: `internal/server/writeable_copies_test.go`

**Step 1: Failing tests** — table-driven cases: equal/ahead/behind/diverged, where each case asserts the right git operations happen.

**Step 2:** Implement `reconcile(gc, repoDir, defaultBranch) (state ReconcileResult, err error)` returning one of: `Equal`, `Ahead` (push attempted), `Behind` (FF done), `Diverged` (rebase attempted).

**Step 3:** Tests + commit.

```
git commit -m "Add writeable-copy fetch-and-reconcile logic"
```

---

### Task 22: Tick logic — full tick orchestration with pause/notify

**Files:**
- Modify: `internal/server/writeable_copies.go`
- Modify: `internal/server/writeable_copies_test.go`

**Step 1: Failing tests** — at least these cases, with fake git + in-memory DB:

- happy path → no pause, no notification
- rebase conflict → pause + notification with body containing conflicted files
- non-FF push reject → pause + notification with kind `writeable_copy.non_ff_reject`
- auth failure → pause + notification with kind `writeable_copy.auth_failure`
- already paused, HEAD unchanged → tick exits without action
- already paused, HEAD moved + clean tree → pause cleared, full tick proceeds
- HEAD on non-default branch → pause + notification with kind `writeable_copy.wrong_branch`

**Step 2:** Implement `runTick(ctx, repo, path, gc, db) error` coordinating sanity check → pause-resume probe → commit-if-dirty → fetch+reconcile → on failure post pause+notification atomically.

Notification body templates: render via Go text templates. For the `writeable_copy.conflict` kind, match the body shown in the design doc.

**Step 3:** Tests + commit.

```
git commit -m "Add writeable-copy tick orchestration with pause/notify"
```

---

### Task 23: Reconcile worker channel + goroutine

**Files:**
- Modify: `internal/server/server.go` (add channel + goroutine)
- Modify: `internal/server/writeable_copies.go`

**Step 1:** Add `writeableCopyReconcileCh chan writeableCopyReconcileRequest` (buffered size 16) to `Server`.

**Step 2:** Add `func (s *Server) runWriteableCopyReconcileWorker(ctx context.Context)` that drains the channel and invokes `runTick` per request, with per-repo flock on `<path>/.git/agenc-writeable-copy.lock` (non-blocking; skip if held).

**Step 3:** Start goroutine in `Server.Run` alongside the existing background loops.

**Step 4:** Build, run existing tests with sandbox disabled to confirm no regressions.

**Step 5:** Commit
```
git commit -m "Add writeable-copy reconcile worker goroutine"
```

---

### Task 24: Boot reconcile sweep

**Files:**
- Modify: `internal/server/server.go` (`Run` method or startup helper)

**Step 1:** On server startup, after the worker goroutine is running, iterate `cfg.GetAllWriteableCopies()` and enqueue one reconcile request per writeable copy.

**Step 2:** Add a probe at startup: `exec.LookPath("git")`. If missing, log fatal error before starting writeable-copy worker (but allow server to continue running for other functionality).

**Step 3:** Build, smoke test (start server, observe log entries for boot reconciles).

**Step 4:** Commit
```
git commit -m "Add boot-time writeable-copy reconcile sweep"
```

---

### Task 25: fsnotify watcher — working tree (debounced)

**Files:**
- Create: `internal/server/writeable_copies_watcher.go`

**Step 1:** Function `watchWorkingTree(ctx, path, ch, debounce)` — fsnotify recursive watch of the working tree, excluding `.git/`, with 15-second debounce timer per the wrapper pattern. On debounce fire, send a reconcile request to the channel.

Use `addWatchesRecursive` from `internal/server/config_watcher.go` if appropriate, or copy the pattern.

**Step 2:** Wire watchers per writeable copy at server startup (after boot reconcile setup).

**Step 3:** Manual test (cannot fully E2E): edit a file in a configured writeable copy, observe reconcile log entry within ~20s.

**Step 4:** Commit
```
git commit -m "Add fsnotify working-tree watcher for writeable copies"
```

---

### Task 26: fsnotify watcher — origin refs (push-event trigger)

**Files:**
- Modify: `internal/server/writeable_copies_watcher.go`

**Step 1:** Mirror the wrapper pattern (`watchWorkspaceRemoteRefs`): watch `<path>/.git/refs/remotes/origin/<default-branch>` with debounce. On change, POST to `/repos/<name>/push-event` (or call the same internal handler directly, since we're already in-server).

This is the bridge that makes "writeable-copy successful push → library refresh" work for free.

**Step 2:** Wire alongside the working-tree watcher.

**Step 3:** Manual test: write a commit to the writeable copy, watch the library library refresh after the push.

**Step 4:** Commit
```
git commit -m "Add fsnotify origin-refs watcher for writeable-copy push detection"
```

---

### Task 27: Set-time clone + adoption

**Files:**
- Modify: `internal/server/writeable_copies.go`

**Step 1:** Function `ensureWriteableCopyExists(ctx, gc, agencDirpath, cfg, repoName, path) error`:
- If path doesn't exist → `git clone <library_remote_url> <path>`
- If path exists and is a git repo with matching origin → adopt
- If path exists but isn't a git repo → return error (server posts notification at startup; CLI shouldn't be allowed to reach this path)
- If path exists and is a git repo with **different** origin → return error

The CLI in Task 14 does config-only writes; this server-side function handles clone/adoption. Triggered both by the config watcher (when a new writeable copy appears) and by the boot sweep.

**Step 2:** Tests with fake git.

**Step 3:** Commit
```
git commit -m "Add writeable-copy clone and adoption logic"
```

---

### Task 28: Config watcher — react to writeable-copy changes

**Files:**
- Modify: `internal/server/config_watcher.go`

**Step 1:** Extend the existing `~/config.yml` debounced reload path. After config reload, diff `cfg.GetAllWriteableCopies()` against the previous snapshot:
- New entries → call `ensureWriteableCopyExists` → install watchers → enqueue reconcile request
- Removed entries → tear down watchers (do NOT delete the on-disk clone)
- Path changed → tear down old watchers, run `ensureWriteableCopyExists` for the new path, install new watchers

**Step 2:** Manual test: `agenc repo writeable-copy set` → observe server logs cloning and starting watchers.

**Step 3:** Commit
```
git commit -m "Wire config watcher to writeable-copy lifecycle"
```

---

### Task 29: Library worker fan-out

**Files:**
- Modify: `internal/server/repo_update_worker.go`

**Step 1:** In `repo_update_worker.go`, after the post-update hook block (after a successful `ForceUpdateRepo` where HEAD changed), check `cfg.GetWriteableCopy(repoName)`. If set, enqueue a `writeableCopyReconcileRequest` for that repo.

**Step 2:** Build with sandbox disabled.

**Step 3:** Commit
```
git commit -m "Fan out library updates to writeable-copy reconcile"
```

---

### Task 30: Adjutant prompt section

**Files:**
- Modify: `internal/claudeconfig/adjutant_claude.md`

**Step 1:** Append the "Notifications" section per the design doc (Adjutant integration). Include the `writeable_copy.conflict` resolution recipe.

**Step 2:** Build (regenerates `prime_content.md`), smoke test by spawning an Adjutant mission and asking "what notifications do I have?".

**Step 3:** Commit
```
git commit -m "Add Notifications section to Adjutant prompt"
```

---

### Task 31: Palette entry — "Show Notifications"

**Files:**
- Modify: relevant palette config in `cmd/tmux_palette.go` or `internal/config/agenc_config.go` (depending on where palette commands live)

**Step 1:** Add a new palette entry titled `Show Notifications` that runs:

```
agenc mission new --adjutant --prompt "$(cat <<'EOF'
The user opened the Show Notifications palette entry. Run `agenc notifications ls`,
read the unread notifications, summarize them concisely, and ask the user how
they'd like to proceed. For any writeable_copy.conflict notifications, follow the
guidance in the Notifications section of your prompt to help the user resolve them.
EOF
)"
```

**Step 2:** Manual test in a real tmux session — open palette, run entry, confirm Adjutant launches.

**Step 3:** Commit
```
git commit -m "Add 'Show Notifications' palette entry"
```

---

### Task 32: Palette footer indicator (unread count)

**Files:**
- Modify: `cmd/tmux_palette.go` (palette display logic)
- Modify: `internal/server/notifications_handlers.go` (add `GET /notifications/unread-count` endpoint for fast lookup)

**Step 1:** New server endpoint `GET /notifications/unread-count` returning `{"count": N}`. Implementation: `SELECT COUNT(*) FROM notifications WHERE read_at IS NULL`.

**Step 2:** Palette adds a footer line `⚠ N unread notifications — pick "Show Notifications" to review` only when N > 0. Color via fzf header attributes.

**Step 3:** Manual test.

**Step 4:** Commit
```
git commit -m "Add unread-notifications indicator to command palette footer"
```

---

### Task 33: E2E tests — notifications CRUD

**Files:**
- Modify: `scripts/e2e-test.sh`

**Step 1:** Add a `--- Notifications ---` section. Test:

```bash
run_test_output_contains "notifications ls empty" \
    "No unread notifications" \
    "${agenc_test}" notifications ls

# Create
run_test "notifications create succeeds" \
    0 \
    "${agenc_test}" notifications create --kind=test --title="Hello" --body="World"

run_test_output_contains "notifications ls shows new" \
    "Hello" \
    "${agenc_test}" notifications ls

# Read
short_id=$(...)  # extract short ID from previous output
run_test "notifications read succeeds" \
    0 \
    "${agenc_test}" notifications read "${short_id}"

run_test_output_contains "notifications ls is empty after read" \
    "No unread notifications" \
    "${agenc_test}" notifications ls

run_test_output_contains "notifications ls --all still shows it" \
    "Hello" \
    "${agenc_test}" notifications ls --all
```

**Step 2:** Run `make e2e` with sandbox disabled.

**Step 3:** Commit
```
git commit -m "Add E2E tests for notifications CRUD"
```

---

### Task 34: E2E tests — writeable-copy auto-commit and push

**Files:**
- Modify: `scripts/e2e-test.sh`

**Step 1:** Set up: create a bare git repo at `${TEST_DIR}/bare.git`. Configure as both library remote and writeable-copy remote. Add the repo via `agenc repo add --url file://${TEST_DIR}/bare.git`. Configure writeable copy at `${TEST_DIR}/wc`.

Test sequence:
- `set` succeeds and clones
- Edit a file in `${TEST_DIR}/wc`, sleep 25s, verify commit + push to `bare.git`
- `agenc repo writeable-copy ls` shows status `ok`

**Step 2:** Run, fix as needed.

**Step 3:** Commit
```
git commit -m "Add E2E tests for writeable-copy edit auto-commit"
```

---

### Task 35: E2E tests — conflict, pause, resume

**Files:**
- Modify: `scripts/e2e-test.sh`

**Step 1:** Set up two clones of `bare.git`. From clone A, push a conflicting change. In writeable copy, commit a conflicting local change. Trigger reconcile (touch a file). Wait. Verify:
- `agenc notifications ls` shows a `writeable_copy.conflict` entry
- `agenc repo writeable-copy ls` shows `paused`

Then resolve manually: `cd ${TEST_DIR}/wc && git pull --rebase --autostash` (resolve), `git rebase --continue && git push`. Wait one tick interval. Verify:
- `agenc repo writeable-copy ls` shows `ok` again
- `agenc notifications ls --all` still shows the (read or unread) notification

**Step 2:** Run.

**Step 3:** Commit
```
git commit -m "Add E2E tests for writeable-copy conflict/resume cycle"
```

---

### Task 36: Architecture doc updates

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1:** Add to the "Background loops" list:
- The writeable-copy reconcile worker, its three triggers, and the pause persistence behavior

Add to the IPC table:
- `<writeable_copy>/.git/refs/remotes/origin/<branch>` (Writer: git after push, Reader: server fsnotify watcher) → triggers library refresh

Add a "Writeable copies" subsection under Key Architectural Patterns describing the model: peer of the library, shares remote, fsnotify-driven sync, conflict pause persisted to DB.

Document the new `notifications` and `writeable_copy_pauses` tables.

**Step 2:** Read the doc end-to-end to confirm coherence.

**Step 3:** Commit
```
git commit -m "Update architecture doc for writeable copies and notifications"
```

---

### Task 37: Manual verification checklist

**Files:**
- None (this task is a checklist run by the user).

**Manual steps to verify before declaring done:**

1. Edit a file in a configured writeable copy via Finder/editor → confirm auto-commit fires within ~20s (`git -C <path> log -1 --format="%s"` shows `auto-sync:`).
2. Open command palette, confirm footer shows `⚠ N unread` when notifications exist; suppressed at 0.
3. Run "Show Notifications" palette entry → confirm Adjutant launches with the listing as initial prompt.
4. Stop server, push a conflicting change to remote, restart server with a paused writeable copy → confirm pause survives, no duplicate notification posted, reconcile re-runs and detects unresolved state.
5. From two machines, edit the same file in the writeable copy within ~30s → confirm second machine gets a conflict notification with correct conflicted-file list.
6. Try `agenc config repoConfig set <repo> --always-synced=false` for a repo with a writeable copy → confirm rejection with the documented self-serve message.

If any step fails, file a beads issue and pause the rollout.

---

## Final commit

Once Task 37 is complete:

```
git push
```

(Per CLAUDE.md auto-commit-and-push rule, the push at the end of every task already happened — this final push is just a no-op safety net.)

## Execution notes

- Most tasks build on the previous one. Do NOT skip ahead.
- After Task 13 (notifications CLI) the notifications subsystem is independently usable — agents can post notifications even before writeable copies work.
- After Task 22 the tick logic is unit-tested but not yet wired to triggers; after Task 28 the full lifecycle is live.
- E2E tests (33–35) gate the merge per `CLAUDE.md` E2E mandate.
- Architecture doc update (36) is required per `CLAUDE.md` doc-sync rule for any change to background loops or DB schema.
