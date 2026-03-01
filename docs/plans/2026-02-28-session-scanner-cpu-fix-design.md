Session Scanner CPU Fix
=======================

Problem
-------

The session scanner's `filepath.Glob` runs every 3 seconds with the pattern
`missions/*/claude-config/projects/*/*.jsonl`. Each mission's `projects/`
directory is a symlink to the shared `~/.claude/projects/` directory.

With 753 mission directories and 605 project subdirectories, the glob traverses
753 x 605 = ~456,000 directory entries per cycle. This takes ~30 seconds of CPU
time, but the ticker fires every 3 seconds — so the glob never finishes before
the next cycle starts, pegging the server at 100% CPU permanently.

Root Cause
----------

The scanner was designed to discover JSONL files by globbing across all mission
directories. This worked when there were few missions, but scales as
O(missions x project_subdirs) due to the shared symlink. As missions
accumulate (they are never deleted from the filesystem automatically), the glob
becomes increasingly expensive.

Fix
---

Replace the broad glob with targeted lookups for only the missions that are
currently running in the tmux pool.

### 1. Discover running missions (pool.go)

New helper `listPoolPaneIDs() []string`:

- Runs `tmux list-panes -t agenc-pool -F "#{pane_id}"`
- Strips the `%` prefix from each pane ID (DB stores them without prefix)
- Returns empty slice on error (no tmux server, pool doesn't exist)

This is crash-safe: if a wrapper dies, its pane disappears from tmux, so it
never appears in the list — even if the database still has a stale `tmux_pane`
value. Window names are not used because they can be renamed by tmux or by the
running process.

### 2. Path encoding helper (claudeconfig/build.go)

Extract the encoding logic from `ProjectDirectoryExists` into a reusable
function `ComputeProjectDirpath(agentDirpath string) (string, error)` that
returns the absolute path to `~/.claude/projects/<encoded-dirname>`.
`ProjectDirectoryExists` then delegates to this new function.

### 3. Scanner cycle rewrite (session_scanner.go)

Replace `runSessionScannerCycle`:

1. Call `listPoolPaneIDs()` to get live pane IDs from the tmux pool
2. For each pane ID, call `db.GetMissionByTmuxPane(paneID)` to resolve to a
   mission record
3. For each mission, compute the agent dirpath
   (`<agencDirpath>/missions/<id>/agent`), then call `ComputeProjectDirpath`
   to get the project directory path
4. `os.ReadDir` that single directory, filter for `*.jsonl` suffix
5. For each JSONL file: session ID = filename minus `.jsonl` extension,
   mission ID from step 2
6. Look up or create session row, check byte offset, incremental scan, trigger
   tmux reconciliation on changes — all identical to today

Delete `buildJSONLGlobPattern` and `extractSessionAndMissionID` (now unused).

### 4. What doesn't change

- 3-second ticker interval
- `scanJSONLFromOffset` (JSONL parsing logic)
- `sessions` table schema
- Tmux reconciliation logic
- Idle timeout's `missionIdleDuration`
- The `findProjectDirpath` function in `internal/session/` (used by other
  callers)

Performance
-----------

| Metric | Before | After |
|--------|--------|-------|
| Directory operations per cycle | ~456,000 | ~8 readdir + 8 DB lookups |
| CPU time per cycle | ~30 seconds | negligible |
| External commands per cycle | 0 | 1 (`tmux list-panes`) |
