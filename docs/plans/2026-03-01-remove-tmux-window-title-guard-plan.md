Remove tmux_window_title Guard Implementation Plan
====================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove the `tmux_window_title` column from the `missions` table and the user-manual-rename guard that depends on it, so `reconcileTmuxWindowTitle` always applies the best title without being blocked by tmux window name drift.

**Architecture:** The column cannot be dropped in SQLite (no `ALTER TABLE DROP COLUMN` in older versions), so a migration zeroes it out. All Go code that reads or writes the field is removed. The `applyTmuxTitle` function is simplified to just check the sole-pane guard, then unconditionally apply the title.

**Tech Stack:** Go, SQLite, tmux CLI

---

### Task 1: Remove tmux_window_title from database layer

**Files:**
- Modify: `internal/database/migrations.go`
- Modify: `internal/database/database.go`
- Modify: `internal/database/missions.go`
- Modify: `internal/database/scanners.go`
- Modify: `internal/database/queries.go`
- Modify: `internal/database/database_test.go`

**Context:** The `tmux_window_title` column is on the `missions` table. It's included in the `Mission` struct, all SELECT queries, both scan functions, and has dedicated `Get`/`Set` functions. Three tests exercise the `Get`/`Set` functions.

**Step 1: Add migration to clear the column**

In `internal/database/migrations.go`, add a new constant and migration function:

```go
// After line 27 (addTmuxWindowTitleColumnSQL):
clearTmuxWindowTitleColumnSQL = `UPDATE missions SET tmux_window_title = '' WHERE tmux_window_title != '';`
```

```go
// New function (add after migrateAddTmuxWindowTitle):
// migrateClearTmuxWindowTitle zeroes out the tmux_window_title column.
// The column remains in the schema (SQLite cannot drop columns) but the value
// is no longer read or written by application code.
func migrateClearTmuxWindowTitle(conn *sql.DB) error {
	_, err := conn.Exec(clearTmuxWindowTitleColumnSQL)
	return err
}
```

In `internal/database/database.go`, call the new migration after `migrateAddTmuxWindowTitle`:

```go
if err := migrateClearTmuxWindowTitle(conn); err != nil {
	conn.Close()
	return nil, stacktrace.Propagate(err, "failed to clear tmux_window_title column")
}
```

**Step 2: Remove TmuxWindowTitle from Mission struct and queries**

In `internal/database/missions.go`:
- Remove `TmuxWindowTitle string` from the `Mission` struct (line 30)
- Remove `tmux_window_title` from ALL SELECT column lists in:
  - `GetMission` (line 103)
  - `GetMostRecentMissionForCron` (line 122)
  - `GetMissionByTmuxPane` (line 140)
  - `ListMissionsNeedingSummary` (line 361-363)
- Delete `GetMissionTmuxWindowTitle` function (lines 329-342)
- Delete `SetMissionTmuxWindowTitle` function (lines 344-356)

In `internal/database/queries.go`:
- Remove `tmux_window_title` from the SELECT in `buildListMissionsQuery` (line 10)

In `internal/database/scanners.go`:
- Remove `&m.TmuxWindowTitle` from the `Scan` call in `scanMissions` (line 17)
- Remove `&m.TmuxWindowTitle` from the `Scan` call in `scanMission` (line 55)

**Step 3: Delete tests**

In `internal/database/database_test.go`, delete these three test functions entirely:
- `TestSetAndGetMissionTmuxWindowTitle` (lines 284-314)
- `TestSetMissionTmuxWindowTitleOverwrite` (lines 316-338)
- `TestGetMissionTmuxWindowTitle_UnknownMission` (lines 340-350)

**Step 4: Run tests**

Run: `make check`
Expected: All tests pass. The database package compiles without `TmuxWindowTitle`.

**Step 5: Commit**

```bash
git add internal/database/
git commit -m "Remove TmuxWindowTitle from database layer"
```

---

### Task 2: Remove tmux_window_title from server layer

**Files:**
- Modify: `internal/server/tmux.go`
- Modify: `internal/server/missions.go`

**Context:** The server layer has two areas that reference `TmuxWindowTitle`:
1. `tmux.go` — the reconciliation guards and stored-title tracking
2. `missions.go` — the `MissionResponse` API type, conversion functions, and `UpdateMissionRequest`

**Step 1: Simplify applyTmuxTitle in tmux.go**

Replace the entire `applyTmuxTitle` function with a simplified version that removes:
- The user-manual-rename guard (lines 103-111)
- The "skip if title unchanged" guard (lines 115-118) — this compared against the stored title
- The `SetMissionTmuxWindowTitle` call (lines 124-126)
- The `queryTmuxWindowName` function is no longer called — delete it too

New `applyTmuxTitle`:

```go
// applyTmuxTitle applies a title to the tmux window for a mission.
// Only guard: the pane must be the sole pane in its window.
func (s *Server) applyTmuxTitle(mission *database.Mission, title string) {
	// No tmux pane registered -- mission is not running in tmux
	if mission.TmuxPane == nil || *mission.TmuxPane == "" {
		s.logger.Printf("Tmux reconcile [%s]: skipping — no tmux pane registered", mission.ShortID)
		return
	}

	// Database stores pane IDs without the "%" prefix (e.g. "3043"), but tmux
	// commands require it (e.g. "%3043") to identify panes.
	paneID := "%" + *mission.TmuxPane

	// Guard: only rename if this pane is the sole pane in its window
	if !isSolePaneInTmuxWindow(paneID) {
		s.logger.Printf("Tmux reconcile [%s]: skipping — pane %s is not the sole pane in its window", mission.ShortID, paneID)
		return
	}

	truncatedTitle := truncateTitle(title, maxTmuxWindowTitleLen)

	if err := exec.Command("tmux", "rename-window", "-t", paneID, truncatedTitle).Run(); err != nil {
		s.logger.Printf("Tmux reconcile [%s]: tmux rename-window failed for pane %s: %v", mission.ShortID, paneID, err)
	}
}
```

Delete the `queryTmuxWindowName` function (lines 139-147 in current file).

Update the reconcile logging to remove the `stored=` field:

```go
s.logger.Printf("Tmux reconcile [%s]: bestTitle=%q (custom=%q, agencCustom=%q, auto=%q)",
	mission.ShortID, bestTitle,
	sessionField(activeSession, func(s *database.Session) string { return s.CustomTitle }),
	sessionField(activeSession, func(s *database.Session) string { return s.AgencCustomTitle }),
	sessionField(activeSession, func(s *database.Session) string { return s.AutoSummary }),
)
```

Also update the comment on `reconcileTmuxWindowTitle` — change line 34 from:
```go
// Step 2: Get mission data for tmux_pane, tmux_window_title, git_repo
```
to:
```go
// Step 2: Get mission data for tmux_pane, git_repo
```

**Step 2: Remove TmuxWindowTitle from missions.go API types**

In `internal/server/missions.go`:
- Remove `TmuxWindowTitle string \`json:"tmux_window_title"\`` from `MissionResponse` (line 36)
- Remove `TmuxWindowTitle: mr.TmuxWindowTitle,` from `ToMission()` (line 59)
- Remove `TmuxWindowTitle: m.TmuxWindowTitle,` from `toMissionResponse()` (line 82)
- Remove `TmuxWindowTitle *string \`json:"tmux_window_title,omitempty"\`` from `UpdateMissionRequest` (line 671)
- Remove the `if req.TmuxWindowTitle != nil { ... }` block from `handleUpdateMission` (lines 715-719)

**Step 3: Run tests**

Run: `make check`
Expected: All tests pass.

**Step 4: Commit**

```bash
git add internal/server/tmux.go internal/server/missions.go
git commit -m "Remove user-manual-rename guard and TmuxWindowTitle from server layer"
```

---

### Task 3: Update architecture docs

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Update the route table**

On line 92, change:
```
- `PATCH /missions/{id}` — update mission fields (config_commit, session_name, prompt, tmux_pane, tmux_window_title)
```
to:
```
- `PATCH /missions/{id}` — update mission fields (config_commit, session_name, prompt, tmux_pane)
```

**Step 2: Update the guards section**

Remove the user-override detection bullet (line 535) and the sentence about recording titles (line 537). The guards section should only describe the sole-pane check. Replace lines 532-537 with:

```
**Guards:**

- **Sole-pane check** — only renames the window if the mission's tmux pane is the sole pane in its window. Avoids renaming shared windows (e.g., when the user has split panes).

Titles are truncated to 30 characters (with ellipsis) before applying.
```

**Step 3: Remove tmux_window_title from the schema table**

Delete line 670:
```
| `tmux_window_title` | TEXT | Last window title that AgenC applied via tmux rename-window, used for user-override detection |
```

**Step 4: Commit**

```bash
git add docs/system-architecture.md
git commit -m "Remove tmux_window_title from architecture docs"
```

---

### Task 4: Build and smoke test

**Step 1: Full build**

Run: `make build`
Expected: Compiles, all tests pass, binary produced.

**Step 2: Verify CLI**

Run: `./agenc mission rename --help`
Expected: Help text displays (confirms the binary links correctly after the database layer change).

**Step 3: Commit any generated files**

If `make build` regenerated CLI docs or prime content, stage and commit them:

```bash
git add docs/cli/ internal/claudeconfig/
git commit -m "Regenerate build artifacts after tmux_window_title removal"
```
