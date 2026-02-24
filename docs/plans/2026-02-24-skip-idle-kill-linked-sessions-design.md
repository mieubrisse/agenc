Skip Idle Kill for Linked Sessions
====================================

Problem
-------

The idle timeout loop (`runIdleTimeoutCycle`) kills wrapper processes that have been idle for 30+ minutes regardless of whether their tmux pool window is linked into a user session. This means a user can be looking at a Claude session in their tmux window and have it killed out from under them.

Decision
--------

Linked sessions are **fully exempt** from idle timeout. Only unlinked (pool-only) sessions are candidates for idle killing. Detection is done by querying tmux at check time — no database state tracking.

Design
------

### New function: `getLinkedMissionIDs()` in `pool.go`

Runs a single `tmux list-windows -a -F '#{session_name} #{window_name}'` command and parses the output. Returns a `map[string]bool` of window names (short mission IDs) that appear in any session other than `agenc-pool`.

Failure mode: if the tmux command fails (no server, etc.), returns an empty map. This is fail-open — missions proceed to idle kill as before, which is the safe default when link state is unknown.

### Change to `runIdleTimeoutCycle()` in `idle_timeout.go`

Call `getLinkedMissionIDs()` once at the top of the cycle. In the per-mission loop, after confirming the wrapper is running and idle, check if `database.ShortID(m.ID)` is in the linked set. If yes, skip with a debug log. If no, proceed with kill.

### Scope

- `pool.go`: +1 new function (~15 lines)
- `idle_timeout.go`: ~3 lines added to `runIdleTimeoutCycle()`
- No database changes
- No API changes
- No changes to attach/detach handlers
