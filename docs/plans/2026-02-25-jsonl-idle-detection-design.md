JSONL-Based Idle Detection
===========================

Problem
-------

The current idle detection uses `last_active` (timestamp of the last user prompt submission) as the primary signal. This misses long-running Claude activity — if Claude works for 30+ minutes on a single prompt (tool use loops, large code generation, etc.), the mission appears idle and gets killed.

Decision
--------

Replace the idle detection signal with the **modification time of the active JSONL conversation log**. Claude Code writes to this file whenever it does anything — streaming output, tool calls, tool results, thinking. If the file hasn't been modified in 30 minutes, Claude is truly idle (sitting at the prompt doing nothing).

Design
------

### New idle detection in `missionIdleDuration()`

Replace the current logic that checks `last_active` / `last_heartbeat` / `created_at` with:

1. Call `session.GetLastSessionID(agencDirpath, missionID)` to get the active session UUID
2. Call `session.FindSessionJSONLPath(sessionID)` to locate the JSONL file
3. `os.Stat()` the file and read its `ModTime()`
4. Idle duration = `now - mtime`

If any step fails (no session UUID, JSONL file not found), fall back to `created_at` as a conservative default.

### What stays unchanged

- `last_active` and `last_heartbeat` database fields remain — they serve other purposes (mission list display, wrapper liveness).
- The `/prompt` and `/heartbeat` server endpoints remain unchanged.
- The linked-pane skip logic in `runIdleTimeoutCycle()` remains unchanged.

### Scope

- `idle_timeout.go`: Rewrite `missionIdleDuration()`, add `internal/session` import
- No database changes
- No wrapper changes
- No API changes
