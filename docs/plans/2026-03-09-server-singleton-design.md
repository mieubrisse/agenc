Server Singleton Enforcement
============================

Problem
-------

Multiple `agenc server start` processes can run simultaneously. This causes:

1. **TOCTOU race in `ensureServerRunning`**: two concurrent CLI calls both see
   "not running" and both fork a new server child.
2. **PID file overwrite**: the second fork overwrites the PID file, orphaning
   the first server process permanently.
3. **Socket takeover without cleanup**: new servers do `os.Remove(socketPath)`
   and bind a new listener, but old processes keep running with their goroutines
   (idle timeout, pane reaping) still operating on the shared database.
4. **Destructive side effects**: orphaned servers' idle timeout loops reaped
   panes for missions that were actually alive, removing their "linked into user
   session" protection and causing the idle timeout to kill them.

Design
------

Two complementary mechanisms:

### 1. Flock in `Server.Run()` — prevents concurrent servers

At the top of `Run()`, open `~/.agenc/server/server.lock` and attempt
`flock(LOCK_EX | LOCK_NB)`:

- **Lock acquired**: this is the rightful server. Hold the fd open for the
  lifetime of `Run()` (defer close). Continue to bind socket and start loops.
- **EWOULDBLOCK**: another server is already running. Log a message and return
  nil — exit cleanly without killing the incumbent.
- **Other error** (filesystem failure): return the error. Do not start a broken
  server.

The lock file is never deleted. The OS releases the lock automatically when the
process exits (including crash, SIGKILL, OOM). The file's existence on disk is
meaningless — only a live process holding `LOCK_EX` on it matters.

### 2. Kill-all in `StopServer()` — cleans up orphans

`StopServer()` currently reads the PID file and kills that one process. Extend
it with a sweep:

1. Kill the PID file process (existing fast-path behavior).
2. Run `pgrep -f "agenc server start"` to find any remaining processes.
3. For each candidate PID, read its environment via `ps eww -p <pid>` and verify
   `AGENC_SERVER_PROCESS=1` is present. This avoids killing unrelated processes.
4. SIGTERM each verified orphan, poll for exit, SIGKILL if needed (reuse
   existing timeout/polling logic).
5. Filter out our own PID (`os.Getpid()`) to avoid self-termination.

This ensures `server stop` (and by extension `server restart`) is thorough. It
handles both the current three-orphan situation and any future leaks.

Files Changed
-------------

- `internal/config/` — add `GetServerLockFilepath` helper
- `internal/server/process.go` — add `tryAcquireServerLock`, enhance
  `StopServer` with orphan sweep
- `internal/server/server.go` — call flock acquire at top of `Run()`, defer
  release

Error Handling
--------------

- Flock open/acquire failure (not EWOULDBLOCK): return error, don't start.
- `pgrep` finds nothing: normal, no error.
- `pgrep` fails entirely: log warning but don't fail `StopServer` — the PID
  file kill already succeeded.
- Lock held but PID file stale: the flock winner writes the correct PID, so
  `IsRunning` works correctly going forward.

Testing
-------

- **Manual**: run `agenc server restart`, verify only 1 process remains via
  `ps aux | grep "agenc server start"`.
- **Unit**: extract `tryAcquireServerLock(lockFilepath) (*os.File, error)` — test
  that two calls result in one success and one EWOULDBLOCK.
- **Orphan sweep**: integration-level; manual verification is sufficient.
