Server Singleton Enforcement Implementation Plan
=================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ensure only one `agenc server` process runs at a time, and `server stop` kills all orphaned server processes.

**Architecture:** Two mechanisms: (1) an exclusive flock in `Server.Run()` that prevents concurrent servers — the loser exits cleanly; (2) an orphan sweep in `StopServer()` that finds and kills all `agenc server start` processes system-wide.

**Tech Stack:** Go stdlib (`syscall.Flock`, `os/exec` for `pgrep`/`ps`)

**Design doc:** `docs/plans/2026-03-09-server-singleton-design.md`

---

Task 1: Add `GetServerLockFilepath` config helper
--------------------------------------------------

**Files:**
- Modify: `internal/config/config.go:31-36` (add constant)
- Modify: `internal/config/config.go:168-176` (add function after `GetServerSocketFilepath`)

**Step 1: Add the constant**

In the constants block after `ServerSocketFilename`:

```go
ServerLockFilename   = "server.lock"
```

**Step 2: Add the path helper**

After `GetServerSocketFilepath`:

```go
// GetServerLockFilepath returns the path to the server lock file used for
// singleton enforcement via flock.
func GetServerLockFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ServerDirname, ServerLockFilename)
}
```

**Step 3: Commit**

```
git add internal/config/config.go
git commit -m "Add GetServerLockFilepath config helper"
```

---

Task 2: Add `tryAcquireServerLock` function
-------------------------------------------

**Files:**
- Modify: `internal/server/process.go` (add function and import)
- Create: `internal/server/process_test.go` (add test)

**Step 1: Write the test**

Create `internal/server/process_test.go`:

```go
package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTryAcquireServerLock_FirstCallerSucceeds(t *testing.T) {
	lockFilepath := filepath.Join(t.TempDir(), "server.lock")

	lockFile, err := tryAcquireServerLock(lockFilepath)
	if err != nil {
		t.Fatalf("expected lock to succeed, got error: %v", err)
	}
	defer lockFile.Close()

	if lockFile == nil {
		t.Fatal("expected non-nil file handle")
	}
}

func TestTryAcquireServerLock_SecondCallerFails(t *testing.T) {
	lockFilepath := filepath.Join(t.TempDir(), "server.lock")

	// First caller acquires the lock
	lockFile1, err := tryAcquireServerLock(lockFilepath)
	if err != nil {
		t.Fatalf("first lock should succeed: %v", err)
	}
	defer lockFile1.Close()

	// Second caller should get ErrServerLocked
	lockFile2, err := tryAcquireServerLock(lockFilepath)
	if err != ErrServerLocked {
		t.Fatalf("expected ErrServerLocked, got: %v", err)
	}
	if lockFile2 != nil {
		lockFile2.Close()
		t.Fatal("expected nil file handle when lock is held")
	}
}

func TestTryAcquireServerLock_ReleasedLockCanBeReacquired(t *testing.T) {
	lockFilepath := filepath.Join(t.TempDir(), "server.lock")

	// Acquire and release
	lockFile1, err := tryAcquireServerLock(lockFilepath)
	if err != nil {
		t.Fatalf("first lock should succeed: %v", err)
	}
	lockFile1.Close() // releases the flock

	// Should be able to acquire again
	lockFile2, err := tryAcquireServerLock(lockFilepath)
	if err != nil {
		t.Fatalf("second lock should succeed after release: %v", err)
	}
	defer lockFile2.Close()
}

func TestTryAcquireServerLock_CreatesParentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	lockFilepath := filepath.Join(dir, "server.lock")

	lockFile, err := tryAcquireServerLock(lockFilepath)
	if err != nil {
		t.Fatalf("expected lock to succeed, got error: %v", err)
	}
	defer lockFile.Close()

	// Verify the directory was created
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("expected parent directory to be created")
	}
}
```

**Step 2: Run the test to verify it fails**

Run: `go test ./internal/server/ -run TestTryAcquireServerLock -v`
Expected: compilation error — `tryAcquireServerLock` and `ErrServerLocked` do
not exist.

**Step 3: Implement `tryAcquireServerLock`**

Add to `internal/server/process.go`. Add `"errors"` to the imports block.

Add a sentinel error in the `var` block (create one if needed, after the `const`
block):

```go
// ErrServerLocked is returned when another server process holds the lock.
var ErrServerLocked = errors.New("another server is already running")
```

Add the function (place after `IsServerProcess`):

```go
// tryAcquireServerLock attempts to acquire an exclusive flock on the given lock
// file. Returns the open file handle (caller must defer Close) on success, or
// ErrServerLocked if another process holds the lock. Any other error indicates
// a filesystem problem.
func tryAcquireServerLock(lockFilepath string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(lockFilepath), 0755); err != nil {
		return nil, stacktrace.Propagate(err, "failed to create lock file directory")
	}

	f, err := os.OpenFile(lockFilepath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to open lock file")
	}

	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrServerLocked
		}
		return nil, stacktrace.Propagate(err, "failed to acquire lock")
	}

	return f, nil
}
```

Add `"errors"` and `"path/filepath"` to the imports if not already present.

**Step 4: Run the tests to verify they pass**

Run: `go test ./internal/server/ -run TestTryAcquireServerLock -v`
Expected: all 4 tests PASS.

**Step 5: Commit**

```
git add internal/server/process.go internal/server/process_test.go
git commit -m "Add tryAcquireServerLock with flock-based singleton guard"
```

---

Task 3: Integrate flock into `Server.Run()`
--------------------------------------------

**Files:**
- Modify: `internal/server/server.go:96-102` (top of `Run()`)

**Step 1: Add flock acquisition at the top of `Run()`**

In `Server.Run()`, after the comment `// Open the database` and before the
`database.Open` call, add the flock guard. This ensures we bail out before
touching the database or socket if another server is already running:

```go
func (s *Server) Run(ctx context.Context) error {
	// Acquire singleton lock — only one server process may run at a time.
	lockFilepath := config.GetServerLockFilepath(s.agencDirpath)
	lockFile, err := tryAcquireServerLock(lockFilepath)
	if err != nil {
		if errors.Is(err, ErrServerLocked) {
			s.logger.Println("Another server is already running, exiting")
			return nil
		}
		return stacktrace.Propagate(err, "failed to acquire server lock")
	}
	defer lockFile.Close()

	// Open the database
	dbFilepath := config.GetDatabaseFilepath(s.agencDirpath)
	...
```

Add `"errors"` to the imports in `server.go` if not already present.

**Step 2: Verify the build compiles**

Run: `make check`

**Step 3: Manual test**

Start the server, then try to start a second one — the second should log
"Another server is already running" in the server log and exit.

**Step 4: Commit**

```
git add internal/server/server.go
git commit -m "Guard Server.Run with flock to prevent concurrent servers"
```

---

Task 4: Add orphan sweep to `StopServer`
-----------------------------------------

**Files:**
- Modify: `internal/server/process.go:122-159` (`StopServer` function)

**Step 1: Add `findOrphanServerPIDs` helper**

Add this function to `process.go` after `StopServer`:

```go
// findOrphanServerPIDs finds all running `agenc server start` processes that
// have the AGENC_SERVER_PROCESS=1 env var set, excluding the given set of PIDs.
// Returns nil (not an error) if pgrep is unavailable or finds nothing.
func findOrphanServerPIDs(excludePIDs map[int]bool) []int {
	// Find candidate PIDs by command string
	cmd := exec.Command("pgrep", "-f", "agenc server start")
	output, err := cmd.Output()
	if err != nil {
		return nil // pgrep returns exit 1 when no matches
	}

	var candidates []int
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil || pid <= 0 || excludePIDs[pid] {
			continue
		}
		candidates = append(candidates, pid)
	}

	// Verify each candidate has AGENC_SERVER_PROCESS=1 in its environment
	var confirmed []int
	for _, pid := range candidates {
		if isServerChild(pid) {
			confirmed = append(confirmed, pid)
		}
	}

	return confirmed
}

// isServerChild checks whether the process with the given PID has the
// AGENC_SERVER_PROCESS=1 environment variable set.
func isServerChild(pid int) bool {
	cmd := exec.Command("ps", "eww", "-p", strconv.Itoa(pid))
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "AGENC_SERVER_PROCESS=1")
}

// killProcess sends SIGTERM to a process and waits for it to exit, falling
// back to SIGKILL if it doesn't exit within the timeout.
func killProcess(pid int) {
	process, err := os.FindProcess(pid)
	if err != nil {
		return
	}

	_ = process.Signal(syscall.SIGTERM)

	deadline := time.Now().Add(stopPollTimeout)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(pid) {
			return
		}
		time.Sleep(stopPollTick)
	}

	_ = process.Signal(syscall.SIGKILL)
}
```

**Step 2: Update `StopServer` to call the orphan sweep**

Replace the existing `StopServer` function:

```go
// StopServer sends SIGTERM to the server process and waits for it to exit.
// After stopping the PID-file process, it sweeps for any orphaned server
// processes and kills those too. Cleans up the PID file afterward.
func StopServer(pidFilepath string) error {
	killedPIDs := map[int]bool{os.Getpid(): true}

	pid, err := ReadPID(pidFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read server PID")
	}

	if pid > 0 && IsProcessRunning(pid) {
		killProcess(pid)
		killedPIDs[pid] = true
	}

	os.Remove(pidFilepath)

	// Sweep for orphaned server processes
	orphans := findOrphanServerPIDs(killedPIDs)
	for _, orphanPID := range orphans {
		killProcess(orphanPID)
	}

	return nil
}
```

Note: the previous version returned an error when the server was not running.
The new version is tolerant — it proceeds to the orphan sweep regardless,
because orphans may exist even when the PID file is stale or missing.

**Step 3: Verify the build compiles**

Run: `make check`

**Step 4: Manual test**

With the 3 orphaned server processes currently running:
1. Run `agenc server stop`
2. Run `ps aux | grep "agenc server start"` — should show no processes
3. Run `agenc server start` — should start a single server
4. Verify only 1 process: `ps aux | grep "agenc server start"`

**Step 5: Commit**

```
git add internal/server/process.go
git commit -m "Add orphan server sweep to StopServer"
```

---

Task 5: Update `server_stop.go` for new `StopServer` signature
---------------------------------------------------------------

**Files:**
- Modify: `cmd/server_stop.go` (if the error-when-not-running behavior change
  requires caller updates)

**Step 1: Check `server_stop.go`**

Read `cmd/server_stop.go` and verify it handles the case where `StopServer` no
longer returns an error when the server wasn't running. The old code may have
relied on the error to print "server is not running". If so, update the caller
to check `IsRunning` before calling `StopServer`, or adjust the messaging.

**Step 2: Verify build compiles and tests pass**

Run: `make check`

**Step 3: Commit if changes were needed**

```
git add cmd/server_stop.go
git commit -m "Update server stop for tolerant StopServer behavior"
```

---

Task 6: Final verification and push
------------------------------------

**Step 1: Run full test suite**

Run: `make check`

**Step 2: Manual end-to-end test**

1. Kill all existing server processes manually: `pkill -f "agenc server start"`
2. Start one server: `agenc server start`
3. Verify one process: `ps aux | grep "agenc server start" | grep -v grep | wc -l` → 1
4. Try starting another: `agenc server start` → should say "already running"
5. `agenc server restart` → should cleanly restart, still 1 process
6. `agenc server stop` → 0 processes

**Step 3: Push**

```
git pull --rebase
git push
```
