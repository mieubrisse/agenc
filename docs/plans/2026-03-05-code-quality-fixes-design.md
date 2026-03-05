Code Quality Fixes Design
=========================

Three high-priority code issues identified during audit. All are correctness/reliability
bugs, not feature requests.


Fix 1: Enable SQLite Foreign Key Constraints
---------------------------------------------

**Problem:** SQLite disables foreign keys by default. The sessions table declares
`FOREIGN KEY (mission_id) REFERENCES missions(id) ON DELETE CASCADE`, but the constraint
is never enforced at runtime. Orphaned sessions can accumulate, and cascade deletes
silently fail.

**Solution:** Append `&_pragma=foreign_keys(1)` to the DSN in `database.go:Open()`.
Add a one-time cleanup migration (`migrateCleanOrphanedSessions`) that deletes sessions
whose `mission_id` does not exist in the missions table. This migration runs before any
operations that would violate the newly-enforced constraint.

**Files changed:**
- `internal/database/database.go` — DSN pragma addition
- `internal/database/migrations.go` — orphan cleanup migration

**Testing:**
- New test: `TestCreateSessionRejectsBadMissionID` — verifies FK constraint error
- New test: orphan cleanup migration removes stale sessions
- Existing `TestSessionsCascadeDeleteWithMission` continues to pass (its manual
  `PRAGMA foreign_keys = ON` becomes redundant but harmless)


Fix 2: Panic Recovery in Server Background Goroutines
------------------------------------------------------

**Problem:** The server spawns 8 background goroutines with no panic recovery. A panic
in any of them crashes the entire AgenC server process, orphaning all running missions.

**Solution:** Add a `loopHealth sync.Map` field to `Server`. Create a
`runLoop(name string, wg *sync.WaitGroup, fn func(ctx context.Context))` helper that:

1. Calls `wg.Add(1)` / `defer wg.Done()`
2. Stores `"running"` in `loopHealth`
3. Defers a `recover()` that logs the panic + stack trace and stores `"crashed"`
4. On clean return, stores `"stopped"`

Replace all 8 bare goroutine blocks in `server.go:138-194` with calls to `runLoop`.

Extend `/health` to include loop status:

```json
{
  "status": "ok",
  "version": "1.2.3",
  "loops": {
    "repo-update-worker": "running",
    "session-scanner": "running"
  }
}
```

The top-level `status` becomes `"degraded"` if any loop is `"crashed"`.

Update `server status` CLI to call `/health` and display loop health.

**Files changed:**
- `internal/server/server.go` — `loopHealth` field, `runLoop` helper, goroutine refactor,
  `handleHealth` extension
- `cmd/server_status.go` — call `/health` endpoint, display loop status

**Testing:**
- New test: `runLoop` recovers a panic, loop marked `"crashed"` in health map
- New test: `/health` response includes loop status and degrades on crash


Fix 3: Propagate Time-Parse Errors in Database Scanners
--------------------------------------------------------

**Problem:** Every `time.Parse()` call in `scanners.go` and `sessions.go` discards the
error with `_ =`. Malformed timestamps silently become zero time, corrupting sort order
and time-based filtering.

**Solution:** Change all `t, _ := time.Parse(...)` to `t, err := time.Parse(...)` with
error propagation via `stacktrace.Propagate`. Affects `scanMissions`, `scanMission`,
`scanSession`, and `scanSessions`.

**Files changed:**
- `internal/database/scanners.go` — 6 parse calls
- `internal/database/sessions.go` — 4 parse calls

**Testing:**
- No new tests needed. The change is mechanical — stop discarding errors. All timestamps
  are written by our own code in RFC3339 format, so parse failures indicate real DB
  corruption and should surface loudly.
