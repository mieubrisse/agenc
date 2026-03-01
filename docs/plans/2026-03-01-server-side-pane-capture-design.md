Server-Side Tmux Pane ID Capture
=================================

Problem
-------

The server creates pool windows (`tmux new-window` in `pool.go:createPoolWindow`) but doesn't capture the pane ID. Instead, the wrapper reads `$TMUX_PANE` after startup and sends it back to the server via `PATCH /missions/{id}`. This creates an unnecessary round-trip and a race window between window creation and title reconciliation.

Design
------

Move pane ID capture into the server at window-creation time. The wrapper no longer registers or clears the pane ID — it becomes fully server-internal state.

### `createPoolWindow` captures pane ID

Add `-P -F '#{pane_id}'` to the `tmux new-window` command. The pane ID comes back on stdout (e.g. `%42`). Strip the `%` prefix to match the existing DB convention. Return signature changes from `(string, error)` to `(string, string, error)` — `(windowTarget, paneID, error)`.

### `spawnWrapper` and `ensureWrapperInPool` store and reconcile

Both callers of `createPoolWindow` receive the pane ID. After creating the pool window, they:

1. Call `s.db.SetTmuxPane(missionID, paneID)` to store it
2. Call `s.reconcileTmuxWindowTitle(missionID)` to set the window title immediately

### Remove `TmuxPane` from the PATCH API

Remove the `TmuxPane` field from `UpdateMissionRequest` and the corresponding block in `handleUpdateMission`. The pane ID is no longer exposed via the API — it is set only by the server's own window-creation code.

### Remove wrapper pane registration

Delete `registerTmuxPane()` and `clearTmuxPane()` from `internal/wrapper/tmux.go`. Remove their calls from `Run()` and `RunHeadless()` in `wrapper.go`.

The rest of `tmux.go` stays intact: `setWindowBusy`, `setWindowNeedsAttention`, `resetWindowTabStyle`, `setWindowTabColors`, and `resolveWindowID` all use `$TMUX_PANE` locally and don't talk to the server.

### No clearing on exit

The `tmux_pane` column is never explicitly cleared. It holds the "last known pane ID" for the mission. Consumers that need to verify liveness check whether the tmux pane actually exists or whether the wrapper PID is running. The value is overwritten the next time a pool window is created for the mission.

### `reloadMissionInTmux` — no changes

`reloadMissionInTmux` uses `respawn-pane`, which reuses the same pane. The pane ID doesn't change across a reload, so the DB value remains correct.

Files Changed
-------------

- `internal/server/pool.go` — `createPoolWindow` adds `-P -F '#{pane_id}'`, parses and returns pane ID
- `internal/server/missions.go` — `spawnWrapper` and `ensureWrapperInPool` store pane ID and reconcile; `UpdateMissionRequest` loses `TmuxPane` field; `handleUpdateMission` loses the pane-setting block
- `internal/wrapper/tmux.go` — Delete `registerTmuxPane` and `clearTmuxPane`
- `internal/wrapper/wrapper.go` — Remove calls to `registerTmuxPane`/`clearTmuxPane` in `Run()` and `RunHeadless()`
