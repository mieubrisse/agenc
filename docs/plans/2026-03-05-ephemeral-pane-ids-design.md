Ephemeral Pane ID Design
========================

Problem
-------

Tmux pane IDs are not globally unique across tmux server lifetimes. When the
tmux server restarts (e.g., machine reboot), pane IDs reset to 0 and get
reused. The `tmux_pane` column in the missions table stores pane IDs as if they
were durable identifiers, but they are ephemeral. This causes:

- **Stale pane IDs** surviving across tmux restarts, pointing at nothing or at
  the wrong pane
- **Pane ID collisions** where a new pane reuses an old ID, and the server
  associates it with the wrong mission
- **Silent failures** in `ensureWrapperInPool`, idle timeout, and session
  scanner, all of which trust the stored pane ID without validation

Approach
--------

Treat `tmux_pane` as a volatile cache refreshed by three mechanisms:

1. **Startup reconciliation** (synchronous, before HTTP server accepts requests)
2. **Heartbeat registration** (ongoing, every 10 seconds)
3. **Staleness reaper** (periodic cleanup of dead entries)

The wrapper process is the source of truth for its own pane ID. It reads
`$TMUX_PANE` from its environment and sends it with every heartbeat.

Components
----------

### 1. Startup reconciliation (`reconcilePaneIDs`)

Runs synchronously in `Server.Run()` after `ensurePoolSession()` and before
`httpServer.Serve()`. Ensures pane IDs are correct before the first request.

Steps:

1. Clear all pane IDs: `UPDATE missions SET tmux_pane = NULL`
2. Query tmux for all panes in agenc-pool:
   `tmux list-panes -s -t agenc-pool -F "#{pane_id} #{pane_pid}"`
3. For each active mission, read its PID file. If the PID matches a pool pane's
   `pane_pid`, set `tmux_pane` to that pane's ID.

If agenc-pool does not exist (tmux not running), step 2 fails gracefully. All
pane IDs stay NULL. Wrappers will re-register via heartbeat when they start.

### 2. Enhanced heartbeat

The wrapper sends its pane ID with every heartbeat, including the initial one on
startup. The heartbeat request body changes from empty to:

```json
{"pane_id": "42"}
```

The server's `handleHeartbeat` handler updates both `last_heartbeat` and
`tmux_pane`. If `pane_id` is empty (headless wrappers have no `$TMUX_PANE`),
only `last_heartbeat` is updated.

### 3. Staleness reaper

Added to the existing `runIdleTimeoutCycle` (runs every 2 minutes). For each
active mission where `tmux_pane IS NOT NULL`: if `last_heartbeat` is nil or
older than 30 seconds (3 heartbeat cycles), clear `tmux_pane` to NULL.

This auto-heals stale pane IDs from wrapper crashes or tmux restarts that happen
during normal operation (not just at AgenC server startup).

Data Flow
---------

### Startup

```
Server.Run()
  -> ensurePoolSession()
  -> reconcilePaneIDs()          <- synchronous, before HTTP listen
       UPDATE missions SET tmux_pane = NULL
       tmux list-panes -s -t agenc-pool -F "#{pane_id} #{pane_pid}"
       for each active mission: read PID file, match pane_pid -> SetTmuxPane
  -> httpServer.Serve()          <- pane IDs correct before first request
```

### Ongoing heartbeat

```
Wrapper starts
  -> reads $TMUX_PANE (e.g., "%42"), strips "%"
  -> POST /missions/{id}/heartbeat  {"pane_id": "42"}   <- initial
  -> every 10s: same POST                                <- periodic

Server handleHeartbeat()
  -> UpdateHeartbeat(id)          <- existing
  -> SetTmuxPane(id, req.PaneID) <- new (skipped if pane_id empty)
```

### Staleness reaper

```
runIdleTimeoutCycle() (every 2 min)
  -> for each active mission where tmux_pane IS NOT NULL:
       if last_heartbeat is nil OR older than 30s:
         ClearTmuxPane(id)
         log "cleared stale pane for mission <shortID>"
```

Error Handling
--------------

- **agenc-pool doesn't exist at startup:** `tmux list-panes` fails. Log warning,
  skip repopulation. All pane IDs stay NULL. Next heartbeat from wrappers fixes.
- **PID file unreadable or PID not in tmux:** Skip that mission. Pane stays NULL.
  Next heartbeat fixes.
- **Heartbeat with empty pane ID:** Headless wrappers. Only update timestamp, do
  not call SetTmuxPane.
- **Staleness reaper clears a valid pane:** Possible if heartbeat was delayed
  (>30s). Next heartbeat re-registers within 10s. The 3x threshold makes this
  unlikely in practice.
- **Race between startup reconciliation and early heartbeat:** Both write the
  same correct pane ID. SetTmuxPane is idempotent.

Testing
-------

- **Unit test `reconcilePaneIDs`:** Mock DB and tmux command. Verify: clears all
  panes, then sets pane for missions whose PID matches a pool pane.
- **Unit test heartbeat with pane ID:** Send heartbeat with `pane_id` field,
  verify SetTmuxPane is called. Send without, verify it is not.
- **Unit test staleness reaper:** Set last_heartbeat to 60s ago with non-NULL
  tmux_pane. Run cycle. Verify pane is cleared.
- **Integration (manual):** Start missions, restart AgenC server, verify
  `mission ls` shows correct pane IDs immediately. Kill a wrapper, wait 2 min,
  verify pane is cleared.

Future Work
-----------

- **Pool-based liveness:** Replace PID-file liveness checks with agenc-pool
  presence as the canonical signal for "wrapper is alive." If a pane exists in
  agenc-pool, the wrapper is running. If not, it is not. This is a separate
  design pass that builds on the stable pane ID foundation established here.
