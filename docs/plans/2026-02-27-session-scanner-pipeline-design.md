Session Scanner Pipeline
========================

Problem
-------

When a user does `/rename` in Claude Code, the tmux window title should update to match. The current approach detects `/rename` only on Stop events in the wrapper — this is lossy. If the user does `/rename` and then `/resume` to switch sessions, the system doesn't know what the new session's name is. Additionally, `sessions-index.json` (Claude's own session index) is unreliable — a known bug (anthropics/claude-code#25032) causes it to stop being updated, so we cannot depend on it.

AgenC needs its own session tracking that persists independent of what the user is doing in Claude, built directly on the JSONL files which are the reliable source of truth.

Decision
--------

Build a server-side session scanner that polls JSONL files every 3 seconds, detects `/rename` custom titles and auto-generated summaries, stores them in a new `sessions` table, and updates tmux window titles directly from the server.

This replaces the wrapper's role in session name detection and tmux title updates (the wrapper retains color management). The `session_name` and `session_name_updated_at` columns on the `missions` table are dropped.

Design
------

### Database: new `sessions` table

```sql
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    mission_id TEXT NOT NULL,
    custom_title TEXT NOT NULL DEFAULT '',
    auto_summary TEXT NOT NULL DEFAULT '',
    last_scanned_offset INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (mission_id) REFERENCES missions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_sessions_mission_id ON sessions(mission_id);
```

- `id` — session UUID, matches the JSONL filename
- `custom_title` — set via `/rename`, extracted from `{"type":"custom-title"}` JSONL entries
- `auto_summary` — auto-generated session description. Initially populated from `{"type":"summary"}` JSONL entries (written by Claude Code). Later, the AgenC AI summarizer will also write to this field on the active session, unifying the two summary sources into one per-session field. This ensures summaries survive session switches — each session carries its own summary.
- `last_scanned_offset` — byte offset for incremental scanning (JSONL files are append-only)

The JSONL filepath is not stored — it can be derived from the mission ID and session ID via the existing `findProjectDirpath()` function. The path follows the pattern `<agencDirpath>/missions/<missionID>/claude-config/projects/<encoded-path>/<sessionID>.jsonl`.

### Database: drop `session_name` from missions

A migration drops `session_name` and `session_name_updated_at` from the missions table. Consumers that need a session name query the `sessions` table instead.

### Server: session scanner loop

A new background goroutine `runSessionScannerLoop`, running every 3 seconds:

1. **Discover JSONL files** — glob for `<agencDirpath>/missions/*/claude-config/projects/*/*.jsonl`
2. **Filter by byte offset** — for each file, `os.Stat()` to get file size. Look up the session in the DB by session ID (derived from filename). If `fileSize <= last_scanned_offset`, skip. If no DB row exists, create one with offset 0.
3. **Incremental scan** — open the file, seek to `last_scanned_offset`, read to EOF. For each line: quick string check for `"custom-title"` or `"type":"summary"` — only parse JSON if the string matches.
4. **Update DB** — write any discovered `custom_title` or `auto_summary` values and the new byte offset.
5. **Trigger tmux rename** — if `custom_title` changed on any session, rename the tmux window for the owning mission.

The mission ID is extracted from the JSONL filepath (the UUID directory under `missions/`).

### Server: tmux window title reconciliation

A single idempotent function `reconcileTmuxWindowTitle(missionID)` examines all available data and converges the tmux window to the correct title. It is called whenever the scanner detects a change, but can also be called from other contexts (e.g., the AI summarizer after generating a new summary, or a future mission-switch handler).

The function:

1. Query the `sessions` table for the **active session** (most recently modified) for this mission — get `custom_title` and `auto_summary`
2. Query the `missions` table for `tmux_pane`, `tmux_window_title`, and `git_repo`
3. Determine the best title using the priority chain (highest to lowest):
   - Active session's `custom_title` (from `/rename`)
   - Active session's `auto_summary` (from Claude or AgenC summarizer)
   - Repo short name (from `git_repo`)
   - Mission short ID (fallback)
4. If no `tmux_pane` registered, skip (mission not running in tmux)
5. Check sole-pane guard: `tmux display-message -p -t %<pane> "#{window_panes}"` — skip if > 1
6. Check user-override guard: compare current window name against `tmux_window_title` in DB — if they differ, the user has manually renamed the window, so skip
7. If the best title differs from what's currently set, call `tmux rename-window -t %<pane> <truncated-title>` and update `tmux_window_title` in the DB

Because the function is idempotent, it can be called as often as needed without side effects. It always converges to the correct state regardless of what triggered it.

### Wrapper simplification

- Remove `updateWindowTitleFromSession()` entirely
- Remove the `session_name` DB write from the Stop handler
- Keep `renameWindowForTmux()` for initial window naming at startup (config windowTitle, repo name, mission ID)
- Keep color management (`setWindowBusy`, `setWindowNeedsAttention`) — these are latency-sensitive and stay in the wrapper
- Remove all JSONL scanning from the wrapper for session name purposes

### Future work (separate beads)

- **Tmux rename on mission switch** — when the user switches between missions in the same tmux window, rename the window to match the new mission's session name. Not addressed in this design.
- **Refactor AI summarizer** — the mission summarizer currently scans JSONL independently and writes `ai_summary` to the missions table. Refactor it to write `auto_summary` to the active session in the `sessions` table instead, unifying the two summary sources. Then drop `ai_summary` from the missions table.

### Scope

| Area | Changes |
|------|---------|
| `internal/database/` | New `sessions` table, migration, CRUD functions. Drop `session_name` + `session_name_updated_at` columns. |
| `internal/server/` | New `session_scanner.go` with polling loop. New tmux rename helper. Add goroutine to `server.go`. |
| `internal/session/` | Minor: export helpers for path extraction if needed. |
| `internal/wrapper/tmux.go` | Remove `updateWindowTitleFromSession()`. Remove session name DB writes. Keep `renameWindowForTmux()` and color management. |
| `internal/wrapper/wrapper.go` | Remove `updateWindowTitleFromSession()` call from Stop handler. |
| `internal/server/missions.go` | Remove `session_name` from `UpdateMissionRequest` and related handlers. |
