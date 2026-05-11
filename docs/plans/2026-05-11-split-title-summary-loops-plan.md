Split Title/Summary Pipeline Implementation Plan
=================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Also invoke `/alembiq:software-engineer` and `/alembiq:go-coding` before touching any `.go` file (per repo CLAUDE.md).

**Goal:** Fix the bug where a failed Haiku call permanently leaves a session unsummarized, by splitting the title/summary pipeline into two independent DB-backed loops, each gating its offset advance on operation success.

**Architecture:** Replace the single title-consumer loop + async summarizer worker (channel + sync.Map dedup) with two independent 3-second loops — `runCustomTitleLoop` and `runAutoSummaryLoop` — each mirroring the search-indexer pattern (`internal/database/search.go:17-47`): atomic UPDATE of (output, offset) together, no offset advance on failure, retry naturally on next cycle. Adds one schema column, renames one, removes the channel/worker entirely.

**Tech Stack:** Go, SQLite (modernc.org/sqlite via `database/sql`), bufio-based JSONL scanning, AgenC server goroutines.

**Reference design doc:** `docs/plans/2026-05-11-split-title-summary-loops-design.md`. Read it before starting — it has the full rationale for every decision below.

---

Pre-flight
----------

Before starting, verify the design doc has been read end-to-end and the working tree is clean.

```
make check        # must pass; use dangerouslyDisableSandbox if sockets fail
git status        # must be clean
```

Each task in this plan ends with a commit. Each commit must pass `make check`. If a task gets large, split it — never commit broken state. Use `dangerouslyDisableSandbox: true` for `make build` / `make check` / `make e2e` per repo CLAUDE.md (the build cache and unix sockets need filesystem access outside the sandbox).

---

### Task 1: Schema migration — rename + add column

**Files:**
- Modify: `internal/database/migrations.go`
- Modify: `internal/database/sessions.go` (Session struct, all SELECT statements + Scan calls)
- Test: `internal/database/sessions_test.go` (update existing tests that reference `LastTitleUpdateOffset`)

This is the largest commit because the column rename forces simultaneous updates to all references. Subsequent tasks are smaller.

**Step 1: Read current migration entry points.**

Read `internal/database/migrations.go` end-to-end. Find `migrateSearchIndex` (around line 539). The new migration will mirror its idempotent-DDL style.

**Step 2: Add the migration logic to `migrateSearchIndex` (or add a new `migrateSplitTitleOffsets` function and call it from the same migration list).**

Append to `migrateSearchIndex` after the existing column adds:

```go
// Rename last_title_update_offset → last_custom_title_scan_offset
columns, err = getSessionColumnNames(conn)
if err != nil {
    return err
}
if columns["last_title_update_offset"] && !columns["last_custom_title_scan_offset"] {
    if _, err := conn.Exec("ALTER TABLE sessions RENAME COLUMN last_title_update_offset TO last_custom_title_scan_offset"); err != nil {
        return stacktrace.Propagate(err, "failed to rename last_title_update_offset to last_custom_title_scan_offset")
    }
}

// Add last_auto_summary_scan_offset
columns, err = getSessionColumnNames(conn)
if err != nil {
    return err
}
if !columns["last_auto_summary_scan_offset"] {
    if _, err := conn.Exec("ALTER TABLE sessions ADD COLUMN last_auto_summary_scan_offset INTEGER NOT NULL DEFAULT 0"); err != nil {
        return stacktrace.Propagate(err, "failed to add last_auto_summary_scan_offset column")
    }
}
```

Both blocks are idempotent — re-running is a no-op.

**Step 3: Update the `Session` struct in `internal/database/sessions.go`.**

Rename field `LastTitleUpdateOffset` → `LastCustomTitleScanOffset`. Add field `LastAutoSummaryScanOffset int64`.

**Step 4: Update every SELECT statement and Scan call.**

Find all SELECTs on `sessions` in `internal/database/sessions.go` and `internal/database/search.go`. There are 6 SELECT statements that need both:
- Column list: change `last_title_update_offset` → `last_custom_title_scan_offset`; add `last_auto_summary_scan_offset`
- Scan: change `&s.LastTitleUpdateOffset` → `&s.LastCustomTitleScanOffset`; add `&s.LastAutoSummaryScanOffset`

Grep to be exhaustive:
```bash
grep -n "last_title_update_offset\|LastTitleUpdateOffset" internal/database/
```

**Step 5: Update existing tests.**

Any test that references `LastTitleUpdateOffset` (e.g., `TestUpdateSessionScanResults`) needs the rename. Don't remove the old `UpdateSessionScanResults` function in this task — it's still in use by the live `runTitleConsumerCycle`. Just rename its internal references and keep its tests passing.

```bash
grep -rn "LastTitleUpdateOffset\|last_title_update_offset" internal/
```

**Step 6: Run tests.**

```bash
make check   # may need dangerouslyDisableSandbox
```

Expected: all pass.

**Step 7: Commit.**

```bash
git add internal/database/migrations.go internal/database/sessions.go internal/database/sessions_test.go internal/database/search.go
git commit -m "Rename last_title_update_offset and add last_auto_summary_scan_offset"
```

---

### Task 2: New DB query functions

**Files:**
- Modify: `internal/database/sessions.go`
- Test: `internal/database/sessions_test.go`

**Step 1: Write failing tests.**

Append to `internal/database/sessions_test.go`:

```go
func TestSessionsNeedingCustomTitleUpdate(t *testing.T) {
    db := openTestDB(t)
    mission, _ := db.CreateMission("github.com/owner/repo", nil)

    // Session with known_size > custom_title_offset → selected
    sess1, _ := db.CreateSession(mission.ID, "sess-1")
    _ = db.UpdateKnownFileSize(sess1.ID, 100)

    // Session with known_size == custom_title_offset → not selected
    sess2, _ := db.CreateSession(mission.ID, "sess-2")
    _ = db.UpdateKnownFileSize(sess2.ID, 50)
    _ = db.UpdateCustomTitleScanOffset(sess2.ID, 50)

    // Session with NULL known_size → not selected
    _, _ = db.CreateSession(mission.ID, "sess-3")

    rows, err := db.SessionsNeedingCustomTitleUpdate()
    if err != nil { t.Fatalf("query failed: %v", err) }
    if len(rows) != 1 || rows[0].ID != "sess-1" {
        t.Errorf("expected exactly sess-1, got %v", rows)
    }
}

func TestSessionsNeedingAutoSummary(t *testing.T) {
    db := openTestDB(t)
    mission, _ := db.CreateMission("github.com/owner/repo", nil)

    // Empty auto_summary, known_size > offset → selected
    sess1, _ := db.CreateSession(mission.ID, "sess-1")
    _ = db.UpdateKnownFileSize(sess1.ID, 100)

    // Non-empty auto_summary → NOT selected even though offset is 0
    sess2, _ := db.CreateSession(mission.ID, "sess-2")
    _ = db.UpdateKnownFileSize(sess2.ID, 100)
    _ = db.UpdateAutoSummaryAndOffset(sess2.ID, "an existing summary", 100)

    // Empty auto_summary but offset == known_size → not selected
    sess3, _ := db.CreateSession(mission.ID, "sess-3")
    _ = db.UpdateKnownFileSize(sess3.ID, 50)
    _ = db.UpdateAutoSummaryScanOffset(sess3.ID, 50)

    rows, err := db.SessionsNeedingAutoSummary()
    if err != nil { t.Fatalf("query failed: %v", err) }
    if len(rows) != 1 || rows[0].ID != "sess-1" {
        t.Errorf("expected exactly sess-1, got %v", rows)
    }
}

func TestUpdateCustomTitleAndOffset(t *testing.T) {
    db := openTestDB(t)
    mission, _ := db.CreateMission("github.com/owner/repo", nil)
    sess, _ := db.CreateSession(mission.ID, "sess-1")

    if err := db.UpdateCustomTitleAndOffset(sess.ID, "My Title", 1234); err != nil {
        t.Fatalf("update failed: %v", err)
    }
    got, _ := db.GetSession(sess.ID)
    if got.CustomTitle != "My Title" { t.Errorf("custom_title = %q, want %q", got.CustomTitle, "My Title") }
    if got.LastCustomTitleScanOffset != 1234 { t.Errorf("offset = %d, want 1234", got.LastCustomTitleScanOffset) }
}

func TestUpdateCustomTitleScanOffset(t *testing.T) {
    db := openTestDB(t)
    mission, _ := db.CreateMission("github.com/owner/repo", nil)
    sess, _ := db.CreateSession(mission.ID, "sess-1")

    if err := db.UpdateCustomTitleScanOffset(sess.ID, 999); err != nil { t.Fatal(err) }
    got, _ := db.GetSession(sess.ID)
    if got.CustomTitle != "" { t.Errorf("custom_title should be empty, got %q", got.CustomTitle) }
    if got.LastCustomTitleScanOffset != 999 { t.Errorf("offset = %d, want 999", got.LastCustomTitleScanOffset) }
}

func TestUpdateAutoSummaryAndOffset(t *testing.T) {
    db := openTestDB(t)
    mission, _ := db.CreateMission("github.com/owner/repo", nil)
    sess, _ := db.CreateSession(mission.ID, "sess-1")

    if err := db.UpdateAutoSummaryAndOffset(sess.ID, "the summary", 5000); err != nil { t.Fatal(err) }
    got, _ := db.GetSession(sess.ID)
    if got.AutoSummary != "the summary" { t.Errorf("auto_summary = %q", got.AutoSummary) }
    if got.LastAutoSummaryScanOffset != 5000 { t.Errorf("offset = %d", got.LastAutoSummaryScanOffset) }
}

func TestUpdateAutoSummaryScanOffset(t *testing.T) {
    db := openTestDB(t)
    mission, _ := db.CreateMission("github.com/owner/repo", nil)
    sess, _ := db.CreateSession(mission.ID, "sess-1")

    if err := db.UpdateAutoSummaryScanOffset(sess.ID, 777); err != nil { t.Fatal(err) }
    got, _ := db.GetSession(sess.ID)
    if got.AutoSummary != "" { t.Errorf("auto_summary should be empty, got %q", got.AutoSummary) }
    if got.LastAutoSummaryScanOffset != 777 { t.Errorf("offset = %d, want 777", got.LastAutoSummaryScanOffset) }
}
```

**Step 2: Run tests — verify failure.**

```bash
go test ./internal/database/ -run "TestSessionsNeeding|TestUpdateCustomTitle|TestUpdateAutoSummary" -v
```

Expected: compile error (the functions don't exist yet) or test failure.

**Step 3: Implement the 6 new functions in `internal/database/sessions.go`.**

Add after the existing `Update*` functions:

```go
// SessionsNeedingCustomTitleUpdate returns sessions where known_file_size >
// last_custom_title_scan_offset, meaning there are new bytes the custom-title
// loop hasn't scanned yet.
func (db *DB) SessionsNeedingCustomTitleUpdate() ([]*Session, error) {
    rows, err := db.conn.Query(
        "SELECT id, short_id, mission_id, custom_title, agenc_custom_title, auto_summary, last_custom_title_scan_offset, last_auto_summary_scan_offset, known_file_size, last_indexed_offset, created_at, updated_at FROM sessions WHERE known_file_size IS NOT NULL AND known_file_size > last_custom_title_scan_offset",
    )
    if err != nil {
        return nil, stacktrace.Propagate(err, "failed to query sessions needing custom title update")
    }
    defer rows.Close()
    return scanSessions(rows)
}

// SessionsNeedingAutoSummary returns sessions where auto_summary is empty AND
// there are new bytes since the last auto-summary scan.
func (db *DB) SessionsNeedingAutoSummary() ([]*Session, error) {
    rows, err := db.conn.Query(
        "SELECT id, short_id, mission_id, custom_title, agenc_custom_title, auto_summary, last_custom_title_scan_offset, last_auto_summary_scan_offset, known_file_size, last_indexed_offset, created_at, updated_at FROM sessions WHERE auto_summary = '' AND known_file_size IS NOT NULL AND known_file_size > last_auto_summary_scan_offset",
    )
    if err != nil {
        return nil, stacktrace.Propagate(err, "failed to query sessions needing auto-summary")
    }
    defer rows.Close()
    return scanSessions(rows)
}

// UpdateCustomTitleAndOffset atomically sets custom_title and advances
// last_custom_title_scan_offset.
func (db *DB) UpdateCustomTitleAndOffset(sessionID, customTitle string, newOffset int64) error {
    _, err := db.conn.Exec(
        `UPDATE sessions SET custom_title = ?, last_custom_title_scan_offset = ?, updated_at = ? WHERE id = ?`,
        customTitle, newOffset, time.Now().UTC().Format(time.RFC3339Nano), sessionID,
    )
    if err != nil {
        return stacktrace.Propagate(err, "failed to update custom_title and offset for session '%s'", sessionID)
    }
    return nil
}

// UpdateCustomTitleScanOffset advances only last_custom_title_scan_offset —
// used when a scan completed but found no new title metadata.
func (db *DB) UpdateCustomTitleScanOffset(sessionID string, newOffset int64) error {
    _, err := db.conn.Exec(
        `UPDATE sessions SET last_custom_title_scan_offset = ?, updated_at = ? WHERE id = ?`,
        newOffset, time.Now().UTC().Format(time.RFC3339Nano), sessionID,
    )
    if err != nil {
        return stacktrace.Propagate(err, "failed to update custom_title offset for session '%s'", sessionID)
    }
    return nil
}

// UpdateAutoSummaryAndOffset atomically sets auto_summary and advances
// last_auto_summary_scan_offset.
func (db *DB) UpdateAutoSummaryAndOffset(sessionID, summary string, newOffset int64) error {
    _, err := db.conn.Exec(
        `UPDATE sessions SET auto_summary = ?, last_auto_summary_scan_offset = ?, updated_at = ? WHERE id = ?`,
        summary, newOffset, time.Now().UTC().Format(time.RFC3339Nano), sessionID,
    )
    if err != nil {
        return stacktrace.Propagate(err, "failed to update auto_summary and offset for session '%s'", sessionID)
    }
    return nil
}

// UpdateAutoSummaryScanOffset advances only last_auto_summary_scan_offset —
// used when a scan completed but found no first user message yet.
func (db *DB) UpdateAutoSummaryScanOffset(sessionID string, newOffset int64) error {
    _, err := db.conn.Exec(
        `UPDATE sessions SET last_auto_summary_scan_offset = ?, updated_at = ? WHERE id = ?`,
        newOffset, time.Now().UTC().Format(time.RFC3339Nano), sessionID,
    )
    if err != nil {
        return stacktrace.Propagate(err, "failed to update auto_summary offset for session '%s'", sessionID)
    }
    return nil
}
```

Also update the existing `scanSessions` / `scanSession` helpers and all OTHER SELECTs to include `last_auto_summary_scan_offset` in the column list and Scan call. (This may already be done from Task 1; verify.)

**Step 4: Run tests — verify pass.**

```bash
go test ./internal/database/ -run "TestSessionsNeeding|TestUpdateCustomTitle|TestUpdateAutoSummary" -v
```

Expected: all 6 tests pass.

**Step 5: `make check` and commit.**

```bash
git add internal/database/sessions.go internal/database/sessions_test.go
git commit -m "Add SessionsNeedingCustomTitleUpdate/AutoSummary queries and atomic update fns"
```

---

### Task 3: New scan helpers

**Files:**
- Modify: `internal/server/session_scanner.go`
- Test: `internal/server/session_scanner_test.go`

**Step 1: Write failing tests.**

Read the existing `internal/server/session_scanner_test.go` patterns first. Then add:

```go
func TestScanJSONLForCustomTitle_FindsLastTitle(t *testing.T) {
    path := writeTempJSONL(t, `{"type":"file-history-snapshot"}
{"type":"custom-title","customTitle":"first"}
{"type":"user","message":{"role":"user","content":"hello"}}
{"type":"custom-title","customTitle":"second"}
`)
    got, err := scanJSONLForCustomTitle(path, 0)
    if err != nil { t.Fatal(err) }
    if got != "second" { t.Errorf("got %q, want %q", got, "second") }
}

func TestScanJSONLForCustomTitle_NoTitle(t *testing.T) {
    path := writeTempJSONL(t, `{"type":"user","message":{"role":"user","content":"hi"}}
`)
    got, err := scanJSONLForCustomTitle(path, 0)
    if err != nil { t.Fatal(err) }
    if got != "" { t.Errorf("got %q, want empty", got) }
}

func TestScanJSONLForCustomTitle_RespectsOffset(t *testing.T) {
    line1 := `{"type":"custom-title","customTitle":"before"}` + "\n"
    line2 := `{"type":"custom-title","customTitle":"after"}` + "\n"
    path := writeTempJSONL(t, line1+line2)
    got, err := scanJSONLForCustomTitle(path, int64(len(line1)))
    if err != nil { t.Fatal(err) }
    if got != "after" { t.Errorf("got %q, want %q", got, "after") }
}

func TestScanJSONLForFirstUserMessage_FindsFirst(t *testing.T) {
    path := writeTempJSONL(t, `{"type":"file-history-snapshot"}
{"type":"user","message":{"role":"user","content":"first prompt"}}
{"type":"user","message":{"role":"user","content":"second prompt"}}
`)
    got, err := scanJSONLForFirstUserMessage(path, 0)
    if err != nil { t.Fatal(err) }
    if got != "first prompt" { t.Errorf("got %q", got) }
}

func TestScanJSONLForFirstUserMessage_SkipsArrayContent(t *testing.T) {
    path := writeTempJSONL(t, `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"x"}]}}
{"type":"user","message":{"role":"user","content":"the real prompt"}}
`)
    got, err := scanJSONLForFirstUserMessage(path, 0)
    if err != nil { t.Fatal(err) }
    if got != "the real prompt" { t.Errorf("got %q", got) }
}

func TestScanJSONLForFirstUserMessage_None(t *testing.T) {
    path := writeTempJSONL(t, `{"type":"assistant","message":{"role":"assistant","content":[]}}
`)
    got, err := scanJSONLForFirstUserMessage(path, 0)
    if err != nil { t.Fatal(err) }
    if got != "" { t.Errorf("expected empty, got %q", got) }
}
```

Add a `writeTempJSONL(t, content)` helper if one doesn't already exist (check existing tests first — `setupTempJSONL` or similar may already be defined).

**Step 2: Run tests — verify they fail to compile.**

```bash
go test ./internal/server/ -run "TestScanJSONL" -v
```

Expected: compile error (functions don't exist).

**Step 3: Implement the two helpers in `internal/server/session_scanner.go`.**

Add (these can coexist with the existing `scanJSONLFromOffset` — we'll delete the old one in Task 7):

```go
// scanJSONLForCustomTitle reads a JSONL file from `offset` to EOF and returns
// the last custom-title metadata value found, or "" if none.
func scanJSONLForCustomTitle(jsonlFilepath string, offset int64) (string, error) {
    file, err := os.Open(jsonlFilepath)
    if err != nil {
        return "", err
    }
    defer file.Close()

    if offset > 0 {
        if _, err := file.Seek(offset, 0); err != nil {
            return "", err
        }
    }

    reader := bufio.NewReaderSize(file, 64*1024)
    var customTitle string
    for {
        line, err := reader.ReadString('\n')
        if len(line) > 0 {
            if t := tryExtractCustomTitle(line); t != "" {
                customTitle = t
            }
        }
        if err != nil {
            if err == io.EOF {
                break
            }
            return "", err
        }
    }
    return customTitle, nil
}

// scanJSONLForFirstUserMessage reads a JSONL file from `offset` to EOF and
// returns the first user-role line with string content. Early-returns on the
// first match — does NOT read the whole file. Skips array-content lines (tool
// results, multimodal) by virtue of tryExtractUserMessage's existing behavior.
func scanJSONLForFirstUserMessage(jsonlFilepath string, offset int64) (string, error) {
    file, err := os.Open(jsonlFilepath)
    if err != nil {
        return "", err
    }
    defer file.Close()

    if offset > 0 {
        if _, err := file.Seek(offset, 0); err != nil {
            return "", err
        }
    }

    reader := bufio.NewReaderSize(file, 64*1024)
    for {
        line, err := reader.ReadString('\n')
        if len(line) > 0 {
            if msg := tryExtractUserMessage(line); msg != "" {
                return msg, nil
            }
        }
        if err != nil {
            if err == io.EOF {
                return "", nil
            }
            return "", err
        }
    }
}
```

**Step 4: Run tests — verify they pass.**

```bash
go test ./internal/server/ -run "TestScanJSONL" -v
```

**Step 5: `make check` and commit.**

```bash
git add internal/server/session_scanner.go internal/server/session_scanner_test.go
git commit -m "Add scanJSONLForCustomTitle and scanJSONLForFirstUserMessage helpers"
```

---

### Task 4: Auto-summary loop

**Files:**
- Modify: `internal/server/session_summarizer.go` (still owns `generateSessionSummary`; rename file later if desired)
- Or create: `internal/server/auto_summary_loop.go`
- Test: `internal/server/auto_summary_loop_test.go` (new) or extend session_scanner_test.go

Pick whichever placement keeps the file <300 lines. The pattern below assumes a new file `auto_summary_loop.go`.

**Step 1: Write a failing integration-style test.**

Tests the loop body's branching, with a stub for `generateSessionSummary`. Strategy: extract the loop body into a function that takes a "generate summary" function as a parameter so it's mockable.

```go
// auto_summary_loop_test.go
func TestRunAutoSummaryCycle_HaikuFailureDoesNotAdvanceOffset(t *testing.T) {
    s := newTestServer(t)
    mission, _ := s.db.CreateMission("github.com/owner/repo", nil)
    sess, _ := s.db.CreateSession(mission.ID, "sess-1")
    // Set up JSONL file with a first user message
    jsonlPath := writeMissionJSONL(t, s.agencDirpath, mission.ID, sess.ID, `{"type":"user","message":{"role":"user","content":"hello"}}` + "\n")
    info, _ := os.Stat(jsonlPath)
    _ = s.db.UpdateKnownFileSize(sess.ID, info.Size())

    // Inject a failing summarizer
    s.runAutoSummaryCycleWith(func(ctx context.Context, _ string, _ string, _ int) (string, error) {
        return "", fmt.Errorf("simulated haiku failure")
    })

    got, _ := s.db.GetSession(sess.ID)
    if got.AutoSummary != "" { t.Errorf("auto_summary should be empty, got %q", got.AutoSummary) }
    if got.LastAutoSummaryScanOffset != 0 { t.Errorf("offset should stay 0, got %d", got.LastAutoSummaryScanOffset) }
}

func TestRunAutoSummaryCycle_SuccessAdvancesOffsetAndSetsSummary(t *testing.T) {
    // ... same setup, but injection returns ("a summary", nil) ...
    // assert AutoSummary == "a summary" and offset == known_size
}

func TestRunAutoSummaryCycle_NoUserMessageAdvancesOffsetOnly(t *testing.T) {
    // JSONL has no user-role string-content line
    // assert AutoSummary stays empty, offset advances to known_size
}
```

You may need `newTestServer` helper — see existing tests in the package for the pattern; if absent, build a minimal one with a tmpdir + a `*Server` initialized with a real `*DB`.

**Step 2: Run tests — verify failure.**

```bash
go test ./internal/server/ -run "TestRunAutoSummaryCycle" -v
```

**Step 3: Implement.**

```go
package server

import (
    "context"
    "time"

    "github.com/odyssey/agenc/internal/database"
)

const autoSummaryInterval = 3 * time.Second

// summarizeFunc is the signature of the auto-summary generator. Defaults to
// generateSessionSummary; overridable in tests.
type summarizeFunc func(ctx context.Context, agencDirpath, firstUserMessage string, maxWords int) (string, error)

func (s *Server) runAutoSummaryLoop(ctx context.Context) {
    select {
    case <-ctx.Done():
        return
    case <-time.After(autoSummaryInterval):
        s.runAutoSummaryCycle(ctx)
    }
    ticker := time.NewTicker(autoSummaryInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            s.runAutoSummaryCycle(ctx)
        }
    }
}

func (s *Server) runAutoSummaryCycle(ctx context.Context) {
    s.runAutoSummaryCycleWith(ctx, generateSessionSummary)
}

// runAutoSummaryCycleWith is the test-friendly inner loop. Production code uses
// generateSessionSummary; tests inject their own.
func (s *Server) runAutoSummaryCycleWith(ctx context.Context, summarize summarizeFunc) {
    sessions, err := s.db.SessionsNeedingAutoSummary()
    if err != nil {
        s.logger.Printf("Auto-summary: failed to query sessions: %v", err)
        return
    }

    maxWords := s.getConfig().GetSessionTitleMaxWords()

    for _, sess := range sessions {
        path := s.resolveSessionJSONLPath(sess)
        if path == "" {
            continue
        }
        knownSize := int64(0)
        if sess.KnownFileSize != nil {
            knownSize = *sess.KnownFileSize
        }

        msg, err := scanJSONLForFirstUserMessage(path, sess.LastAutoSummaryScanOffset)
        if err != nil {
            s.logger.Printf("Auto-summary: scan failed for session '%s': %v", database.ShortID(sess.ID), err)
            continue
        }
        if msg == "" {
            if err := s.db.UpdateAutoSummaryScanOffset(sess.ID, knownSize); err != nil {
                s.logger.Printf("Auto-summary: failed to advance offset for session '%s': %v", database.ShortID(sess.ID), err)
            }
            continue
        }

        summary, err := summarize(ctx, s.agencDirpath, msg, maxWords)
        if err != nil {
            s.logger.Printf("Auto-summary: failed to generate summary for session '%s': %v", database.ShortID(sess.ID), err)
            continue
        }

        if err := s.db.UpdateAutoSummaryAndOffset(sess.ID, summary, knownSize); err != nil {
            s.logger.Printf("Auto-summary: failed to save summary for session '%s': %v", database.ShortID(sess.ID), err)
            continue
        }

        s.reconcileTmuxWindowTitle(sess.MissionID)
        s.logger.Printf("Auto-summary: set auto_summary for session '%s': %q", database.ShortID(sess.ID), summary)
    }
}
```

**Step 4: Run tests — verify pass.**

**Step 5: `make check` and commit.**

```bash
git add internal/server/auto_summary_loop.go internal/server/auto_summary_loop_test.go
git commit -m "Add auto-summary loop with DB-backed retry on Haiku failure"
```

---

### Task 5: Custom-title loop

**Files:**
- Create: `internal/server/custom_title_loop.go`
- Test: `internal/server/custom_title_loop_test.go`

**Step 1: Write failing tests.**

```go
func TestRunCustomTitleCycle_FindsTitleAndAdvances(t *testing.T) {
    // Setup: session with a JSONL containing a custom-title line
    // Assert: CustomTitle set, LastCustomTitleScanOffset == known_size, tmux reconciled
}

func TestRunCustomTitleCycle_NoTitleAdvancesOffsetOnly(t *testing.T) {
    // JSONL has no custom-title metadata
    // Assert: CustomTitle stays empty, offset advances to known_size
}

func TestRunCustomTitleCycle_UnchangedTitleDoesNotWriteOrReconcile(t *testing.T) {
    // Existing CustomTitle == scan result
    // Assert: only offset is bumped, no second write to custom_title, no reconcile call
    // (use a counter or test-double for reconcileTmuxWindowTitle if practical)
}
```

**Step 2: Run tests — verify failure.**

**Step 3: Implement.**

```go
package server

import (
    "context"
    "time"

    "github.com/odyssey/agenc/internal/database"
)

const customTitleInterval = 3 * time.Second

func (s *Server) runCustomTitleLoop(ctx context.Context) {
    select {
    case <-ctx.Done():
        return
    case <-time.After(customTitleInterval):
        s.runCustomTitleCycle()
    }
    ticker := time.NewTicker(customTitleInterval)
    defer ticker.Stop()
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            s.runCustomTitleCycle()
        }
    }
}

func (s *Server) runCustomTitleCycle() {
    sessions, err := s.db.SessionsNeedingCustomTitleUpdate()
    if err != nil {
        s.logger.Printf("Custom-title: failed to query sessions: %v", err)
        return
    }

    for _, sess := range sessions {
        path := s.resolveSessionJSONLPath(sess)
        if path == "" {
            continue
        }
        knownSize := int64(0)
        if sess.KnownFileSize != nil {
            knownSize = *sess.KnownFileSize
        }

        title, err := scanJSONLForCustomTitle(path, sess.LastCustomTitleScanOffset)
        if err != nil {
            s.logger.Printf("Custom-title: scan failed for session '%s': %v", database.ShortID(sess.ID), err)
            continue
        }

        if title != "" && title != sess.CustomTitle {
            if err := s.db.UpdateCustomTitleAndOffset(sess.ID, title, knownSize); err != nil {
                s.logger.Printf("Custom-title: failed to save title for session '%s': %v", database.ShortID(sess.ID), err)
                continue
            }
            s.reconcileTmuxWindowTitle(sess.MissionID)
        } else {
            if err := s.db.UpdateCustomTitleScanOffset(sess.ID, knownSize); err != nil {
                s.logger.Printf("Custom-title: failed to advance offset for session '%s': %v", database.ShortID(sess.ID), err)
            }
        }
    }
}
```

**Step 4: Run tests — verify pass. `make check`. Commit.**

```bash
git add internal/server/custom_title_loop.go internal/server/custom_title_loop_test.go
git commit -m "Add custom-title loop following search-indexer atomic pattern"
```

---

### Task 6: Wire new loops into `Server.Start`; remove old goroutine launches

**Files:**
- Modify: `internal/server/server.go`

**Step 1: Find the existing goroutine launch block.**

```bash
grep -n "runLoop\|initSessionSummarizer" internal/server/server.go
```

You should see entries for `"title-consumer"`, `"session-summarizer"`, and the `s.initSessionSummarizer()` call.

**Step 2: Replace.**

Remove:
```go
s.initSessionSummarizer()
...
go s.runLoop("title-consumer", &wg, ctx, s.runTitleConsumerLoop)
go s.runLoop("session-summarizer", &wg, ctx, s.runSessionSummarizerWorker)
```

Add:
```go
go s.runLoop("custom-title", &wg, ctx, s.runCustomTitleLoop)
go s.runLoop("auto-summary", &wg, ctx, s.runAutoSummaryLoop)
```

Also remove the `sessionSummaryCh` and `summarizedSessions` fields from the `Server` struct.

**Step 3: `make check`.**

This will likely fail because the old `runTitleConsumerLoop`, `runSessionSummarizerWorker`, `initSessionSummarizer`, `requestSessionSummary`, etc. are still defined but now reference removed struct fields. Don't fix that here — Task 7 deletes them. Instead, temporarily comment out the calls in those dead functions if the compiler insists. Better: combine this task with Task 7 if the dead code blocks compilation.

**Decision point.** If `make check` fails purely on dead-function references to removed struct fields, just include the deletions from Task 7 in this commit and merge the two tasks. The plan splits them for clarity, but they may need to be one commit.

**Step 4: Commit.**

```bash
git add internal/server/server.go
git commit -m "Wire new custom-title and auto-summary loops; drop old goroutines"
```

---

### Task 7: Delete dead code

**Files:**
- Delete or shrink: `internal/server/session_summarizer.go` (keep `generateSessionSummary` and `buildSummarizerSystemPrompt`)
- Modify: `internal/server/session_scanner.go` (remove `runTitleConsumerLoop`, `runTitleConsumerCycle`, `scanJSONLFromOffset`, `jsonlScanResult`)
- Modify: `internal/database/sessions.go` (remove `UpdateSessionScanResults`, `SessionsNeedingTitleUpdate`, `UpdateSessionAutoSummary`)
- Modify: `internal/database/sessions_test.go` (remove `TestUpdateSessionScanResults` and any test for the removed funcs)
- Modify: `internal/server/session_scanner_test.go` (remove tests for `scanJSONLFromOffset`)

**Step 1: List what to delete.**

```
internal/server/session_summarizer.go:
  - const summarizerChannelSize
  - type summaryRequest
  - func (s *Server) requestSessionSummary
  - func (s *Server) runSessionSummarizerWorker
  - func (s *Server) handleSummaryRequest
  - func (s *Server) initSessionSummarizer

internal/server/session_scanner.go:
  - func (s *Server) runTitleConsumerLoop
  - func (s *Server) runTitleConsumerCycle
  - func scanJSONLFromOffset
  - type jsonlScanResult
  - const titleConsumerInterval

internal/database/sessions.go:
  - func (db *DB) UpdateSessionScanResults
  - func (db *DB) SessionsNeedingTitleUpdate
  - func (db *DB) UpdateSessionAutoSummary
```

**Step 2: Delete them.**

Be surgical — leave `generateSessionSummary`, `buildSummarizerSystemPrompt`, and their constants intact.

**Step 3: Delete the matching tests.**

```bash
grep -n "TestUpdateSessionScanResults\|TestSessionsNeedingTitleUpdate\|TestUpdateSessionAutoSummary\|TestScanJSONLFromOffset\|TestScanJSONLFromOffset_" internal/
```

**Step 4: `make check`.**

Expected: PASS. Deadcode analyzer should also be clean for these functions.

**Step 5: Commit.**

```bash
git add internal/server/session_summarizer.go internal/server/session_scanner.go internal/server/session_scanner_test.go internal/database/sessions.go internal/database/sessions_test.go
git commit -m "Remove obsolete title-consumer loop, summarizer worker, and channel infra"
```

---

### Task 8: E2E test

**Files:**
- Modify: `scripts/e2e-test.sh`

**Step 1: Find a section to add to.**

```bash
grep -n "auto_summary\|session" scripts/e2e-test.sh | head
```

Pick the closest existing section header, or add a new section `--- Auto-Summary Pipeline ---`.

**Step 2: Add a test that verifies the happy path.**

E2E is constrained by what can be scripted. A pragmatic check: spawn an idle mission, write a fake JSONL with a first user message into its project directory, update `known_file_size` (via direct DB write or by triggering the file watcher), wait ~10 seconds, query the DB for the session's `auto_summary` column — it should be non-empty.

If that's too invasive, a simpler smoke test:
```bash
run_test "auto-summary loop registers in server" \
    0 \
    "${agenc_test}" server status   # or some equivalent that proves the server is up
```

The real test is manual smoke in `_test-env` (see Task 10).

**Step 3: `make e2e`.**

```bash
make e2e
```

Expected: PASS.

**Step 4: Commit.**

```bash
git add scripts/e2e-test.sh
git commit -m "Add E2E check for auto-summary pipeline"
```

---

### Task 9: Architecture doc

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Find references.**

```bash
grep -n "session summarizer\|title consumer\|title-consumer\|session_summarizer\|session_scanner" docs/system-architecture.md
```

Lines around 137-170 and 425-427 from the earlier survey describe the components and file listing.

**Step 2: Rewrite.**

Replace "5. Session summarizer worker" + "9. Title consumer" with:

```
**5. Custom-title loop** (`internal/server/custom_title_loop.go`)
- Polls every 3s for sessions where the JSONL has grown since the last custom-title scan
- Scans new bytes for `custom-title` metadata entries
- Atomically writes the title and advances `last_custom_title_scan_offset` together

**6. Auto-summary loop** (`internal/server/auto_summary_loop.go`)
- Polls every 3s for sessions with empty `auto_summary` and new JSONL bytes since the last auto-summary scan
- Extracts the first string-content user message; calls Claude Haiku via the CLI to generate a 3-N word title
- Atomically writes `auto_summary` and advances `last_auto_summary_scan_offset` only on success — Haiku failures are naturally retried on the next cycle
```

(Renumber the remaining components.)

Update the `internal/server/` file listing (around line 425) to remove the channel/dedup description for `session_summarizer.go` and reflect that it now contains only the `generateSessionSummary` helper; add the two new files.

Update the session schema table (around line 739-761) to replace `last_title_update_offset` with the two new columns.

**Step 3: Read your changes back end-to-end — make sure component numbering and cross-references are consistent.**

**Step 4: Commit.**

```bash
git add docs/system-architecture.md
git commit -m "Update architecture doc for split title/summary loops"
```

---

### Task 10: Full verification

**Step 1: `make check` (sandbox disabled).**

```bash
# Use dangerouslyDisableSandbox: true
make check
```

Expected: PASS, no new deadcode warnings.

**Step 2: `make e2e` (sandbox disabled).**

```bash
make e2e
```

Expected: PASS.

**Step 3: Manual smoke test in `_test-env`.**

```bash
make build
make test-env
./_build/agenc-test mission new mieubrisse/agenc --prompt "say hi"
# Wait ~10s
./_build/agenc-test mission ls
# Verify the auto-summary appears in the mission listing
sqlite3 _test-env/database.sqlite "SELECT id, auto_summary, last_custom_title_scan_offset, last_auto_summary_scan_offset FROM sessions ORDER BY updated_at DESC LIMIT 1;"
make test-env-clean
```

Expected: `auto_summary` is non-empty within ~10 seconds of the prompt being sent.

**Step 4: Smoke-test the bug specifically.**

This requires temporarily simulating Haiku failure. Skip if too invasive — the unit test in Task 4 (`TestRunAutoSummaryCycle_HaikuFailureDoesNotAdvanceOffset`) already covers the retry semantics. The mission `c0e36548` itself (when this code ships to the user's real installation) provides production verification: its `auto_summary` should populate on the next loop cycle after the binary is restarted with the fix.

**Step 5: `git pull --rebase && git push`.**

```bash
git pull --rebase
git push
```

Per repo CLAUDE.md: always pull-rebase before push because other missions may have pushed concurrently.

---

Definition of done
------------------

- [ ] All `make check` and `make e2e` pass
- [ ] Manual smoke test in `_test-env` shows `auto_summary` populated within ~10s of first user prompt
- [ ] Unit tests cover: Haiku failure → no offset advance, retry; success → both fields advance together; no user message → only offset advances
- [ ] Old channel infrastructure (`sessionSummaryCh`, `summarizedSessions`, `summaryRequest`, `requestSessionSummary`, `runSessionSummarizerWorker`, `initSessionSummarizer`) removed
- [ ] Old loop functions (`runTitleConsumerLoop`, `runTitleConsumerCycle`, `scanJSONLFromOffset`) removed
- [ ] Old DB functions (`UpdateSessionScanResults`, `SessionsNeedingTitleUpdate`, `UpdateSessionAutoSummary`) removed
- [ ] `docs/system-architecture.md` reflects the new structure
- [ ] All commits pushed to remote

Provenance
----------

Designed and planned in AgenC mission `e62182e8-a318-4f6d-934e-40e3850add48`. Run `agenc mission print e62182e8` for the full discussion. Design doc: `docs/plans/2026-05-11-split-title-summary-loops-design.md`.
