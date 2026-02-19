# Tmux Window Title Tracking Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Track the tmux window title AgenC last set in the DB so dynamic title updates respect user renames and always fire correctly.

**Architecture:** Add a `tmux_window_title` column to `missions` via an idempotent migration. Read the current live tmux title before each rename and compare to the stored value — if they differ, a user has renamed the window and we bail. After every successful rename, write the new title back to the DB. Also fix the pre-existing bug where the AI summary rename branch skips the `UpdateMissionSessionName` call.

**Tech Stack:** Go, SQLite (`modernc.org/sqlite`), tmux CLI via `os/exec`

---

### Task 1: Add DB migration for `tmux_window_title`

**Files:**
- Modify: `internal/database/migrations.go`
- Modify: `internal/database/database.go`

**Step 1: Add the SQL constant to `migrations.go`**

In `migrations.go`, add one new constant alongside the others in the `const` block (after `addAISummaryColumnSQL`):

```go
addTmuxWindowTitleColumnSQL = `ALTER TABLE missions ADD COLUMN tmux_window_title TEXT NOT NULL DEFAULT '';`
```

**Step 2: Add the migration function to `migrations.go`**

Add this function at the bottom of `migrations.go` (after `migrateDropAgentTemplate`):

```go
// migrateAddTmuxWindowTitle idempotently adds the tmux_window_title column
// for tracking what title AgenC last set on the tmux window tab.
func migrateAddTmuxWindowTitle(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}

	if columns["tmux_window_title"] {
		return nil
	}

	_, err = conn.Exec(addTmuxWindowTitleColumnSQL)
	return err
}
```

**Step 3: Register the migration in `database.go`**

In the `Open()` function in `database.go`, add the new migration call after the `migrateAddAISummary` block (before `migrateAddQueryIndices`):

```go
if err := migrateAddTmuxWindowTitle(conn); err != nil {
    conn.Close()
    return nil, stacktrace.Propagate(err, "failed to add tmux_window_title column")
}
```

**Step 4: Run the tests to make sure migration doesn't break anything**

```
go test ./internal/database/... -v
```

Expected: all existing tests PASS (new column gets added to the temp DB transparently).

**Step 5: Commit**

```
git add internal/database/migrations.go internal/database/database.go
git commit -m "Add tmux_window_title column migration to missions table"
```

---

### Task 2: Add DB functions for `tmux_window_title`

**Files:**
- Modify: `internal/database/missions.go`

**Step 1: Write the failing tests in `internal/database/database_test.go`**

Add these two tests at the bottom of `database_test.go`:

```go
func TestSetAndGetMissionTmuxWindowTitle(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	// Initially empty
	title, err := db.GetMissionTmuxWindowTitle(mission.ID)
	if err != nil {
		t.Fatalf("GetMissionTmuxWindowTitle failed: %v", err)
	}
	if title != "" {
		t.Errorf("expected empty initial title, got %q", title)
	}

	// Set a title
	if err := db.SetMissionTmuxWindowTitle(mission.ID, "agenc"); err != nil {
		t.Fatalf("SetMissionTmuxWindowTitle failed: %v", err)
	}

	// Read it back
	got, err := db.GetMissionTmuxWindowTitle(mission.ID)
	if err != nil {
		t.Fatalf("GetMissionTmuxWindowTitle after set failed: %v", err)
	}
	if got != "agenc" {
		t.Errorf("expected %q, got %q", "agenc", got)
	}
}

func TestSetMissionTmuxWindowTitleOverwrite(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	if err := db.SetMissionTmuxWindowTitle(mission.ID, "first"); err != nil {
		t.Fatalf("first SetMissionTmuxWindowTitle failed: %v", err)
	}
	if err := db.SetMissionTmuxWindowTitle(mission.ID, "second"); err != nil {
		t.Fatalf("second SetMissionTmuxWindowTitle failed: %v", err)
	}

	got, err := db.GetMissionTmuxWindowTitle(mission.ID)
	if err != nil {
		t.Fatalf("GetMissionTmuxWindowTitle failed: %v", err)
	}
	if got != "second" {
		t.Errorf("expected %q after overwrite, got %q", "second", got)
	}
}
```

**Step 2: Run the tests to verify they fail**

```
go test ./internal/database/... -run TestSetAndGetMissionTmuxWindowTitle -v
go test ./internal/database/... -run TestSetMissionTmuxWindowTitleOverwrite -v
```

Expected: FAIL with "db.GetMissionTmuxWindowTitle undefined" (or similar compile error).

**Step 3: Add the DB functions to `missions.go`**

Add these two functions at the bottom of `missions.go` (after `GetMissionAISummary`):

```go
// GetMissionTmuxWindowTitle returns the tmux window title that AgenC last set
// for this mission. Returns "" if never set. Used to detect user renames
// before applying automatic title updates.
func (db *DB) GetMissionTmuxWindowTitle(id string) (string, error) {
	var title string
	err := db.conn.QueryRow("SELECT tmux_window_title FROM missions WHERE id = ?", id).Scan(&title)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to get tmux window title for mission '%s'", id)
	}
	return title, nil
}

// SetMissionTmuxWindowTitle records the exact string AgenC sent to
// tmux rename-window for this mission. Called after every successful rename
// so the wrapper can detect if the user has changed the title themselves.
func (db *DB) SetMissionTmuxWindowTitle(id string, title string) error {
	_, err := db.conn.Exec(
		"UPDATE missions SET tmux_window_title = ? WHERE id = ?",
		title, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to set tmux window title for mission '%s'", id)
	}
	return nil
}
```

**Step 4: Run tests to verify they pass**

```
go test ./internal/database/... -v
```

Expected: all tests PASS, including the two new ones.

**Step 5: Commit**

```
git add internal/database/missions.go internal/database/database_test.go
git commit -m "Add GetMissionTmuxWindowTitle and SetMissionTmuxWindowTitle DB functions"
```

---

### Task 3: Wire title tracking and override detection into `tmux.go`

**Files:**
- Modify: `internal/wrapper/tmux.go`

This task makes four targeted edits to `tmux.go`. Read the whole file before making any edit.

**Step 1: Add helper to read current tmux window name**

Add this private function near the other helpers at the bottom of `tmux.go` (after `extractRepoName`):

```go
// currentWindowName returns the current name of the tmux window containing
// paneID, by querying tmux directly. Returns "" if the query fails or we are
// not inside tmux.
func currentWindowName(paneID string) string {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{window_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
```

**Step 2: Add user-override guard to `updateWindowTitleFromSession`**

In `updateWindowTitleFromSession`, after the existing `AGENC_WINDOW_NAME` early-return block (after line ~195), add the user-override check:

```go
// If AgenC has previously set a title, check whether the user has since
// renamed the window. If the current window name no longer matches what
// AgenC set, respect the user's rename and do not overwrite it.
if storedTitle, err := w.db.GetMissionTmuxWindowTitle(w.missionID); err == nil && storedTitle != "" {
    if current := currentWindowName(paneID); current != storedTitle {
        return
    }
}
```

**Step 3: Fix the AI summary branch — add missing `UpdateMissionSessionName` call and track the title**

Replace the current AI summary block (lines ~211–216):

```go
// Before (missing DB write):
if aiSummary, err := w.db.GetMissionAISummary(w.missionID); err == nil && aiSummary != "" {
    title := truncateWindowTitle(aiSummary, maxWindowTitleLen)
    //nolint:errcheck // best-effort; failure is not critical
    exec.Command("tmux", "rename-window", "-t", paneID, title).Run()
    return
}
```

With:

```go
// After (includes session name update and title tracking):
if aiSummary, err := w.db.GetMissionAISummary(w.missionID); err == nil && aiSummary != "" {
    _ = w.db.UpdateMissionSessionName(w.missionID, aiSummary)
    title := truncateWindowTitle(aiSummary, maxWindowTitleLen)
    //nolint:errcheck // best-effort; failure is not critical
    exec.Command("tmux", "rename-window", "-t", paneID, title).Run()
    //nolint:errcheck // best-effort; failure is not critical
    _ = w.db.SetMissionTmuxWindowTitle(w.missionID, title)
    return
}
```

**Step 4: Add title tracking to the custom-title and session-name branches**

In the custom-title branch (around line 200–206), after the `rename-window` call, add:

```go
_ = w.db.SetMissionTmuxWindowTitle(w.missionID, title)
```

In the session-name branch (around line 225–228), after the `rename-window` call, add:

```go
_ = w.db.SetMissionTmuxWindowTitle(w.missionID, title)
```

**Step 5: Track the startup rename in `renameWindowForTmux`**

In `renameWindowForTmux`, after the final `exec.Command("tmux", "rename-window", ...).Run()` call (line ~64), add:

```go
_ = w.db.SetMissionTmuxWindowTitle(w.missionID, title)
```

Note: `renameWindowForTmux` is a method on `*Wrapper` so it has access to `w.db` and `w.missionID`.

**Step 6: Build to verify no compile errors**

```
make build
```

Expected: build succeeds. Use `dangerouslyDisableSandbox: true` because `make build` needs the Go build cache.

**Step 7: Commit**

```
git add internal/wrapper/tmux.go
git commit -m "Track tmux window title in DB, add user-override detection, fix AI summary branch"
```

---

### Task 4: Manual integration test

This feature depends on a running tmux session, so it cannot be unit tested. Verify manually:

**Test A — Dynamic title update (the main fix):**
1. Run `agenc mission new <repo>` inside tmux
2. Have a conversation with Claude (submit 1–2 messages)
3. When Claude goes idle (Stop event fires), observe the tmux window tab
4. Expected: tab title updates from the initial repo name to something derived from the session (custom title if `/rename` was used, AI summary after 10+ prompts, or a JSONL session name if available)

**Test B — User-override protection:**
1. Start a mission and let AgenC set the title (e.g., "agenc")
2. Manually rename the tmux window: `tmux rename-window "my-window"`
3. Submit another message to Claude and wait for the Stop event
4. Expected: tab title stays "my-window" — AgenC does NOT overwrite it

**Test C — Override resets on new mission:**
1. After Test B, exit the mission and start a new one in the same window
2. Expected: AgenC sets the title to the new mission's initial title (since `tmux_window_title` for the new mission's ID starts empty)

**Step 8: Commit if any fixes were needed during testing, then push**

```
git pull --rebase
git push
```

---

### Reference: Key File Locations

| File | Purpose |
|------|---------|
| `internal/database/migrations.go` | SQL constants + idempotent migration functions |
| `internal/database/database.go` | `Open()` — registers migrations in order |
| `internal/database/missions.go` | DB CRUD functions for mission rows |
| `internal/database/database_test.go` | DB unit tests |
| `internal/wrapper/tmux.go` | All tmux window title logic |
| `internal/wrapper/wrapper.go` | Calls `updateWindowTitleFromSession` on Stop event (line ~360) |
