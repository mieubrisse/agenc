Stash Push/Pop — Workspace Snapshot & Restore
===============================================

Purpose
-------

Let users atomically snapshot and restore the set of running missions. `agenc stash push`
captures which missions are running and which tmux sessions their windows are linked into,
stops all running missions, and writes the snapshot to disk. `agenc stash pop` reads a
snapshot, re-spawns all the missions, and re-links their windows into the original tmux
sessions (creating sessions that no longer exist).

This enables workflows like rebooting, switching contexts, or freeing resources — then
resuming exactly where you left off.

Current State
-------------

- Missions run as wrapper processes inside the `agenc-pool` tmux session. Their windows are
  linked into user-facing tmux sessions via `link-window`.
- `agenc mission stop` stops a single mission. `agenc mission resume` resumes a single
  stopped mission. There is no way to snapshot and restore the full set of running missions
  and their tmux links in one operation.
- `getLinkedPaneIDs()` in `pool.go` enumerates which panes appear in non-pool sessions, but
  only returns a `map[paneID]bool` — it does not track which specific sessions each pane is
  linked into.

Design
------

### REST API

Three new server-side endpoints handle all orchestration:

| Endpoint         | Method | Purpose                                    |
|------------------|--------|--------------------------------------------|
| `GET /stash`     | GET    | List available stash files with metadata   |
| `POST /stash/push` | POST | Snapshot running missions and stop them     |
| `POST /stash/pop`  | POST | Restore missions from a stash and delete it |

### CLI Commands

| Command             | Endpoint         | Description                            |
|---------------------|------------------|----------------------------------------|
| `agenc stash push`  | `POST /stash/push` | Snapshot and stop all running missions |
| `agenc stash pop`   | `POST /stash/pop`  | Restore a stashed workspace            |
| `agenc stash ls`    | `GET /stash`       | List available stashes                 |

### `POST /stash/push`

Request body:

```json
{
  "force": false
}
```

Server behavior:

1. List all active missions, enrich with `ClaudeState` (reuse existing enrichment).
2. Filter to running-only (wrapper PID alive).
3. If no running missions, return 200 with an empty result.
4. For each running mission, determine which user tmux sessions its pane is linked into
   using a new `getLinkedPaneSessions()` function (see below).
5. If any missions are non-idle (BUSY or WAITING) and `force` is false, return 409 with
   the list of non-idle missions so the CLI can warn and prompt.
6. Write the stash file to `$AGENC_DIRPATH/stash/<timestamp>.json`.
7. Stop each mission's wrapper and destroy pool windows (reuse `stopWrapper` +
   `destroyPoolWindow`).
8. Return 200 with a summary of what was stashed.

Response (success):

```json
{
  "stash_id": "2026-03-02T10-30-00",
  "missions_stashed": 5
}
```

Response (409 — non-idle missions, force not set):

```json
{
  "non_idle_missions": [
    {
      "mission_id": "uuid",
      "short_id": "abc12345",
      "claude_state": "busy",
      "session_name": "Implementing auth flow"
    }
  ]
}
```

### `POST /stash/pop`

Request body:

```json
{
  "stash_id": "2026-03-02T10-30-00"
}
```

If `stash_id` is empty, pops the most recent stash.

Server behavior:

1. Read the specified stash file (or most recent if none specified).
2. For each mission in the stash:
   a. Call `ensureWrapperInPool` to spawn the wrapper (lazy start — reuses existing logic).
   b. For each saved tmux session link:
      - If the tmux session doesn't exist, create it with `tmux new-session -d -s <name>`.
      - Link the pool window into the session via `linkPoolWindow`.
3. Delete the stash file on success.
4. Return 200 with a summary.

Response:

```json
{
  "missions_restored": 5
}
```

### `GET /stash`

Returns a list of available stash files:

```json
[
  {
    "stash_id": "2026-03-02T10-30-00",
    "created_at": "2026-03-02T10:30:00Z",
    "mission_count": 5
  }
]
```

### Stash File Format

Stored at `$AGENC_DIRPATH/stash/<stash_id>.json`:

```json
{
  "created_at": "2026-03-02T10:30:00Z",
  "missions": [
    {
      "mission_id": "full-uuid",
      "linked_sessions": ["agenc", "my-other-session"]
    }
  ]
}
```

### New tmux helper: `getLinkedPaneSessions()`

The existing `getLinkedPaneIDs()` returns `map[string]bool` (pane ID → is linked). Stash
needs to know *which* sessions each pane is linked into.

New function `getLinkedPaneSessions()` returns `map[string][]string` (pane ID → session
names). Uses the same `tmux list-panes -a -F "#{session_name} #{pane_id}"` data but inverts
the grouping.

### Non-idle warning flow

1. CLI calls `POST /stash/push` with `force: false`.
2. Server detects non-idle missions, returns 409 with the list.
3. CLI prints a warning table showing the busy/waiting missions.
4. CLI prompts "Continue? [y/N]".
5. If confirmed, CLI re-calls `POST /stash/push` with `force: true`.
6. `--force` flag on the CLI skips the prompt entirely.

### Pop selection flow

1. CLI calls `GET /stash` to list available stashes.
2. If 0: prints "No stashed workspaces."
3. If 1: auto-selects it.
4. If 2+: shows an fzf picker (using existing `Resolve` pattern).
5. CLI calls `POST /stash/pop` with the chosen `stash_id`.

Files
-----

| File | Change |
|------|--------|
| `cmd/stash.go` | New — top-level `stash` command |
| `cmd/stash_push.go` | New — `agenc stash push` subcommand |
| `cmd/stash_pop.go` | New — `agenc stash pop` subcommand |
| `cmd/stash_ls.go` | New — `agenc stash ls` subcommand |
| `cmd/command_str_consts.go` | Add `stashCmdStr`, `pushCmdStr`, `popCmdStr` |
| `internal/server/handle_stash.go` | New — server handlers for all three endpoints |
| `internal/server/server.go` | Register `GET /stash`, `POST /stash/push`, `POST /stash/pop` routes |
| `internal/server/client.go` | Add `ListStashes()`, `PushStash()`, `PopStash()` client methods |
| `internal/server/pool.go` | Add `getLinkedPaneSessions()` function |
| `internal/config/config.go` | Add `GetStashDirpath()` path helper |
| `docs/system-architecture.md` | Document `stash/` directory and new endpoints |
