Ephemeral Pane IDs Implementation Plan
=======================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make tmux pane IDs ephemeral — refreshed by wrapper heartbeats and reconciled on server startup — so that stale pane IDs from tmux server restarts cannot cause mission/pane mismatches.

**Architecture:** The wrapper sends its `$TMUX_PANE` with every heartbeat. The server stores it in the existing `tmux_pane` DB column (treated as a volatile cache). On startup, the server clears all pane IDs and repopulates them by matching agenc-pool pane PIDs to wrapper PID files. A staleness reaper in the idle timeout loop clears pane IDs for wrappers that stop heartbeating.

**Tech Stack:** Go, SQLite, tmux CLI

---

Task 1: Add ClearAllTmuxPanes to database
------------------------------------------

**Files:**
- Modify: `internal/database/missions.go:286-296` (after ClearTmuxPane)
- Test: `internal/database/database_test.go`

**Step 1: Write the test**

Add to `internal/database/database_test.go`:

```go
func TestClearAllTmuxPanes(t *testing.T) {
	db := setupTestDB(t)

	// Create two missions with pane IDs
	m1, err := db.CreateMission("repo1", nil)
	if err != nil {
		t.Fatal(err)
	}
	m2, err := db.CreateMission("repo2", nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := db.SetTmuxPane(m1.ID, "42"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetTmuxPane(m2.ID, "99"); err != nil {
		t.Fatal(err)
	}

	// Clear all
	if err := db.ClearAllTmuxPanes(); err != nil {
		t.Fatalf("ClearAllTmuxPanes failed: %v", err)
	}

	// Verify both are cleared
	got1, _ := db.GetMission(m1.ID)
	got2, _ := db.GetMission(m2.ID)
	if got1.TmuxPane != nil {
		t.Errorf("expected nil pane for m1, got %v", *got1.TmuxPane)
	}
	if got2.TmuxPane != nil {
		t.Errorf("expected nil pane for m2, got %v", *got2.TmuxPane)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/database/ -run TestClearAllTmuxPanes -v`
Expected: FAIL — `ClearAllTmuxPanes` not defined

**Step 3: Implement**

Add to `internal/database/missions.go` after `ClearTmuxPane`:

```go
// ClearAllTmuxPanes removes the tmux pane association for all missions.
// Used on server startup to clear stale pane IDs before reconciling with
// the actual tmux state.
func (db *DB) ClearAllTmuxPanes() error {
	_, err := db.conn.Exec("UPDATE missions SET tmux_pane = NULL")
	if err != nil {
		return stacktrace.Propagate(err, "failed to clear all tmux panes")
	}
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/database/ -run TestClearAllTmuxPanes -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/database/missions.go internal/database/database_test.go
git commit -m "Add ClearAllTmuxPanes database function"
```

---

Task 2: Enhance heartbeat to carry pane ID
-------------------------------------------

**Files:**
- Modify: `internal/server/missions.go:850-866` (handleHeartbeat)
- Modify: `internal/server/client.go:277-280` (Heartbeat client method)
- Modify: `internal/wrapper/wrapper.go:207-212` (initial heartbeat call)
- Modify: `internal/wrapper/wrapper.go:607-622` (periodic heartbeat loop)
- Modify: `internal/wrapper/wrapper.go:691-696` (headless initial heartbeat)

**Step 1: Add HeartbeatRequest struct and update handler**

In `internal/server/missions.go`, replace `handleHeartbeat` (lines 850-866):

```go
// HeartbeatRequest is the optional JSON body for POST /missions/{id}/heartbeat.
type HeartbeatRequest struct {
	PaneID string `json:"pane_id"`
}

// handleHeartbeat handles POST /missions/{id}/heartbeat.
// Updates the mission's last_heartbeat timestamp and optionally its tmux pane.
func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if err := s.db.UpdateHeartbeat(resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to update heartbeat: %s", err.Error())
	}

	// Update tmux pane if provided (wrappers running in tmux send their $TMUX_PANE)
	var req HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err == nil && req.PaneID != "" {
		if err := s.db.SetTmuxPane(resolvedID, req.PaneID); err != nil {
			s.logger.Printf("Warning: failed to set tmux pane for mission %s: %v", id, err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
```

**Step 2: Update client to accept pane ID**

In `internal/server/client.go`, replace `Heartbeat` (lines 277-280):

```go
// HeartbeatRequest is the JSON body for POST /missions/{id}/heartbeat.
type HeartbeatRequest struct {
	PaneID string `json:"pane_id,omitempty"`
}

// Heartbeat updates a mission's last_heartbeat timestamp and tmux pane.
func (c *Client) Heartbeat(id string, paneID string) error {
	body := HeartbeatRequest{PaneID: paneID}
	return c.Post("/missions/"+id+"/heartbeat", body, nil)
}
```

**Step 3: Update wrapper to send pane ID with heartbeats**

The wrapper needs a helper to get the pane ID. In `internal/wrapper/wrapper.go`,
add a field and populate it early:

Add a `tmuxPaneID` field to the `Wrapper` struct. During construction or early in
`Run`/`RunHeadless`, read `$TMUX_PANE`, strip the `%` prefix, and store it. Then
pass it to every `Heartbeat` call.

Update the three heartbeat call sites:

1. Interactive initial heartbeat (line 209):
   `w.client.Heartbeat(w.missionID, w.tmuxPaneID)`

2. Periodic heartbeat loop (line 618):
   `w.client.Heartbeat(w.missionID, w.tmuxPaneID)`

3. Headless initial heartbeat (line 693):
   `w.client.Heartbeat(w.missionID, w.tmuxPaneID)`

The `tmuxPaneID` field will be empty for headless wrappers (no `$TMUX_PANE`),
which causes the server to skip the `SetTmuxPane` call.

**Step 4: Run all tests**

Run: `make check`
Expected: All tests pass (heartbeat is not directly unit-tested — it's an HTTP
handler exercised by integration tests. The key is that the build compiles and
existing tests still pass.)

**Step 5: Commit**

```bash
git add internal/server/missions.go internal/server/client.go internal/wrapper/wrapper.go
git commit -m "Send tmux pane ID with wrapper heartbeats"
```

---

Task 3: Add startup pane reconciliation
----------------------------------------

**Files:**
- Modify: `internal/server/server.go:117-120` (after ensurePoolSession, before HTTP listen)
- Create: `internal/server/pane_reconcile.go` (new file for reconcilePaneIDs)
- Test: `internal/server/pane_reconcile_test.go`

**Step 1: Write the reconciliation function**

Create `internal/server/pane_reconcile.go`:

```go
package server

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

// reconcilePaneIDs clears all stored tmux pane IDs and repopulates them by
// matching running wrapper PIDs against the actual panes in the agenc-pool
// tmux session. This runs synchronously on server startup before the HTTP
// server begins accepting requests.
//
// After this function returns, every active mission's tmux_pane is either:
//   - set to the correct pane ID (wrapper is alive and has a pool pane), or
//   - NULL (wrapper is not running, or pool session doesn't exist)
func (s *Server) reconcilePaneIDs() {
	// Step 1: Clear all stored pane IDs
	if err := s.db.ClearAllTmuxPanes(); err != nil {
		s.logger.Printf("Warning: failed to clear tmux panes on startup: %v", err)
		return
	}

	// Step 2: Query agenc-pool for (paneID, panePID) pairs
	poolPanes, err := listPoolPanesWithPIDs()
	if err != nil {
		s.logger.Printf("Pane reconciliation: pool session not available (%v), skipping", err)
		return
	}

	if len(poolPanes) == 0 {
		s.logger.Printf("Pane reconciliation: no panes in pool, nothing to reconcile")
		return
	}

	// Step 3: Build PID -> paneID lookup from tmux
	pidToPaneID := make(map[int]string, len(poolPanes))
	for _, pp := range poolPanes {
		pidToPaneID[pp.pid] = pp.paneID
	}

	// Step 4: For each active mission, check if its wrapper PID matches a pool pane
	missions, err := s.db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		s.logger.Printf("Warning: failed to list missions for pane reconciliation: %v", err)
		return
	}

	reconciled := 0
	for _, m := range missions {
		pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, m.ID)
		pid, err := ReadPID(pidFilepath)
		if err != nil || pid == 0 || !IsProcessRunning(pid) {
			continue
		}

		paneID, found := pidToPaneID[pid]
		if !found {
			continue
		}

		if err := s.db.SetTmuxPane(m.ID, paneID); err != nil {
			s.logger.Printf("Warning: failed to set pane for mission %s: %v", database.ShortID(m.ID), err)
			continue
		}
		reconciled++
	}

	s.logger.Printf("Pane reconciliation: matched %d missions to pool panes", reconciled)
}

// poolPaneInfo holds a pane ID and the PID of the process running in that pane.
type poolPaneInfo struct {
	paneID string
	pid    int
}

// listPoolPanesWithPIDs queries tmux for all panes in the agenc-pool session
// and returns their pane IDs (without "%" prefix) and process PIDs.
func listPoolPanesWithPIDs() ([]poolPaneInfo, error) {
	cmd := exec.Command("tmux", "list-panes", "-s", "-t", poolSessionName, "-F", "#{pane_id} #{pane_pid}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes failed: %w", err)
	}

	var result []poolPaneInfo
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		paneID := strings.TrimPrefix(parts[0], "%")
		pid := 0
		fmt.Sscanf(parts[1], "%d", &pid)
		if pid > 0 {
			result = append(result, poolPaneInfo{paneID: paneID, pid: pid})
		}
	}
	return result, nil
}
```

**Step 2: Wire into server startup**

In `internal/server/server.go`, after `ensurePoolSession()` (line 120) and before
the launchctl check (line 122), add:

```go
	// Reconcile tmux pane IDs with actual pool state
	s.reconcilePaneIDs()
```

**Step 3: Run build and tests**

Run: `make check`
Expected: All tests pass. (reconcilePaneIDs depends on tmux being available, so
it cannot be meaningfully unit-tested without mocking tmux. The function is
structured so that every failure path logs and returns gracefully.)

**Step 4: Commit**

```bash
git add internal/server/pane_reconcile.go internal/server/server.go
git commit -m "Reconcile tmux pane IDs on server startup"
```

---

Task 4: Add staleness reaper to idle timeout loop
--------------------------------------------------

**Files:**
- Modify: `internal/server/idle_timeout.go:50-87` (runIdleTimeoutCycle)

**Step 1: Add staleness constant and reaping logic**

In `internal/server/idle_timeout.go`, add a constant after the existing constants
(line 21):

```go
	// staleHeartbeatThreshold is how long after the last heartbeat before a
	// mission's tmux_pane is considered stale and cleared. Set to 3x the
	// wrapper heartbeat interval (10s) to tolerate occasional delays.
	staleHeartbeatThreshold = 30 * time.Second
```

Add a new function after `runIdleTimeoutCycle`:

```go
// reapStalePaneIDs clears tmux_pane for missions whose wrapper has stopped
// heartbeating. This catches wrapper crashes and tmux restarts that happen
// during normal operation (not just at server startup).
func (s *Server) reapStalePaneIDs(missions []*database.Mission, now time.Time) {
	for _, m := range missions {
		if m.TmuxPane == nil {
			continue
		}

		isStale := m.LastHeartbeat == nil || now.Sub(*m.LastHeartbeat) > staleHeartbeatThreshold
		if !isStale {
			continue
		}

		if err := s.db.ClearTmuxPane(m.ID); err != nil {
			s.logger.Printf("Warning: failed to clear stale pane for mission %s: %v", database.ShortID(m.ID), err)
			continue
		}
		s.logger.Printf("Cleared stale tmux pane for mission %s (last heartbeat: %v)", database.ShortID(m.ID), m.LastHeartbeat)
	}
}
```

Then call it from `runIdleTimeoutCycle`, right after the `missions` variable is
populated (after line 55):

```go
	s.reapStalePaneIDs(missions, now)
```

Note: `now` is already declared at line 59. Move the `now := time.Now()` line
to before the `reapStalePaneIDs` call so it's available.

**Step 2: Run build and tests**

Run: `make check`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add internal/server/idle_timeout.go
git commit -m "Add staleness reaper for tmux pane IDs in idle timeout loop"
```

---

Task 5: Remove startup percent-stripping migration
---------------------------------------------------

The `stripTmuxPanePercentSQL` migration in `internal/database/database.go:117-121`
runs on every startup to strip `%` from pane IDs. Now that we clear all pane IDs
on startup via `reconcilePaneIDs`, this migration is redundant — the pane IDs
will either be NULL (cleared) or freshly populated (already without `%`).

**Files:**
- Modify: `internal/database/database.go:117-121`

**Step 1: Remove the percent-stripping block**

In `internal/database/database.go`, remove the block that runs
`stripTmuxPanePercentSQL` on every Open(). Keep the SQL constant in
`migrations.go` in case it's referenced, but stop executing it on startup.

**Step 2: Run build and tests**

Run: `make check`
Expected: All tests pass.

**Step 3: Commit**

```bash
git add internal/database/database.go
git commit -m "Remove redundant percent-stripping on startup (reconcilePaneIDs handles it)"
```

---

Task 6: Manual integration test
--------------------------------

**Step 1: Build**

Run: `make build`

**Step 2: Test startup reconciliation**

1. Start some missions: `./agenc mission new <repo>`
2. Restart the server: `./agenc server stop` then `./agenc server start`
3. Verify missions show correct pane IDs: `./agenc mission ls`
4. Check server logs for "Pane reconciliation: matched N missions"

**Step 3: Test staleness reaper**

1. Start a mission
2. Kill the wrapper process directly: `kill <pid>`
3. Wait ~2 minutes for idle timeout cycle
4. Check server logs for "Cleared stale tmux pane for mission"
5. Verify mission shows no pane: `./agenc mission ls`

**Step 4: Test heartbeat registration**

1. Start a mission
2. Check server logs for heartbeat messages
3. Restart the server
4. Within 10 seconds, verify the pane ID reappears via `./agenc mission ls`
   (wrapper's next periodic heartbeat re-registers the pane)
