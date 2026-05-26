Spawn-from-Mission Session Link Mirroring — Design
====================================================

Context: designed in AgenC mission `beabd207-a9ba-48ca-a3c0-6283f30dfb39`. Run `agenc mission print beabd207-a9ba-48ca-a3c0-6283f30dfb39` for the original discussion.

Problem
-------

When a Claude agent inside a mission invokes `agenc mission new` (without `--headless`) from its bash tool, the spawned child mission silently lands in the pool as a background window instead of appearing in the user's current tmux session — the same session the parent mission is visible in.

Reproduced empirically: from inside a mission's bash shell, `tmux display-message -p '#{session_name}'` fails with `error connecting to /private/tmp/tmux-501/default (Operation not permitted)`. The Claude bash sandbox blocks the tmux Unix socket. `cmd/mission_helpers.go:getCurrentTmuxSessionName()` swallows the error and returns empty; `cmd/mission_new.go:getCallingSessionName()` falls through to the same empty result because `$AGENC_CALLING_SESSION_NAME` is only populated by tmux keybindings and popup dispatch, not by direct CLI invocations from inside a mission.

The empty `TmuxSession` is sent to the server, which treats it as "no session — pool-only." The CLI prints `Running in background (pool window)` — a confident lie. Failure is silent.

Reference mission demonstrating the bug: `f877ff24-73ee-465e-835e-6f4b0393058b` (session_name empty in DB despite having been spawned from inside an attached mission).

Root cause
----------

Two conditions conspire:

1. `$AGENC_CALLING_SESSION_NAME` is unset for direct CLI invocations from inside a mission. It is only populated by tmux keybindings (`internal/tmux/keybindings.go`) and popup dispatch (`cmd/tmux_palette.go`).
2. The documented fallback path (`tmux display-message`) fails inside the mission bash sandbox because the tmux Unix socket is blocked.

Beyond the bug itself, the architectural framing was wrong. `getCallingSessionName()` asks "which tmux session is the caller attached to?" — a question that has a clean answer when the caller is the user's terminal (one tmux client, one session), but no clean answer when the caller is a mission. A mission has no "home" session; the mission lives in the pool and gets *linked into* zero or more user sessions. The right concept for spawn-from-mission is "mirror the parent's link-set onto the child" — link the child into every session where the parent is currently visible.

Architectural model
-------------------

The pool session is every mission's home. Sessions are *views* onto missions. Spawning a child does not pick "a session" — it mirrors the parent's projection set.

`source` becomes the dispatch key for "what UI affordance does this mission get at spawn time":

| `source` | UI affordance | Rationale |
|----------|---------------|-----------|
| `"mission"` | Mirror parent's link-set | Parent is the spawning agent; child should appear where parent appears |
| `"cron"` | Pool-only | No human watching at fire time |
| `"slack"` (future) | Pool-only; output → Slack | Driver is Slack, not tmux |
| `""` (user-terminal) | Link into calling session | The user IS at the terminal |

Provenance and UI affordance ride the same field by design — `source` answers both "who spawned me?" and "where should I show up?" Future source types that don't imply tmux placement (slack, webhook, etc.) fall through to pool-only behavior without disturbing existing source types.

Design
------

### CLI changes — `cmd/mission_new.go`

At the top of `runMissionNew`, before any branching, auto-populate `source` and `source_id` from `$AGENC_MISSION_UUID` when not explicitly set:

```go
if sourceFlag == "" {
    if parentUUID := os.Getenv(config.MissionUUIDEnvVar); parentUUID != "" {
        sourceFlag = "mission"
        sourceIDFlag = parentUUID
    }
}
```

The calling agent invokes `agenc mission new <repo>` exactly as before. It cannot forget to record provenance — the CLI does it. Explicit `--source=X` always wins (e.g., a cron firing inside a mission context with `--source=cron`).

CLI validation (small):
- If `sourceFlag != ""` and `sourceIDFlag == ""`: error `"--source and --source-id must be set together"`.
- If `sourceIDFlag != ""` and `sourceFlag == ""`: same error.
- If `len(sourceIDFlag) > 256`: error `"--source-id exceeds 256 characters"`.

### Server changes — `internal/server/missions.go`

Add a new helper `resolveLinkSessions` that returns the set of tmux session names to link the child into:

```go
func (s *Server) resolveLinkSessions(req CreateMissionRequest) []string {
    if req.Source == "mission" && req.SourceID != "" {
        parent, err := s.db.GetMission(req.SourceID)
        if err != nil || parent == nil {
            s.logger.Printf("Warning: parent mission %s not found, child spawning pool-only", req.SourceID)
            return nil
        }
        if parent.TmuxPane == nil || *parent.TmuxPane == "" {
            s.logger.Printf("Warning: parent mission %s has no tmux pane, child spawning pool-only", database.ShortID(req.SourceID))
            return nil
        }
        poolName := s.getPoolSessionName()
        linkMap := getLinkedPaneSessions(poolName)
        sessions := linkMap[*parent.TmuxPane]
        if len(sessions) == 0 {
            s.logger.Printf("Info: parent mission %s is pool-only, child spawning pool-only", database.ShortID(req.SourceID))
            return nil
        }
        return sessions
    }
    if req.TmuxSession != "" {
        return []string{req.TmuxSession}
    }
    return nil
}
```

Replace the existing single-link block in `spawnWrapper` with the multi-session loop:

```go
if !req.Headless {
    for _, session := range s.resolveLinkSessions(req) {
        if err := linkPoolWindowByPane(paneID, session); err != nil {
            s.logger.Printf("Warning: failed to link child %s into session %s: %v (continuing)", missionRecord.ShortID, session, err)
            continue
        }
        if !req.NoFocus {
            focusPaneInSession(paneID, session)
        }
    }
}
```

Note: `req.Headless` short-circuits all linking. Source/source_id still persist in the DB row — provenance is independent of UI affordance.

### Persistence

The `missions` table already has `source`, `source_id`, and `source_metadata` columns. Crons already use `source="cron"`. Adding `source="mission"` is a new value, not a schema change.

Every mission-spawned child carries a permanent pointer back to its parent. `SELECT id, source_id FROM missions WHERE source='mission'` reconstructs the spawn tree. This unlocks broader provenance work as a follow-up.

### Documentation

`docs/system-architecture.md` "Calling pane and session resolution" section gains a paragraph on the spawn-from-mission case — explains the source dispatch table and that "calling session" does not apply to mission-originated CLI calls.

Edge cases and error handling
-----------------------------

All "parent unfound / pane gone / link failed" paths log a warning and degrade to pool-only. Spawn never fails because of a parent-resolution problem. The design discipline:

| Failure | Severity | Behavior |
|---|---|---|
| Source/SourceID mismatch (one set, other not) | CLI-side | Error before request |
| `--source-id` >256 chars | CLI-side | Error before request |
| Parent UUID malformed or not in DB | Soft | Pool-only; warn |
| Parent exists, no `tmux_pane` | Soft | Pool-only; warn |
| Parent pane in zero non-pool sessions | Soft | Pool-only; info |
| `tmux list-panes` fails | Soft | Pool-only (existing behavior in `getLinkedPaneSessions`) |
| `linkPoolWindowByPane` fails for one session | Soft | Skip that session, continue with others |
| `--headless` set | Hard | Skip linking entirely; provenance still persists |

Race conditions:

- **Parent migrates between sessions** between CLI request and server-side resolution → server queries live tmux at spawn time, gets current state; child lands in current link-set. Acceptable.
- **Parent detached mid-spawn** → `linkPoolWindowByPane` fails for the now-gone session, logged + skipped. Other links proceed.
- **Parent attached to new session mid-spawn (after link-set read)** → child won't be in the new session this time. User can re-attach the child manually. Acceptable.
- **Two concurrent spawn-from-mission requests from same parent** → independent, no shared state. ✓

Backwards compatibility:

- Existing missions with no `source` value fall through to the `TmuxSession` path — existing behavior unchanged.
- No migration needed.

Out of scope:

- Migrating the user-terminal path to server-side resolution (kills the last CLI tmux call). Worthwhile cleanup, separate change.
- Walking the source_id chain to reconstruct spawn trees in a UI. Future provenance work; the DB will already be populated by then.
- Auto-updating `AGENC_CALLING_SESSION_NAME` on `mission attach` events. Not needed; server-side resolution at spawn time is always fresh.
- Renaming the "background" output message to match the `--headless` flag name. Pre-existing vocabulary mismatch, unrelated to this fix.

Test plan
---------

E2E coverage in `scripts/e2e-test.sh`, new section `--- Mission spawn-from-mission session mirroring ---`:

1. **Happy path:** spawn a parent, attach it to a test session, spawn a child with `AGENC_MISSION_UUID=<parent>` set in the env. Assert child's `source='mission'`, `source_id=<parent>`, and child's `tmux_pane` appears in the test session via `tmux list-panes`.
2. **Headless:** repeat with `--headless`. Assert child's `source='mission'`, `source_id=<parent>` (provenance persists), but no tmux linking occurred.
3. **Explicit override:** spawn with `AGENC_MISSION_UUID` set AND `--source=cron --source-id=foo`. Explicit wins. Assert `source='cron'`, no link.
4. **Parent pool-only:** spawn a parent, don't attach it. Spawn child from inside. Assert child is pool-only, source still recorded.
5. **Missing parent:** spawn a child with `AGENC_MISSION_UUID=<nonexistent-uuid>`. Assert child spawns successfully pool-only, source recorded as-is, no error to the user.

Implementation notes
--------------------

- All three creation paths (`handleCreateMission` for regular + adjutant, `handleCreateClonedMission`) share `spawnWrapper(missionRecord, req)`. Single fix point. Adjutant and cloned paths inherit cleanly.
- `spawnWrapper` is called only at mission *creation*, not reload. Source-driven linking fires exactly once at create time. Verify in implementation.
- Pool session name comes from `config.GetPoolSessionName(s.agencDirpath)` — handles E2E namespace (`agenc-<hash>-pool`) automatically.
- `getLinkedPaneSessions(poolName)` (existing helper at `internal/server/pool.go:241`) already filters the pool session and returns the per-pane session list. Direct reuse.
