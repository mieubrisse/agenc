Mission Picker Sorting Implementation Plan
===========================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Sort the mission attach picker so missions needing attention appear first, followed by most-recently-interacted, then fallback to heartbeat/creation time.

**Architecture:** Add a `last_user_prompt_at` DB column updated on UserPromptSubmit and via heartbeat. Remove `idle_prompt` from the wrapper's attention group. Sort missions client-side using three tiers: needs_attention, last_user_prompt_at, then heartbeat/created_at.

**Tech Stack:** Go, SQLite, tmux

---

Task 1: Add `last_user_prompt_at` DB column and migration
----------------------------------------------------------

**Files:**
- Modify: `internal/database/migrations.go`
- Modify: `internal/database/database.go`
- Test: `internal/database/database_test.go`

**Step 1: Write the failing test**

Add to `internal/database/database_test.go`:

```go
func TestMigrateAddLastUserPromptAt(t *testing.T) {
	db := openTestDB(t)

	// Verify column exists by writing and reading a value
	m, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	if err := db.UpdateLastUserPromptAt(m.ID); err != nil {
		t.Fatalf("failed to update last_user_prompt_at: %v", err)
	}

	got, err := db.GetMission(m.ID)
	if err != nil {
		t.Fatalf("failed to get mission: %v", err)
	}
	if got.LastUserPromptAt == nil {
		t.Fatal("expected last_user_prompt_at to be set, got nil")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/database/ -run TestMigrateAddLastUserPromptAt -v`
Expected: FAIL — `UpdateLastUserPromptAt` and `LastUserPromptAt` don't exist yet.

**Step 3: Write the migration and struct changes**

In `internal/database/migrations.go`, add the constant:

```go
addLastUserPromptAtColumnSQL = `ALTER TABLE missions ADD COLUMN last_user_prompt_at TEXT;`
```

Add the migration function:

```go
func migrateAddLastUserPromptAt(conn *sql.DB) error {
	columns, err := getColumnNames(conn)
	if err != nil {
		return err
	}
	if columns["last_user_prompt_at"] {
		return nil
	}
	_, err = conn.Exec(addLastUserPromptAtColumnSQL)
	return err
}
```

In `internal/database/database.go`, add the migration call at the end of `Open()` (before `return`):

```go
if err := migrateAddLastUserPromptAt(conn); err != nil {
	conn.Close()
	return nil, stacktrace.Propagate(err, "failed to add last_user_prompt_at column")
}
```

In `internal/database/missions.go`, add the field to the `Mission` struct (after `LastHeartbeat`):

```go
LastUserPromptAt *time.Time
```

Add the DB function:

```go
func (db *DB) UpdateLastUserPromptAt(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"UPDATE missions SET last_user_prompt_at = ? WHERE id = ?",
		now, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update last_user_prompt_at for mission '%s'", id)
	}
	return nil
}
```

Add a second DB function for heartbeat-driven updates (only writes when timestamp is non-empty):

```go
func (db *DB) SetLastUserPromptAt(id string, timestamp string) error {
	if timestamp == "" {
		return nil
	}
	_, err := db.conn.Exec(
		"UPDATE missions SET last_user_prompt_at = ? WHERE id = ?",
		timestamp, id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to set last_user_prompt_at for mission '%s'", id)
	}
	return nil
}
```

Update ALL SELECT queries that read from missions to include `last_user_prompt_at`. The column list appears in these locations:
- `internal/database/queries.go:10` — `buildListMissionsQuery`
- `internal/database/missions.go:109` — `GetMission`
- `internal/database/missions.go:128` — `GetMostRecentMissionForCron`
- `internal/database/missions.go:146` — `GetMissionByTmuxPane`

Add `last_user_prompt_at` after `last_heartbeat` in each SELECT list.

Update both scanner functions in `internal/database/scanners.go`:
- `scanMissions` (line 15): add `lastUserPromptAt` to the `sql.NullString` var list, add it to `rows.Scan(...)`, and parse it like `lastHeartbeat`.
- `scanMission` (line 66): same changes for the single-row scanner.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/database/ -run TestMigrateAddLastUserPromptAt -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/database/migrations.go internal/database/database.go internal/database/missions.go internal/database/scanners.go internal/database/queries.go internal/database/database_test.go
git commit -m "Add last_user_prompt_at column to missions table"
```

---

Task 2: Update RecordPrompt to set `last_user_prompt_at`
---------------------------------------------------------

**Files:**
- Modify: `internal/server/missions.go:959-975` — `handleRecordPrompt`
- Test: `internal/database/database_test.go`

**Step 1: Write the failing test**

Add to `internal/database/database_test.go`:

```go
func TestIncrementPromptCount_SetsLastUserPromptAt(t *testing.T) {
	db := openTestDB(t)

	m, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	// Before: last_user_prompt_at should be nil
	got, _ := db.GetMission(m.ID)
	if got.LastUserPromptAt != nil {
		t.Fatal("expected last_user_prompt_at to be nil initially")
	}

	// Update last_user_prompt_at
	if err := db.UpdateLastUserPromptAt(m.ID); err != nil {
		t.Fatalf("failed to update last_user_prompt_at: %v", err)
	}

	got, _ = db.GetMission(m.ID)
	if got.LastUserPromptAt == nil {
		t.Fatal("expected last_user_prompt_at to be set after update")
	}
}
```

**Step 2: Run test to verify it passes** (this should already pass from Task 1)

Run: `go test ./internal/database/ -run TestIncrementPromptCount_SetsLastUserPromptAt -v`

**Step 3: Update handleRecordPrompt to also call UpdateLastUserPromptAt**

In `internal/server/missions.go`, modify `handleRecordPrompt` to also update `last_user_prompt_at`:

```go
func (s *Server) handleRecordPrompt(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if err := s.db.IncrementPromptCount(resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to increment prompt_count: %s", err.Error())
	}

	if err := s.db.UpdateLastUserPromptAt(resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to update last_user_prompt_at: %s", err.Error())
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/server/ -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/server/missions.go internal/database/database_test.go
git commit -m "Update RecordPrompt to set last_user_prompt_at"
```

---

Task 3: Add `last_user_prompt_at` to heartbeat payload
-------------------------------------------------------

**Files:**
- Modify: `internal/wrapper/wrapper.go` — add `lastUserPromptAt` field, include in heartbeat
- Modify: `internal/server/missions.go:923-957` — `HeartbeatRequest`, `handleHeartbeat`
- Modify: `internal/server/client.go:277-285` — `Heartbeat` method
- Modify: `internal/server/missions.go:22-47` — `MissionResponse`
- Modify: `internal/server/missions.go:72-91` — `toMissionResponse`
- Modify: `internal/server/missions.go:50-70` — `ToMission`
- Test: `internal/database/database_test.go`

**Step 1: Write the failing test for heartbeat DB update**

Add to `internal/database/database_test.go`:

```go
func TestSetLastUserPromptAt_SkipsEmpty(t *testing.T) {
	db := openTestDB(t)

	m, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	// Set an initial value
	if err := db.UpdateLastUserPromptAt(m.ID); err != nil {
		t.Fatalf("failed to set initial last_user_prompt_at: %v", err)
	}

	got, _ := db.GetMission(m.ID)
	original := got.LastUserPromptAt

	// SetLastUserPromptAt with empty string should not clobber
	if err := db.SetLastUserPromptAt(m.ID, ""); err != nil {
		t.Fatalf("SetLastUserPromptAt with empty string failed: %v", err)
	}

	got, _ = db.GetMission(m.ID)
	if got.LastUserPromptAt == nil {
		t.Fatal("expected last_user_prompt_at to be preserved, got nil")
	}
	if !got.LastUserPromptAt.Equal(*original) {
		t.Errorf("expected last_user_prompt_at to be unchanged, got %v (was %v)", got.LastUserPromptAt, original)
	}
}
```

**Step 2: Run test to verify it passes** (SetLastUserPromptAt already skips empty from Task 1)

Run: `go test ./internal/database/ -run TestSetLastUserPromptAt_SkipsEmpty -v`

**Step 3: Update the wrapper to track and report lastUserPromptAt**

In `internal/wrapper/wrapper.go`, add a field to the `Wrapper` struct (after `needsAttention`):

```go
lastUserPromptAt time.Time // zero value means no prompt yet this session
```

In `handleClaudeUpdate`, in the `"UserPromptSubmit"` case (line 452), add after `w.needsAttention = false`:

```go
w.lastUserPromptAt = time.Now().UTC()
```

In `writeHeartbeat` (line 614), update the heartbeat call to include `lastUserPromptAt`:

```go
func (w *Wrapper) writeHeartbeat(ctx context.Context) {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.stateMu.RLock()
			lastPromptAt := w.lastUserPromptAt
			w.stateMu.RUnlock()

			var lastPromptAtStr string
			if !lastPromptAt.IsZero() {
				lastPromptAtStr = lastPromptAt.Format(time.RFC3339)
			}
			if err := w.client.Heartbeat(w.missionID, w.tmuxPaneID, lastPromptAtStr); err != nil {
				w.logger.Warn("Failed to write heartbeat", "error", err)
			}
		}
	}
}
```

Also update the two initial heartbeat calls in `Run()` (line 214) and `RunHeadless()` (line 701) to pass empty string for the new parameter:

```go
if err := w.client.Heartbeat(w.missionID, w.tmuxPaneID, ""); err != nil {
```

**Step 4: Update the client Heartbeat method**

In `internal/server/client.go`, update `Heartbeat`:

```go
func (c *Client) Heartbeat(id string, paneID string, lastUserPromptAt string) error {
	body := map[string]string{}
	if paneID != "" {
		body["pane_id"] = paneID
	}
	if lastUserPromptAt != "" {
		body["last_user_prompt_at"] = lastUserPromptAt
	}
	return c.Post("/missions/"+id+"/heartbeat", body, nil)
}
```

**Step 5: Update HeartbeatRequest and handleHeartbeat on the server**

In `internal/server/missions.go`, update `HeartbeatRequest`:

```go
type HeartbeatRequest struct {
	PaneID           string `json:"pane_id"`
	LastUserPromptAt string `json:"last_user_prompt_at"`
}
```

In `handleHeartbeat`, after the pane_id handling (line 953), add:

```go
if req.LastUserPromptAt != "" {
	if err := s.db.SetLastUserPromptAt(resolvedID, req.LastUserPromptAt); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to set last_user_prompt_at: %s", err.Error())
	}
}
```

**Step 6: Add `LastUserPromptAt` to MissionResponse and conversion functions**

In `internal/server/missions.go`, add to `MissionResponse` struct (after `LastHeartbeat`):

```go
LastUserPromptAt *time.Time `json:"last_user_prompt_at"`
```

In `toMissionResponse`, add:

```go
LastUserPromptAt: m.LastUserPromptAt,
```

In `ToMission` (the reverse conversion), add:

```go
LastUserPromptAt: mr.LastUserPromptAt,
```

**Step 7: Run all tests**

Run: `go test ./internal/database/ ./internal/server/ ./internal/wrapper/ -v`
Expected: PASS

**Step 8: Commit**

```
git add internal/wrapper/wrapper.go internal/server/missions.go internal/server/client.go internal/database/database_test.go
git commit -m "Include last_user_prompt_at in heartbeat payload"
```

---

Task 4: Remove `idle_prompt` from attention group
---------------------------------------------------

**Files:**
- Modify: `internal/wrapper/wrapper.go:470`

**Step 1: Make the change**

In `internal/wrapper/wrapper.go`, change line 470 from:

```go
case "permission_prompt", "idle_prompt", "elicitation_dialog":
```

to:

```go
case "permission_prompt", "elicitation_dialog":
```

**Step 2: Run wrapper tests**

Run: `go test ./internal/wrapper/ -v`
Expected: PASS

**Step 3: Commit**

```
git add internal/wrapper/wrapper.go
git commit -m "Remove idle_prompt from needs_attention group"
```

---

Task 5: Add three-tier mission sort function
----------------------------------------------

**Files:**
- Create: `cmd/mission_sort.go`
- Create: `cmd/mission_sort_test.go`

**Step 1: Write the failing test**

Create `cmd/mission_sort_test.go`:

```go
package cmd

import (
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/database"
)

func timePtr(t time.Time) *time.Time { return &t }
func strPtr(s string) *string        { return &s }

func TestSortMissionsForPicker(t *testing.T) {
	now := time.Now().UTC()

	needsAttention := strPtr("needs_attention")
	busy := strPtr("busy")
	idle := strPtr("idle")

	tests := []struct {
		name     string
		missions []*database.Mission
		wantIDs  []string // expected order of ShortIDs
	}{
		{
			name: "needs_attention sorts first",
			missions: []*database.Mission{
				{ShortID: "busy1", ClaudeState: busy, LastHeartbeat: timePtr(now)},
				{ShortID: "attn1", ClaudeState: needsAttention, LastHeartbeat: timePtr(now)},
				{ShortID: "idle1", ClaudeState: idle, LastHeartbeat: timePtr(now)},
			},
			wantIDs: []string{"attn1", "busy1", "idle1"},
		},
		{
			name: "within same tier, sort by last_user_prompt_at DESC",
			missions: []*database.Mission{
				{ShortID: "old", ClaudeState: busy, LastUserPromptAt: timePtr(now.Add(-1 * time.Hour))},
				{ShortID: "new", ClaudeState: busy, LastUserPromptAt: timePtr(now)},
			},
			wantIDs: []string{"new", "old"},
		},
		{
			name: "missions with prompt history sort before those without",
			missions: []*database.Mission{
				{ShortID: "noprompt", ClaudeState: busy, LastHeartbeat: timePtr(now)},
				{ShortID: "prompted", ClaudeState: busy, LastUserPromptAt: timePtr(now.Add(-1 * time.Hour))},
			},
			wantIDs: []string{"prompted", "noprompt"},
		},
		{
			name: "fallback to heartbeat then created_at",
			missions: []*database.Mission{
				{ShortID: "created", CreatedAt: now.Add(-2 * time.Hour)},
				{ShortID: "heartbeat", LastHeartbeat: timePtr(now.Add(-1 * time.Hour))},
			},
			wantIDs: []string{"heartbeat", "created"},
		},
		{
			name: "nil claude_state treated as non-attention",
			missions: []*database.Mission{
				{ShortID: "stopped", ClaudeState: nil, LastUserPromptAt: timePtr(now)},
				{ShortID: "attn", ClaudeState: needsAttention, LastUserPromptAt: timePtr(now.Add(-1 * time.Hour))},
			},
			wantIDs: []string{"attn", "stopped"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sortMissionsForPicker(tt.missions)
			for i, want := range tt.wantIDs {
				if tt.missions[i].ShortID != want {
					t.Errorf("position %d: want %s, got %s", i, want, tt.missions[i].ShortID)
				}
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./cmd/ -run TestSortMissionsForPicker -v`
Expected: FAIL — `sortMissionsForPicker` doesn't exist yet.

**Step 3: Write the sort function**

Create `cmd/mission_sort.go`:

```go
package cmd

import (
	"sort"

	"github.com/odyssey/agenc/internal/database"
)

// sortMissionsForPicker sorts missions in-place using three tiers:
//  1. Missions with claude_state "needs_attention" float to the top
//  2. Sorted by last_user_prompt_at DESC (nil sorts after non-nil)
//  3. Fallback to COALESCE(last_heartbeat, created_at) DESC
func sortMissionsForPicker(missions []*database.Mission) {
	sort.SliceStable(missions, func(i, j int) bool {
		mi, mj := missions[i], missions[j]

		// Tier 1: needs_attention first
		iAttn := mi.ClaudeState != nil && *mi.ClaudeState == "needs_attention"
		jAttn := mj.ClaudeState != nil && *mj.ClaudeState == "needs_attention"
		if iAttn != jAttn {
			return iAttn
		}

		// Tier 2: sort by last_user_prompt_at DESC (non-nil before nil)
		iPrompt := mi.LastUserPromptAt
		jPrompt := mj.LastUserPromptAt
		if (iPrompt != nil) != (jPrompt != nil) {
			return iPrompt != nil
		}
		if iPrompt != nil && jPrompt != nil && !iPrompt.Equal(*jPrompt) {
			return iPrompt.After(*jPrompt)
		}

		// Tier 3: fallback to coalesce(last_heartbeat, created_at) DESC
		iTime := mi.CreatedAt
		if mi.LastHeartbeat != nil {
			iTime = *mi.LastHeartbeat
		}
		jTime := mj.CreatedAt
		if mj.LastHeartbeat != nil {
			jTime = *mj.LastHeartbeat
		}
		return iTime.After(jTime)
	})
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./cmd/ -run TestSortMissionsForPicker -v`
Expected: PASS

**Step 5: Commit**

```
git add cmd/mission_sort.go cmd/mission_sort_test.go
git commit -m "Add three-tier mission sort function for picker"
```

---

Task 6: Wire sort into mission attach picker
----------------------------------------------

**Files:**
- Modify: `cmd/mission_attach.go:56`

**Step 1: Add sort call before building picker entries**

In `cmd/mission_attach.go`, after fetching missions (line 48) and before `buildMissionPickerEntries` (line 56), add:

```go
sortMissionsForPicker(missions)
```

**Step 2: Run the full test suite**

Run: `make check` (with sandbox disabled)
Expected: PASS

**Step 3: Commit**

```
git add cmd/mission_attach.go
git commit -m "Wire three-tier sort into mission attach picker"
```

---

Task 7: Update architecture docs
----------------------------------

**Files:**
- Modify: `docs/system-architecture.md` — if the heartbeat payload or DB schema sections need updating

**Step 1: Read the architecture doc and update any sections that mention the heartbeat payload, mission sorting, or the missions table schema.**

**Step 2: Commit**

```
git add docs/system-architecture.md
git commit -m "Update architecture docs for mission picker sorting"
```
