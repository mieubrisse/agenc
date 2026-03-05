Code Quality Fixes Implementation Plan
=======================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix three high-priority code quality issues: enable SQLite foreign keys, add panic recovery to server goroutines with health observability, and propagate time-parse errors in database scanners.

**Architecture:** Three independent fixes applied sequentially. Fix 1 modifies the database layer. Fix 2 adds a goroutine supervision helper to the server and extends the health endpoint + CLI. Fix 3 is a mechanical edit to scanner functions.

**Tech Stack:** Go, SQLite (modernc.org/sqlite), Cobra CLI

---

Task 1: Enable SQLite Foreign Key Constraints
----------------------------------------------

**Files:**
- Modify: `internal/database/database.go:19`

**Step 1: Write the failing test**

Add to `internal/database/sessions_test.go`:

```go
func TestCreateSessionRejectsBadMissionID(t *testing.T) {
	db := openTestDB(t)

	_, err := db.CreateSession("nonexistent-mission-id", "session-123")
	if err == nil {
		t.Fatal("expected error when creating session with nonexistent mission ID, got nil")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/database/ -run TestCreateSessionRejectsBadMissionID -v`
Expected: FAIL — the test expects an error but `CreateSession` succeeds because foreign keys are not enforced.

**Step 3: Enable foreign keys in DSN**

In `internal/database/database.go:19`, change:

```go
dsn := dbFilepath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
```

to:

```go
dsn := dbFilepath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/database/ -run TestCreateSessionRejectsBadMissionID -v`
Expected: PASS

**Step 5: Run the full database test suite**

Run: `go test ./internal/database/ -v`
Expected: All tests pass. The existing `TestSessionsCascadeDeleteWithMission` test already enables foreign keys manually — this is now redundant but harmless.

**Step 6: Remove redundant manual PRAGMA from cascade test**

In `internal/database/sessions_test.go`, remove lines 359-362 from `TestSessionsCascadeDeleteWithMission`:

```go
// Remove this block — foreign keys are now enabled globally via DSN
if _, err := db.conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
    t.Fatalf("failed to enable foreign keys: %v", err)
}
```

**Step 7: Run tests again**

Run: `go test ./internal/database/ -v`
Expected: All tests pass.

**Step 8: Commit**

```
git add internal/database/database.go internal/database/sessions_test.go
git commit -m "Enable SQLite foreign key constraints via DSN pragma"
```

---

Task 2: Add Orphaned Sessions Cleanup Migration
------------------------------------------------

**Files:**
- Modify: `internal/database/migrations.go`
- Modify: `internal/database/database.go`

**Step 1: Write the failing test**

Add to `internal/database/sessions_test.go`:

```go
func TestOrphanedSessionsCleanedOnOpen(t *testing.T) {
	dbFilepath := filepath.Join(t.TempDir(), "test.sqlite")

	// Open the database to create the schema
	db1, err := Open(dbFilepath)
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}

	// Create a mission and a session
	mission, err := db1.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("CreateMission failed: %v", err)
	}
	if _, err := db1.CreateSession(mission.ID, "legit-session"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Insert an orphaned session directly via SQL (bypass FK check by
	// temporarily disabling foreign keys at the connection level)
	if _, err := db1.conn.Exec("PRAGMA foreign_keys = OFF"); err != nil {
		t.Fatalf("failed to disable foreign keys: %v", err)
	}
	if _, err := db1.conn.Exec(
		"INSERT INTO sessions (id, short_id, mission_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		"orphan-session", "orphan-s", "nonexistent-mission", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z",
	); err != nil {
		t.Fatalf("failed to insert orphaned session: %v", err)
	}
	db1.Close()

	// Re-open the database — the migration should clean up the orphan
	db2, err := Open(dbFilepath)
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}
	defer db2.Close()

	// The orphaned session should be gone
	got, err := db2.GetSession("orphan-session")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected orphaned session to be cleaned up, but it still exists")
	}

	// The legitimate session should still exist
	legit, err := db2.GetSession("legit-session")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if legit == nil {
		t.Error("expected legitimate session to survive cleanup, but it was deleted")
	}
}
```

Note: You will also need to add `"path/filepath"` to the imports of `sessions_test.go`.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/database/ -run TestOrphanedSessionsCleanedOnOpen -v`
Expected: FAIL — the orphan session survives because no cleanup migration exists yet.

**Step 3: Add the cleanup migration**

In `internal/database/migrations.go`, add the constant:

```go
const cleanOrphanedSessionsSQL = `DELETE FROM sessions WHERE mission_id NOT IN (SELECT id FROM missions);`
```

Add the migration function:

```go
// migrateCleanOrphanedSessions removes sessions whose mission_id does not
// reference an existing mission. These orphans could accumulate from periods
// when foreign key constraints were not enforced.
func migrateCleanOrphanedSessions(conn *sql.DB) error {
	_, err := conn.Exec(cleanOrphanedSessionsSQL)
	return err
}
```

In `internal/database/database.go`, add the migration call after the `dropMissionDescriptionsTableSQL` block (after line 121):

```go
if err := migrateCleanOrphanedSessions(conn); err != nil {
    conn.Close()
    return nil, stacktrace.Propagate(err, "failed to clean orphaned sessions")
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/database/ -run TestOrphanedSessionsCleanedOnOpen -v`
Expected: PASS

**Step 5: Run full test suite**

Run: `go test ./internal/database/ -v`
Expected: All tests pass.

**Step 6: Commit**

```
git add internal/database/database.go internal/database/migrations.go internal/database/sessions_test.go
git commit -m "Add migration to clean orphaned sessions on DB open"
```

---

Task 3: Propagate Time-Parse Errors in Database Scanners
---------------------------------------------------------

**Files:**
- Modify: `internal/database/scanners.go:21,25,40,41,59,63,78,79`
- Modify: `internal/database/sessions.go:171,172,185,186`

**Step 1: Fix scanMissions in scanners.go**

Replace lines 20-22 with:

```go
if lastHeartbeat.Valid {
    t, err := time.Parse(time.RFC3339, lastHeartbeat.String)
    if err != nil {
        return nil, stacktrace.Propagate(err, "failed to parse last_heartbeat timestamp")
    }
    m.LastHeartbeat = &t
}
```

Replace lines 24-26 with:

```go
if sessionNameUpdatedAt.Valid {
    t, err := time.Parse(time.RFC3339, sessionNameUpdatedAt.String)
    if err != nil {
        return nil, stacktrace.Propagate(err, "failed to parse session_name_updated_at timestamp")
    }
    m.SessionNameUpdatedAt = &t
}
```

Replace lines 40-41 with:

```go
var err error
m.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
if err != nil {
    return nil, stacktrace.Propagate(err, "failed to parse created_at timestamp")
}
m.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
if err != nil {
    return nil, stacktrace.Propagate(err, "failed to parse updated_at timestamp")
}
```

**Step 2: Fix scanMission in scanners.go**

Apply the same pattern to `scanMission` (lines 58-79). The `err` variable from the `row.Scan` call on line 55 can be reused:

Replace lines 58-60 with:

```go
if lastHeartbeat.Valid {
    t, err := time.Parse(time.RFC3339, lastHeartbeat.String)
    if err != nil {
        return nil, stacktrace.Propagate(err, "failed to parse last_heartbeat timestamp")
    }
    m.LastHeartbeat = &t
}
```

Replace lines 62-64 with:

```go
if sessionNameUpdatedAt.Valid {
    t, err := time.Parse(time.RFC3339, sessionNameUpdatedAt.String)
    if err != nil {
        return nil, stacktrace.Propagate(err, "failed to parse session_name_updated_at timestamp")
    }
    m.SessionNameUpdatedAt = &t
}
```

Replace lines 78-79 with:

```go
var parseErr error
m.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
if parseErr != nil {
    return nil, stacktrace.Propagate(parseErr, "failed to parse created_at timestamp")
}
m.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
if parseErr != nil {
    return nil, stacktrace.Propagate(parseErr, "failed to parse updated_at timestamp")
}
```

**Step 3: Fix scanSession in sessions.go**

Replace lines 171-172 with:

```go
var err error
s.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
if err != nil {
    return nil, stacktrace.Propagate(err, "failed to parse session created_at timestamp")
}
s.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
if err != nil {
    return nil, stacktrace.Propagate(err, "failed to parse session updated_at timestamp")
}
```

Note: The `err` variable from `row.Scan` on line 168 is in an `if` block scope, so declaring a new `err` here is correct.

**Step 4: Fix scanSessions in sessions.go**

Replace lines 185-186 with:

```go
var parseErr error
s.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
if parseErr != nil {
    return nil, stacktrace.Propagate(parseErr, "failed to parse session created_at timestamp")
}
s.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
if parseErr != nil {
    return nil, stacktrace.Propagate(parseErr, "failed to parse session updated_at timestamp")
}
```

**Step 5: Run full test suite**

Run: `go test ./internal/database/ -v`
Expected: All tests pass (all existing timestamps are well-formed RFC3339).

**Step 6: Commit**

```
git add internal/database/scanners.go internal/database/sessions.go
git commit -m "Propagate time-parse errors in database scanners instead of silently discarding"
```

---

Task 4: Add Panic Recovery Helper and Loop Health Tracking
----------------------------------------------------------

**Files:**
- Modify: `internal/server/server.go`

**Step 1: Write the failing test**

Create `internal/server/run_loop_test.go`:

```go
package server

import (
	"context"
	"log"
	"os"
	"sync"
	"testing"
	"time"
)

func TestRunLoop_NormalCompletion(t *testing.T) {
	s := &Server{
		logger: log.New(os.Stderr, "", 0),
	}

	var wg sync.WaitGroup
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so the loop exits

	go s.runLoop("test-loop", &wg, func(ctx context.Context) {
		<-ctx.Done()
	})

	wg.Wait()

	val, ok := s.loopHealth.Load("test-loop")
	if !ok {
		t.Fatal("expected loop health entry for 'test-loop'")
	}
	if val != "stopped" {
		t.Errorf("expected status 'stopped', got %q", val)
	}
}

func TestRunLoop_PanicRecovery(t *testing.T) {
	s := &Server{
		logger: log.New(os.Stderr, "", 0),
	}

	var wg sync.WaitGroup

	go s.runLoop("panic-loop", &wg, func(ctx context.Context) {
		panic("test panic")
	})

	wg.Wait()

	val, ok := s.loopHealth.Load("panic-loop")
	if !ok {
		t.Fatal("expected loop health entry for 'panic-loop'")
	}
	if val != "crashed" {
		t.Errorf("expected status 'crashed', got %q", val)
	}
}

func TestRunLoop_HealthSetToRunningDuringExecution(t *testing.T) {
	s := &Server{
		logger: log.New(os.Stderr, "", 0),
	}

	var wg sync.WaitGroup
	started := make(chan struct{})

	go s.runLoop("running-loop", &wg, func(ctx context.Context) {
		close(started)
		time.Sleep(50 * time.Millisecond)
	})

	<-started

	val, ok := s.loopHealth.Load("running-loop")
	if !ok {
		t.Fatal("expected loop health entry for 'running-loop'")
	}
	if val != "running" {
		t.Errorf("expected status 'running' during execution, got %q", val)
	}

	wg.Wait()
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestRunLoop -v`
Expected: FAIL — `runLoop` method and `loopHealth` field don't exist yet.

**Step 3: Add loopHealth field and runLoop method**

In `internal/server/server.go`, add the `loopHealth` field to the `Server` struct (after the `stashInProgress` field):

```go
// loopHealth tracks the status of each background loop goroutine.
// Values are "running", "stopped", or "crashed".
loopHealth sync.Map
```

Add the `runLoop` method:

```go
// runLoop runs a named background loop function with panic recovery and health tracking.
// On normal return, the loop is marked "stopped". On panic, it is marked "crashed"
// and the panic is logged — the loop is NOT restarted.
func (s *Server) runLoop(name string, wg *sync.WaitGroup, fn func(ctx context.Context)) {
	wg.Add(1)
	s.loopHealth.Store(name, "running")
	defer func() {
		if r := recover(); r != nil {
			s.logger.Printf("PANIC in background loop %q: %v", name, r)
			s.loopHealth.Store(name, "crashed")
		} else {
			s.loopHealth.Store(name, "stopped")
		}
		wg.Done()
	}()
	fn(context.TODO()) // placeholder — caller will pass real ctx after refactor
}
```

Wait — the `fn` should receive the real context. But `runLoop` is called with `go s.runLoop(...)`, so the context needs to be passed in. Update the signature:

```go
func (s *Server) runLoop(name string, wg *sync.WaitGroup, ctx context.Context, fn func(ctx context.Context)) {
	wg.Add(1)
	s.loopHealth.Store(name, "running")
	defer func() {
		if r := recover(); r != nil {
			s.logger.Printf("PANIC in background loop %q: %v", name, r)
			s.loopHealth.Store(name, "crashed")
		} else {
			s.loopHealth.Store(name, "stopped")
		}
		wg.Done()
	}()
	fn(ctx)
}
```

Update the tests to match this signature (pass `context.Background()` or a cancelled context as needed).

**Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestRunLoop -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/server/server.go internal/server/run_loop_test.go
git commit -m "Add runLoop helper with panic recovery and loop health tracking"
```

---

Task 5: Refactor Server Goroutines to Use runLoop
--------------------------------------------------

**Files:**
- Modify: `internal/server/server.go:138-194`

**Step 1: Replace bare goroutine blocks**

Replace lines 138-194 with:

```go
// Start HTTP server in a goroutine (not managed by runLoop — it has its own shutdown)
wg.Add(1)
go func() {
    defer wg.Done()
    if err := s.httpServer.Serve(listener); err != http.ErrServerClosed {
        s.logger.Printf("HTTP server error: %v", err)
    }
}()

// Start background loops with panic recovery
go s.runLoop("repo-update-worker", &wg, ctx, s.runRepoUpdateWorker)
go s.runLoop("repo-update-loop", &wg, ctx, s.runRepoUpdateLoop)
go s.runLoop("config-auto-commit", &wg, ctx, s.runConfigAutoCommitLoop)
go s.runLoop("config-watcher", &wg, ctx, s.runConfigWatcherLoop)
go s.runLoop("keybindings-writer", &wg, ctx, s.runKeybindingsWriterLoop)
go s.runLoop("idle-timeout", &wg, ctx, s.runIdleTimeoutLoop)
go s.runLoop("session-scanner", &wg, ctx, s.runSessionScannerLoop)
go s.runLoop("session-summarizer", &wg, ctx, s.runSessionSummarizerWorker)
```

**Step 2: Run full server test suite**

Run: `go test ./internal/server/ -v`
Expected: All tests pass.

**Step 3: Commit**

```
git add internal/server/server.go
git commit -m "Refactor server background goroutines to use runLoop with panic recovery"
```

---

Task 6: Extend Health Endpoint with Loop Status
------------------------------------------------

**Files:**
- Modify: `internal/server/server.go` (handleHealth function)

**Step 1: Update handleHealth**

Replace the `handleHealth` method with:

```go
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) error {
	loops := make(map[string]string)
	s.loopHealth.Range(func(key, value any) bool {
		loops[key.(string)] = value.(string)
		return true
	})

	status := "ok"
	for _, loopStatus := range loops {
		if loopStatus == "crashed" {
			status = "degraded"
			break
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":  status,
		"version": version.Version,
		"loops":   loops,
	})
	return nil
}
```

**Step 2: Run server tests**

Run: `go test ./internal/server/ -v`
Expected: All tests pass.

**Step 3: Commit**

```
git add internal/server/server.go
git commit -m "Extend /health endpoint with loop status and degraded state detection"
```

---

Task 7: Add GetHealth Client Method and Update server status CLI
----------------------------------------------------------------

**Files:**
- Modify: `internal/server/client.go`
- Modify: `cmd/server_status.go`

**Step 1: Add HealthResponse type and GetHealth method to client.go**

Add to `internal/server/client.go`:

```go
// HealthResponse represents the response from the /health endpoint.
type HealthResponse struct {
	Status  string            `json:"status"`
	Version string            `json:"version"`
	Loops   map[string]string `json:"loops"`
}

// GetHealth calls the /health endpoint and returns the server health status.
func (c *Client) GetHealth() (*HealthResponse, error) {
	var result HealthResponse
	if err := c.Get("/health", &result); err != nil {
		return nil, err
	}
	return &result, nil
}
```

**Step 2: Update server status CLI**

Replace `cmd/server_status.go` with:

```go
package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
)

var serverStatusCmd = &cobra.Command{
	Use:   statusCmdStr,
	Short: "Check AgenC server status",
	RunE:  runServerStatus,
}

func init() {
	serverCmd.AddCommand(serverStatusCmd)
}

func runServerStatus(cmd *cobra.Command, args []string) error {
	if _, err := ensureConfigured(); err != nil {
		return err
	}

	pidFilepath := config.GetServerPIDFilepath(agencDirpath)
	pid, err := server.ReadPID(pidFilepath)
	if err != nil {
		return err
	}

	if pid <= 0 || !server.IsRunning(pidFilepath) {
		fmt.Println("Server is not running.")
		return nil
	}

	fmt.Printf("Server is running (PID %d).\n", pid)

	// Try to get detailed health from the server
	socketPath := config.GetServerSocketPath(agencDirpath)
	client := server.NewClient(socketPath)
	health, err := client.GetHealth()
	if err != nil {
		fmt.Printf("  (could not reach health endpoint: %v)\n", err)
		return nil
	}

	if len(health.Loops) > 0 {
		fmt.Println()
		fmt.Println("Loops:")

		names := make([]string, 0, len(health.Loops))
		for name := range health.Loops {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			status := health.Loops[name]
			marker := greenText("●")
			if status == "crashed" {
				marker = redText("●")
			} else if status == "stopped" {
				marker = yellowText("●")
			}
			fmt.Printf("  %s %-25s %s\n", marker, name, status)
		}
	}

	return nil
}
```

Note: The `greenText`, `redText`, `yellowText` functions should already exist in `cmd/ansi_colors.go`. If not, use plain text formatting instead. Check the file first.

**Step 3: Run CLI tests and build**

Run: `go test ./cmd/ -v`
Run: `make build` (with `dangerouslyDisableSandbox: true`)
Expected: All tests pass, binary builds.

**Step 4: Commit**

```
git add internal/server/client.go cmd/server_status.go
git commit -m "Add loop health display to server status CLI"
```

---

Task 8: Final Verification
---------------------------

**Step 1: Run the full test suite**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: All tests pass (wrapper integration tests may fail due to sandbox — this is pre-existing).

**Step 2: Push**

```
git pull --rebase
git push
```
