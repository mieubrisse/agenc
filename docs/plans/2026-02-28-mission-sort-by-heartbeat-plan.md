Mission Sort by Heartbeat — Implementation Plan
=================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Sort missions by heartbeat (10s interval) instead of last_active, and drop the last_active column entirely.

**Architecture:** Remove the `last_active` column and all code that reads/writes it. Change heartbeat from 60s to 10s. Update the sort query to `COALESCE(last_heartbeat, created_at) DESC`. The `/missions/{id}/prompt` endpoint stays but only increments prompt_count.

**Tech Stack:** Go, SQLite, Cobra CLI

---

Task 1: Change heartbeat interval from 60s to 10s
--------------------------------------------------

**Files:**
- Modify: `internal/wrapper/wrapper.go:29-31`

**Step 1: Update the constant**

Change:
```go
// Heartbeat interval: 60s keeps server request volume low while maintaining
// adequate liveness detection.
heartbeatInterval = 60 * time.Second
```

To:
```go
// Heartbeat interval: 10s provides responsive activity tracking for mission
// sorting while keeping server request volume manageable.
heartbeatInterval = 10 * time.Second
```

**Step 2: Commit**

```bash
git add internal/wrapper/wrapper.go
git commit -m "Reduce heartbeat interval from 60s to 10s"
```

---

Task 2: Add migration to drop last_active column and update index
-----------------------------------------------------------------

**Files:**
- Modify: `internal/database/migrations.go`
- Modify: `internal/database/database.go`

**Step 1: Add migration function to `migrations.go`**

Add after `migrateCreateSessionsTable`:

```go
// migrateDropLastActive idempotently drops the last_active column and
// recreates the activity index to cover only last_heartbeat.
func migrateDropLastActive(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}

	if !columns["last_active"] {
		return nil
	}

	if _, err := conn.Exec("ALTER TABLE missions DROP COLUMN last_active"); err != nil {
		return stacktrace.Propagate(err, "failed to drop last_active column")
	}

	// Recreate the activity index without last_active
	if _, err := conn.Exec("DROP INDEX IF EXISTS idx_missions_activity"); err != nil {
		return stacktrace.Propagate(err, "failed to drop old activity index")
	}
	if _, err := conn.Exec("CREATE INDEX IF NOT EXISTS idx_missions_activity ON missions(last_heartbeat DESC)"); err != nil {
		return stacktrace.Propagate(err, "failed to create new activity index")
	}

	return nil
}
```

**Step 2: Wire migration into `database.go` Open()**

Add after the `migrateCreateSessionsTable` call (line ~100):

```go
if err := migrateDropLastActive(conn); err != nil {
	conn.Close()
	return nil, stacktrace.Propagate(err, "failed to drop last_active column")
}
```

**Step 3: Update `migrateAddQueryIndices` to not reference last_active**

In `migrations.go` line 266, change:
```go
"CREATE INDEX IF NOT EXISTS idx_missions_activity ON missions(last_active DESC, last_heartbeat DESC)",
```
To:
```go
"CREATE INDEX IF NOT EXISTS idx_missions_activity ON missions(last_heartbeat DESC)",
```

**Step 4: Remove the `migrateAddLastActive` function and its constant**

Delete:
- The `addLastActiveColumnSQL` constant (line 24)
- The `migrateAddLastActive` function (lines 215-229)
- The call to `migrateAddLastActive` in `database.go` Open() (lines 77-80)

**Step 5: Commit**

```bash
git add internal/database/migrations.go internal/database/database.go
git commit -m "Add migration to drop last_active column and update activity index"
```

---

Task 3: Remove LastActive from Mission struct and scanners
----------------------------------------------------------

**Files:**
- Modify: `internal/database/missions.go`
- Modify: `internal/database/scanners.go`
- Modify: `internal/database/queries.go`

**Step 1: Remove `LastActive` from Mission struct**

In `missions.go`, remove line 21:
```go
LastActive             *time.Time
```

**Step 2: Remove `UpdateLastActive` function**

In `missions.go`, delete the entire function (lines 241-253):
```go
// UpdateLastActive sets the last_active timestamp to the current time for
// the given mission. Called by the wrapper when the user submits a prompt.
func (db *DB) UpdateLastActive(id string) error {
	...
}
```

**Step 3: Remove `last_active` from all SELECT statements**

In `missions.go`, update ALL four SQL queries that select `last_active`. Each one has this column list:
```
id, short_id, prompt, status, git_repo, last_heartbeat, last_active, session_name, ...
```

Remove `, last_active` from each. There are 4 locations:
- `GetMission` (line 104)
- `GetMostRecentMissionForCron` (line 123)
- `GetMissionByTmuxPane` (line 141)
- `ListMissionsNeedingSummary` (line 376)

**Step 4: Update `queries.go`**

Remove `last_active` from the SELECT in `buildListMissionsQuery` (line 10).

Change the ORDER BY (line 26) from:
```go
query += " ORDER BY COALESCE(last_active, last_heartbeat, created_at) DESC"
```
To:
```go
query += " ORDER BY COALESCE(last_heartbeat, created_at) DESC"
```

**Step 5: Update scanners**

In `scanners.go`, update both `scanMissions` and `scanMission`:

Remove `lastActive` from the var declaration (lines 15 and 57):
```go
var lastHeartbeat, lastActive, sessionNameUpdatedAt, cronID, cronName, configCommit, tmuxPane sql.NullString
```
becomes:
```go
var lastHeartbeat, sessionNameUpdatedAt, cronID, cronName, configCommit, tmuxPane sql.NullString
```

Remove `&lastActive` from the Scan call (lines 17 and 59).

Remove the `lastActive.Valid` block (lines 24-27 and 66-69):
```go
if lastActive.Valid {
    t, _ := time.Parse(time.RFC3339, lastActive.String)
    m.LastActive = &t
}
```

**Step 6: Update `ListMissions` doc comment**

In `missions.go` lines 83-84, change:
```go
// ListMissions returns missions ordered by the most recent activity timestamp
// (newest of last_active, last_heartbeat, or created_at) descending.
```
To:
```go
// ListMissions returns missions ordered by the most recent activity timestamp
// (newest of last_heartbeat or created_at) descending.
```

**Step 7: Commit**

```bash
git add internal/database/missions.go internal/database/scanners.go internal/database/queries.go
git commit -m "Remove last_active from Mission struct, queries, and scanners"
```

---

Task 4: Remove LastActive from server layer
--------------------------------------------

**Files:**
- Modify: `internal/server/missions.go`
- Modify: `internal/server/client.go`
- Modify: `internal/server/server.go`

**Step 1: Remove `LastActive` from `MissionResponse` struct**

In `missions.go`, remove line 27:
```go
LastActive             *time.Time `json:"last_active"`
```

**Step 2: Remove `LastActive` from `ToMission()` and `toMissionResponse()`**

In `ToMission()` (line 51), remove:
```go
LastActive:             mr.LastActive,
```

In `toMissionResponse()` (line 75), remove:
```go
LastActive:             m.LastActive,
```

**Step 3: Update `handleRecordPrompt` to only increment prompt count**

Change the function (lines 746-766) to remove the `UpdateLastActive` call. The endpoint stays because `IncrementPromptCount` is still needed:

```go
// handleRecordPrompt handles POST /missions/{id}/prompt.
// Increments the prompt count for the mission.
func (s *Server) handleRecordPrompt(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if err := s.db.IncrementPromptCount(resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to increment prompt_count: %s", err.Error())
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
```

**Step 4: Update `RecordPrompt` client doc comment**

In `client.go` line 225, change:
```go
// RecordPrompt updates last_active and increments prompt_count for a mission.
```
To:
```go
// RecordPrompt increments prompt_count for a mission.
```

**Step 5: Commit**

```bash
git add internal/server/missions.go internal/server/client.go
git commit -m "Remove last_active from server response and prompt handler"
```

---

Task 5: Remove LastActive from CLI display layer
-------------------------------------------------

**Files:**
- Modify: `cmd/mission_ls.go`
- Modify: `cmd/mission_helpers.go`
- Modify: `cmd/summary.go`

**Step 1: Simplify `formatLastActive` function**

In `mission_ls.go`, change the function (lines 177-189) to drop the `lastActive` parameter:

```go
// formatLastActive returns a human-readable timestamp of the mission's last
// activity. Uses the newest of: LastHeartbeat (wrapper liveness) or CreatedAt.
func formatLastActive(lastHeartbeat *time.Time, createdAt time.Time) string {
	if lastHeartbeat != nil {
		return lastHeartbeat.Local().Format("2006-01-02 15:04")
	}
	return createdAt.Local().Format("2006-01-02 15:04")
}
```

**Step 2: Update all 4 call sites in `mission_ls.go`**

Lines 110, 121, 133, 142 — change each from:
```go
formatLastActive(m.LastActive, m.LastHeartbeat, m.CreatedAt),
```
To:
```go
formatLastActive(m.LastHeartbeat, m.CreatedAt),
```

**Step 3: Update call site in `mission_helpers.go`**

Line 88 — change from:
```go
LastActive: formatLastActive(m.LastActive, m.LastHeartbeat, m.CreatedAt),
```
To:
```go
LastActive: formatLastActive(m.LastHeartbeat, m.CreatedAt),
```

(The `LastActive` field name on `missionPickerEntry` is fine — it's just a display string label, not tied to the DB column.)

**Step 4: Update `summary.go` to use `LastHeartbeat` instead of `LastActive`**

Lines 138-142 — change from:
```go
if m.LastActive != nil && m.LastActive.After(dayStart) && m.LastActive.Before(dayEnd) {
    if stats.LastActivity == nil || m.LastActive.After(*stats.LastActivity) {
        stats.LastActivity = m.LastActive
    }
}
```
To:
```go
if m.LastHeartbeat != nil && m.LastHeartbeat.After(dayStart) && m.LastHeartbeat.Before(dayEnd) {
    if stats.LastActivity == nil || m.LastHeartbeat.After(*stats.LastActivity) {
        stats.LastActivity = m.LastHeartbeat
    }
}
```

**Step 5: Commit**

```bash
git add cmd/mission_ls.go cmd/mission_helpers.go cmd/summary.go
git commit -m "Update CLI display to use last_heartbeat instead of last_active"
```

---

Task 6: Update tests
---------------------

**Files:**
- Modify: `internal/database/database_test.go`

**Step 1: Rewrite `TestListMissions_SortsByNewestActivity`**

Replace the test (lines 186-238) with:

```go
func TestListMissions_SortsByNewestActivity(t *testing.T) {
	db := openTestDB(t)

	// Create three missions with different activity patterns
	m1, err := db.CreateMission("github.com/owner/repo1", nil)
	if err != nil {
		t.Fatalf("failed to create mission 1: %v", err)
	}

	m2, err := db.CreateMission("github.com/owner/repo2", nil)
	if err != nil {
		t.Fatalf("failed to create mission 2: %v", err)
	}

	m3, err := db.CreateMission("github.com/owner/repo3", nil)
	if err != nil {
		t.Fatalf("failed to create mission 3: %v", err)
	}

	// m1: has last_heartbeat (most recent)
	if err := db.UpdateHeartbeat(m1.ID); err != nil {
		t.Fatalf("failed to update heartbeat for m1: %v", err)
	}

	// m2: has older heartbeat
	oldHeartbeat := "2026-01-01T12:00:00Z"
	if _, err := db.conn.Exec("UPDATE missions SET last_heartbeat = ? WHERE id = ?", oldHeartbeat, m2.ID); err != nil {
		t.Fatalf("failed to set old heartbeat for m2: %v", err)
	}

	// m3: has neither (only created_at, oldest)
	// Backdate created_at to ensure it's oldest
	oldCreated := "2025-01-01T00:00:00Z"
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", oldCreated, m3.ID); err != nil {
		t.Fatalf("failed to backdate m3: %v", err)
	}

	// List missions
	missions, err := db.ListMissions(ListMissionsParams{IncludeArchived: false})
	if err != nil {
		t.Fatalf("failed to list missions: %v", err)
	}

	if len(missions) != 3 {
		t.Fatalf("expected 3 missions, got %d", len(missions))
	}

	// Verify sort order: m1 (recent heartbeat), m2 (old heartbeat), m3 (only created_at)
	if missions[0].ID != m1.ID {
		t.Errorf("expected first mission to be m1 (recent heartbeat), got %s", missions[0].ID)
	}
	if missions[1].ID != m2.ID {
		t.Errorf("expected second mission to be m2 (old heartbeat), got %s", missions[1].ID)
	}
	if missions[2].ID != m3.ID {
		t.Errorf("expected third mission to be m3 (only created_at), got %s", missions[2].ID)
	}
}
```

**Step 2: Update `TestListMissions_BrandNewMissionAppearsFirst`**

Lines 261 comment — change:
```go
// Create a brand new mission (no heartbeat, no last_active)
```
To:
```go
// Create a brand new mission (no heartbeat)
```

**Step 3: Run tests**

```bash
make check
```

Expected: All tests pass.

**Step 4: Commit**

```bash
git add internal/database/database_test.go
git commit -m "Update sort tests for heartbeat-only sorting"
```

---

Task 7: Build and verify
-------------------------

**Step 1: Run full build**

```bash
make build
```

Expected: Compiles cleanly with no errors.

**Step 2: Run the binary**

```bash
./agenc mission ls
```

Expected: Missions listed and sorted by `last_heartbeat` then `created_at`.

**Step 3: Commit any remaining changes and push**

```bash
git push
```
