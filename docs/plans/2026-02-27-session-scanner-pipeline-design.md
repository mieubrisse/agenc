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
- `auto_summary` — extracted from `{"type":"summary"}` JSONL entries
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

### Server: tmux window rename

When the session scanner detects a new or changed `custom_title`:

1. Query the `missions` table for the mission's `tmux_pane`
2. If no pane registered, skip (mission not running in tmux)
3. Check sole-pane guard: `tmux display-message -p -t %<pane> "#{window_panes}"` — skip if > 1
4. Check user-override guard: compare current window name against `tmux_window_title` in DB
5. Call `tmux rename-window -t %<pane> <truncated-title>`
6. Update `tmux_window_title` in the missions table

Title priority for tmux window (highest to lowest):
1. `custom_title` from the most recently modified session for this mission
2. `ai_summary` from the missions table (existing summarizer)
3. `auto_summary` from the most recently modified session
4. Existing `renameWindowForTmux()` logic in the wrapper (repo name, mission ID) — retained for initial window naming at startup

### Wrapper simplification

- Remove `updateWindowTitleFromSession()` entirely
- Remove the `session_name` DB write from the Stop handler
- Keep `renameWindowForTmux()` for initial window naming at startup (config windowTitle, repo name, mission ID)
- Keep color management (`setWindowBusy`, `setWindowNeedsAttention`) — these are latency-sensitive and stay in the wrapper
- Remove all JSONL scanning from the wrapper for session name purposes

### Future work (separate beads)

- **Tmux rename on mission switch** — when the user switches between missions in the same tmux window, rename the window to match the new mission's session name. Not addressed in this design.
- **Refactor AI summarizer** — the mission summarizer currently scans JSONL independently. Refactor it to read from the `sessions` table instead, reusing the scanner's data.

### Scope

| Area | Changes |
|------|---------|
| `internal/database/` | New `sessions` table, migration, CRUD functions. Drop `session_name` + `session_name_updated_at` columns. |
| `internal/server/` | New `session_scanner.go` with polling loop. New tmux rename helper. Add goroutine to `server.go`. |
| `internal/session/` | Minor: export helpers for path extraction if needed. |
| `internal/wrapper/tmux.go` | Remove `updateWindowTitleFromSession()`. Remove session name DB writes. Keep `renameWindowForTmux()` and color management. |
| `internal/wrapper/wrapper.go` | Remove `updateWindowTitleFromSession()` call from Stop handler. |
| `internal/server/missions.go` | Remove `session_name` from `UpdateMissionRequest` and related handlers. |
