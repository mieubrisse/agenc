Tmux Window Title Tracking Design
==================================

Status: Approved
Date: 2026-02-19


Overview
--------

Two bugs prevent tmux window titles from updating dynamically as the user works in their Claude session:

1. `FindSessionName` (JSONL-based fallback) almost never returns a value during active sessions — `sessions-index.json` entries are always empty and JSONL `"type":"summary"` entries are only written at session close. The AI summary from the daemon is the only working dynamic title source, but it requires 10+ prompts and a running daemon, and it has a pre-existing bug (missing DB write).

2. There is no user-override detection. If a user renames a tmux window themselves, `updateWindowTitleFromSession` blindly overwrites it on the next Stop event.

This design fixes both issues.


Root Cause Analysis
-------------------

### Why dynamic title updates don't fire

`updateWindowTitleFromSession()` has three title sources in priority order:

1. Custom title from Claude's `/rename` command — works, but only when user explicitly renames
2. AI summary from daemon (`db.GetMissionAISummary`) — works after 10+ prompts with daemon running
3. `session.FindSessionName` (JSONL/sessions-index) — essentially dead:
   - All observed `sessions-index.json` files have `"entries": []`
   - JSONL `"type":"summary"` entries exist in ~6 of 100+ mission files and are only written at session close

For a typical session with <10 prompts and no `/rename`, none of the three sources produce a value and the function is a no-op.

### Pre-existing bug: AI summary branch missing DB write

When the AI summary path fires (lines 211–215 in `tmux.go`), it renames the window but never calls `UpdateMissionSessionName`. This is inconsistent with the custom-title and session-name branches, which both write to the DB.

### No user-override protection

`updateWindowTitleFromSession` has no mechanism to detect whether the current window title was set by AgenC or the user. It always renames.


Design
------

### DB schema addition

Add one column to the `missions` table:

```sql
tmux_window_title TEXT
```

This stores the **exact post-truncation string** last sent to `tmux rename-window`. It is deliberately separate from `session_name`, which stores the full logical name (pre-truncation). Comparing `tmux_window_title` against the live `#{window_name}` from tmux detects user renames.

Two new DB functions in `internal/database/missions.go`:

- `GetMissionTmuxWindowTitle(missionID string) (string, error)`
- `SetMissionTmuxWindowTitle(missionID string, title string) error`

The `Mission` struct gains a corresponding `TmuxWindowTitle string` field.

### User-override detection logic

At the top of `updateWindowTitleFromSession()`, after the `AGENC_WINDOW_NAME` guard:

```
currentTitle = tmux display-message -p -t <paneID> "#{window_name}"
storedTitle  = db.GetMissionTmuxWindowTitle(missionID)

if storedTitle != "" && currentTitle != storedTitle {
    // User has renamed the window — respect it, bail out
    return
}
```

If `storedTitle` is empty (first run, or column newly added), AgenC proceeds normally. If `storedTitle` matches `currentTitle`, AgenC set the title and it hasn't changed — safe to update. If they differ, the user renamed it — stop.

### Tracking every rename

After every `tmux rename-window` call (in all three branches of `updateWindowTitleFromSession` and in `renameWindowForTmux`), call `SetMissionTmuxWindowTitle` with the title that was just set.

### Fix AI summary branch DB write

In the AI summary branch (`tmux.go:211–215`), add a call to `UpdateMissionSessionName(missionID, aiSummary)` to match the behavior of the other two branches.


Files Changed
-------------

- `internal/database/missions.go` — add `TmuxWindowTitle` field, `GetMissionTmuxWindowTitle`, `SetMissionTmuxWindowTitle`
- `internal/database/migrations.go` (or equivalent migration file) — add column `tmux_window_title TEXT` to `missions`
- `internal/wrapper/tmux.go` — add override detection, wire `SetMissionTmuxWindowTitle` after every rename, fix AI summary `UpdateMissionSessionName` call


What This Does Not Change
-------------------------

- The existing title resolution priority order is unchanged
- `FindSessionName` (JSONL path) is kept as a fallback — it may become useful in future Claude versions that write session summaries more eagerly
- The daemon AI summary mechanism is unchanged
- `AGENC_WINDOW_NAME` (explicit `--name` flag) continues to take absolute priority and bypasses all dynamic updating


Non-Goals
---------

- This design does not fix the underlying reason why `sessions-index.json` is always empty (that is a Claude Code behavior outside AgenC's control)
- This design does not add new title sources beyond what already exists
