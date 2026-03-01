Remove User-Manual-Rename Guard and tmux_window_title Column
=============================================================

Problem
-------

The `tmux_window_title` column on the `missions` table stores the last title
AgenC applied to a tmux window. A guard in `applyTmuxTitle` compares this stored
value against the current tmux window name — if they differ, AgenC assumes the
user manually renamed the window via tmux and skips the rename to avoid
overriding their choice.

This guard is now counterproductive. The "Rename Session" feature gives users an
explicit way to set a window title (`agenc mission rename` / palette command).
The guard interferes with this: if anything changes the tmux window name between
reconciliation calls (tmux `automatic-rename`, a shell prompt update, etc.), the
guard trips and blocks ALL title updates — including the user's own rename
request.

Design
------

Remove the `tmux_window_title` column and all code that reads or writes it.
AgenC will always apply the best title from the priority chain without checking
whether the window name drifted.

### What changes

**Database:**
- New migration: set `tmux_window_title` to `''` for all rows (column stays in
  schema since SQLite cannot drop columns in older versions, but the value is
  never read)
- Remove `TmuxWindowTitle` from the `Mission` struct
- Remove `GetMissionTmuxWindowTitle()` and `SetMissionTmuxWindowTitle()`
- Remove the field from all scan functions and SELECT queries

**Server — tmux.go:**
- Remove the user-manual-rename guard (`if mission.TmuxWindowTitle != "" { ... }`)
- Remove the "skip if title unchanged" guard (depended on stored title)
- Remove the `SetMissionTmuxWindowTitle` call after rename
- Clean up reconcile logging to remove the `stored=` field

**Server — missions.go:**
- Remove `TmuxWindowTitle` from `MissionResponse`, `UpdateMissionRequest`,
  `toMissionResponse()`, `ToMission()`
- Remove the `TmuxWindowTitle` case from `handleUpdateMission`

**Tests:**
- Remove `TestSetAndGetMissionTmuxWindowTitle`,
  `TestSetMissionTmuxWindowTitleOverwrite`, and
  `TestGetMissionTmuxWindowTitle_UnknownMission`

**Docs:**
- Update `system-architecture.md`: remove user-override detection description
  and the column from the schema table

### What does NOT change

- `TmuxWindowTitleConfig` in `agenc_config.go` (color settings for the tmux tab)
  is unrelated and stays
- The `isSolePaneInTmuxWindow` guard stays — renaming a multi-pane window is
  still undesirable
- The title priority chain is unchanged

Files Changed
-------------

- `internal/database/migrations.go` — new migration to clear the column
- `internal/database/database.go` — call the new migration
- `internal/database/missions.go` — remove struct field, DB functions, update queries
- `internal/database/scanners.go` — remove from scan functions
- `internal/database/database_test.go` — remove related tests
- `internal/server/tmux.go` — remove guards and stored-title logic
- `internal/server/missions.go` — remove from API types and handler
- `docs/system-architecture.md` — update documentation
