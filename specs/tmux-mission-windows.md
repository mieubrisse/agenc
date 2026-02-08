Tmux Mission Windows
====================

Overview
--------

AgenC manages a tmux session where each active mission runs in its own window. Users attach to this session and use hotkeys to create new missions (via popup overlay), navigate between them, and automatically return to the original window when a side mission ends.

This spec covers the foundational tmux layer — the session lifecycle, window-per-mission mapping, and the "side mission" return behavior. It is a prerequisite for the broader tmux hotkey system described in the Hyperspace spec (fork, interrogate, pop/push, etc.).


Prerequisites
-------------

**Minimum tmux version: 3.2.** The `display-popup` command (used for the mission-new overlay) was introduced in tmux 3.2. The `new-window -e` flag for environment variable passing requires tmux 3.0. `agenc tmux attach` checks `tmux -V` and exits with a clear error if the version is too old.


User Workflow
-------------

1. User runs `agenc tmux attach` from any terminal
2. AgenC creates the tmux session `agenc` if it doesn't exist, then attaches
3. The session starts by running `agenc mission new` — the user lands in the repo picker immediately
4. User presses a hotkey (e.g., `prefix + N`) — a popup overlay appears running `agenc mission new`
5. User selects a repo — the popup closes, a new tmux window opens with Claude running in it
6. When the user's side mission ends (Claude exits), the window closes and tmux switches back to the window the user was on before


Architecture
------------

### The AgenC Tmux Session

AgenC owns a single tmux session named `agenc`. This session is:

- **Created lazily** — only when `agenc tmux attach` runs and no session exists
- **Long-lived** — persists across attach/detach cycles; only destroyed when explicitly killed or when all windows close
- **Named deterministically** — always `agenc`, making it easy to target from scripts and hotkeys

The session stores AgenC-specific state via tmux environment variables set on the session:

| Variable | Purpose |
|----------|---------|
| `AGENC_TMUX` | Set to `1`. Indicates this is the AgenC-managed tmux session. Used by commands to detect they're running inside AgenC's session vs. an arbitrary terminal. |
| `AGENC_DIRPATH` | Propagated from the attaching client's environment. Ensures all tmux windows use the same agenc base directory, even if the user has a custom `$AGENC_DIRPATH`. |

### Window-Per-Mission Mapping

Each active mission gets its own tmux window. The mapping is:

```
tmux window name:  "<short-id> <repo-or-label>"
tmux window:       runs the wrapper process (which runs Claude)
```

Window naming examples:
```
a1b2c3d4 mieubrisse/agenc
f5e6d7c8 mieubrisse/dotfiles
deadbeef (blank)
```

The window name uses the mission's `short_id` as a human-readable prefix. The rest is the display-formatted repo name (or `(blank)` for blank missions). Note that `short_id` collisions are possible (it's only the first 8 chars of the UUID). For programmatic window lookup, use the `AGENC_MISSION_UUID` environment variable on each window, not the window name.

**Window environment variables:**

| Variable | Purpose |
|----------|---------|
| `AGENC_MISSION_UUID` | Full mission UUID for this window |
| `AGENC_PARENT_WINDOW` | tmux window ID of the window that spawned this mission (for return-on-exit) |

### The Initial Window

When the session is first created, the first window runs `agenc mission new` — the user lands directly in the repo picker. There is no idle "lobby" window. Once a repo is selected, the initial window becomes the first mission window.

If the user cancels the picker, `agenc mission new` exits, tmux auto-closes the window, and since it's the last window, the session is destroyed.

**Race condition note:** `agenc tmux attach` creates the session detached (`-d`) then attaches in a separate step. If the user cancels the picker before the attach completes, the session may be destroyed, causing the attach to fail. To handle this, `agenc tmux attach` should catch "session not found" errors from the attach step and exit cleanly rather than printing a raw error.


Commands
--------

### `agenc tmux attach`

Attaches to the AgenC tmux session, creating it if it doesn't exist.

```
agenc tmux attach
```

Behavior:
1. Check tmux version (`tmux -V`). Exit with error if < 3.2.
2. Resolve the absolute path to the `agenc` binary via `os.Executable()`. This path is used in all tmux commands to avoid PATH issues inside tmux windows.
3. Check if tmux session `agenc` exists (`tmux has-session -t agenc`)
4. If not, create it:
   a. `tmux new-session -d -s agenc "/abs/path/to/agenc mission new"`
   b. Set session environment: `tmux set-environment -t agenc AGENC_TMUX 1`
   c. Set session environment: `tmux set-environment -t agenc AGENC_DIRPATH <value>` (propagate from client)
   d. Inject hotkey bindings (see Hotkeys section)
5. Attach: `tmux attach-session -t agenc`. If this fails with "session not found" (user cancelled picker before attach), exit cleanly.

If already inside the agenc session (detected via `$AGENC_TMUX`), print a message and exit — no nested attach.

**Binary path resolution:** tmux windows may not inherit the user's full PATH (especially on macOS where the tmux server starts early). All tmux commands that invoke `agenc` must use the absolute path to the binary, resolved once at session creation time.

### `agenc tmux detach`

Convenience alias for `tmux detach-client`. Mostly exists for discoverability.

### `agenc tmux kill`

Destroys the AgenC tmux session, stopping all running missions first.

Behavior:
1. List all windows in the `agenc` session
2. For each window with a valid `AGENC_MISSION_UUID`, stop the mission's wrapper (same as `mission stop`)
3. Kill the tmux session: `tmux kill-session -t agenc`

This prevents orphaned wrapper processes. Without this command, a raw `tmux kill-session -t agenc` sends SIGHUP to all processes in the session — which the wrapper handles (see SIGHUP Handling), but `agenc tmux kill` provides a cleaner shutdown with proper mission state updates.

### `agenc tmux inject-config`

Injects (or re-injects) AgenC's tmux key bindings and configuration into the running `agenc` session. Called automatically during `agenc tmux attach` but also available standalone for reloading after changes.

This keeps AgenC's tmux customizations isolated from the user's `~/.tmux.conf`. AgenC sets bindings programmatically on the session rather than modifying the user's config file.


How `mission new` Integrates with Tmux
---------------------------------------

When `mission new` runs inside the AgenC tmux session, it needs to behave differently than when run outside it:

**Outside tmux (current behavior):** Creates mission, launches wrapper in the current terminal. The wrapper's `Run()` blocks until Claude exits.

**Inside agenc tmux session:** Creates mission, opens a **new tmux window**, launches wrapper in that window. The current terminal (popup or shell) returns immediately.

### Detection

The `mission new` command detects it's inside the AgenC session by checking:

```go
os.Getenv("AGENC_TMUX") == "1"
```

When this is true AND the `--headless` flag is not set, the command switches to tmux-window mode.

**Both code paths must be tmux-aware:** `mission new` has two launch paths — the picker path (via `createAndLaunchMission`) and the `--clone` path (via `runMissionNewWithClone`). Both paths currently call `wrapper.Run()` directly. Both must detect `AGENC_TMUX` and dispatch to tmux-window mode instead. Extract the tmux window creation logic into a shared helper.

### Tmux-Window Mode

Instead of calling `wrapper.Run()` directly (which blocks), the command:

1. Creates the mission record and directory (same as today)
2. Records the current tmux window as the parent: `tmux display-message -p '#{window_id}'`
3. Creates a new tmux window running the wrapper:

```bash
tmux new-window -a -t <parent-window-id> \
    -n "<short-id> <repo-display>" \
    -e "AGENC_MISSION_UUID=<uuid>" \
    -e "AGENC_PARENT_WINDOW=<parent-window-id>" \
    "/abs/path/to/agenc mission _run-wrapper <uuid> [--prompt '...']"
```

The absolute binary path avoids PATH resolution issues inside tmux (see Binary Path Resolution in the `agenc tmux attach` section).

The `-a` flag inserts the new window immediately after the parent window, so side missions appear adjacent to the mission that spawned them.

4. The new window becomes active (tmux switches to it automatically)
5. The command exits (returning control to the popup or shell)

### The `_run-wrapper` Internal Command

A new internal (hidden) command that runs the wrapper directly:

```
agenc mission _run-wrapper <uuid> [--resume] [--prompt "..."]
```

Flags:
- `--resume`: Pass `isResume=true` to `wrapper.Run()`, which uses `claude -c` to continue an existing conversation instead of starting a new one.
- `--prompt`: Initial prompt to pass to Claude. Required for new missions started with `--prompt`.

This command:
1. Opens its own database connection (not shared with the calling process)
2. Fetches the mission record to read `GitRepo` and other metadata
3. Creates the wrapper via `NewWrapper(agencDirpath, missionID, missionRecord.GitRepo, prompt, db)`
4. Calls `wrapper.Run(isResume)` — blocks until mission ends
5. Defers `db.Close()` after `Run()` returns to ensure heartbeat writes work for the full lifetime
6. Executes the return-to-parent logic (see below)

This is hidden from `--help` — it's an implementation detail for tmux integration.

When `mission new` dispatches to tmux-window mode, it passes `--prompt` if a prompt was specified. When `mission resume` dispatches, it passes `--resume`.


Return-to-Parent on Mission Exit
---------------------------------

Return-to-parent only applies to **interactive** missions. Headless missions have no tmux window and no return behavior.

When an interactive mission's wrapper exits, it checks for `AGENC_PARENT_WINDOW` in its environment:

1. If set, switch tmux to that window: `tmux select-window -t <parent-window-id>`
2. The current window closes naturally because the command exited (tmux auto-removes windows whose command exits)

This logic lives in the wrapper (not the Claude process), because the wrapper outlives individual Claude restarts — Claude may be restarted multiple times during a mission's lifecycle due to config hot-reload.

This creates the "side mission" UX: press hotkey → new window opens → do work → Claude exits → you're back where you started.

### SIGHUP Handling

The wrapper currently catches SIGINT and SIGTERM. In a tmux environment, it must also catch **SIGHUP**, which tmux sends when a window or session is destroyed (e.g., user presses `prefix + &`, or the session is killed).

Without SIGHUP handling, the wrapper process terminates via Go's default handler, which means:
- PID file cleanup (`defer os.Remove`) does not run → stale PID files
- Return-to-parent logic does not run
- Database connection is not closed cleanly

The wrapper should add `syscall.SIGHUP` to its signal handler and treat it identically to SIGINT — forward to Claude, wait for exit, then shut down gracefully. The return-to-parent `tmux select-window` should still be attempted (the session may still be alive even if this window is being killed).

**Edge cases:**
- Parent window no longer exists (user closed it): Do nothing. tmux will select the next window automatically.
- Multiple side missions deep (A → B → C): Each tracks its own parent. When C ends, returns to B. When B ends, returns to A. Stack-like behavior emerges naturally.
- User manually switches to window C before mission B ends: Return still goes to A (the spawner). This matches the "side mission" mental model — the spawner is the conceptual parent, regardless of where the user navigated in the meantime.


Hotkeys
-------

**Keybinding strategy is deferred** (tracked in agenc-95q). The commands below are specified; the exact key bindings will be decided after hands-on usage of the core tmux infrastructure.

Key consideration: tmux bindings are server-global (not per-session). Direct `prefix + key` bindings would affect all tmux sessions on the server, not just the `agenc` session. A key table approach (e.g., `prefix + a` enters AgenC mode, then action keys) isolates cleanly but adds a keystroke.

### Planned Hotkey Commands

**Mission New Popup:** Opens a tmux popup overlay running `agenc mission new`. The fzf picker runs inside the popup. The `-E` flag closes the popup when the command exits. Since `mission new` in tmux mode exits immediately after creating the window, the popup closes right away and the new mission window appears.

```bash
# Example binding (actual key TBD per agenc-95q)
tmux bind-key <key> display-popup -E -w 80% -h 60% "/abs/path/to/agenc mission new"
```

**Mission Switch:** Opens a popup running `agenc tmux switch` for quick window navigation.

```bash
tmux bind-key <key> display-popup -E -w 80% -h 60% "/abs/path/to/agenc tmux switch"
```

### Configuration Injection

Bindings are injected programmatically by `agenc tmux inject-config` (called automatically during `agenc tmux attach`). All `agenc` invocations in bindings use the absolute binary path resolved at session creation time.

This keeps all AgenC key bindings in Go code, not in a tmux config file. The user's `~/.tmux.conf` is untouched.


### `agenc tmux switch`

An fzf-based picker for jumping between mission windows.

Behavior:
1. List all windows in the `agenc` tmux session via `tmux list-windows`
2. For each window, read its `AGENC_MISSION_UUID` environment variable
3. Look up the mission in the database to get display info (short_id, repo name, session name, status)
4. Format as fzf rows showing: short_id, repo, session name, Claude state (idle/busy)
5. User selects a mission → execute `tmux select-window -t <window-id>`

Only windows with a valid `AGENC_MISSION_UUID` are shown (excludes any non-mission windows). If no mission windows exist, display a message and exit.


Window Lifecycle
----------------

### Creation

Windows are created by `mission new` (tmux mode) or `mission resume` (which should also create a window if in tmux mode).

### Naming

Window names follow the pattern `<short-id> <display>`:
- `a1b2c3d4 mieubrisse/agenc`
- `deadbeef (blank)`

The short-id prefix enables programmatic lookup: to find the window for mission `a1b2c3d4`, grep tmux window names.

### Destruction

Windows are destroyed when:
- The wrapper process exits (tmux auto-closes the window)
- The user manually closes the window (`prefix + &`)
- `agenc mission stop` kills the wrapper (window closes as a consequence)

### Reconnection

If the wrapper is still running but the user detached and re-attached, the window is still there — tmux preserves it. No special reconnection logic needed.


Existing Mission Integration
-----------------------------

### `mission stop`

Works as today. If the mission has a tmux window, killing the wrapper causes the window to close. The stop command doesn't need to know about tmux.

### `mission resume`

When run inside the agenc tmux session:

- **Mission is stopped:** Create a new tmux window (like `mission new` in tmux mode) and pass `--resume` to `_run-wrapper`.
- **Mission is already running with a tmux window:** Instead of returning an error ("mission is already running"), find and focus the existing window. This is the most natural meaning of "resume" in a tmux context.

### `mission ls`

Could gain a column showing whether a mission has an active tmux window. Optional — not required for the initial implementation.

### `mission archive`

If the mission has a running tmux window, `archive` should stop it first (same as today's behavior of stopping before archiving).


Database Changes
----------------

No schema changes required. The tmux window mapping is ephemeral — it exists only while the tmux session is alive. The mapping can be reconstructed from tmux at any time by reading each window's environment:

```bash
# Find window for a mission by UUID (authoritative lookup)
for wid in $(tmux list-windows -t agenc -F '#{window_id}'); do
    uuid=$(tmux show-environment -t "$wid" AGENC_MISSION_UUID 2>/dev/null | cut -d= -f2)
    if [ "$uuid" = "<target-uuid>" ]; then echo "$wid"; break; fi
done
```

Window names use `short_id` for human readability but should not be used for programmatic lookup (short_id collisions are possible). The `AGENC_MISSION_UUID` env var on each window is the authoritative identifier.

The `AGENC_PARENT_WINDOW` is passed via environment variable, not stored in the database. Parent tracking is a tmux-session-lifetime concern, not a persistent one.

### Concurrent Database Access

Multiple `_run-wrapper` processes will have concurrent database connections (one per running mission, plus the daemon). The existing SQLite WAL mode (`journal_mode=wal`) and `busy_timeout(5000)` configuration handle this correctly. Heartbeat writes are spaced at 30-second intervals, providing natural spacing. No schema or configuration changes needed.


Implementation Plan
-------------------

### Phase 1: Session & Attach

1. Add `tmux` command group to CLI (`agenc tmux`)
2. Implement `agenc tmux attach` — version check, create session, set env vars, attach
3. Implement `agenc tmux detach` — convenience wrapper
4. Implement `agenc tmux kill` — stop all missions, destroy session
5. Implement `agenc tmux inject-config` — bind hotkeys (once agenc-95q is resolved)

### Phase 2: Mission-in-Window

6. Add `agenc mission _run-wrapper <uuid> [--resume] [--prompt]` hidden command
7. Add SIGHUP to wrapper's signal handler (alongside SIGINT/SIGTERM)
8. Modify `mission new` (both picker and `--clone` paths) to detect `AGENC_TMUX` and open new window
9. Implement return-to-parent on wrapper exit
10. Test: popup → pick repo → new window → Claude → exit → return

### Phase 3: Navigation

11. Implement `agenc tmux switch` — fzf picker to jump between mission windows
12. Bind hotkeys (dependent on agenc-95q keybinding decision)

### Phase 4: Polish

13. Modify `mission resume` — tmux-aware: create window for stopped missions, focus window for running missions
14. Add tmux window indicator to `mission ls` (optional)
15. Handle edge cases (parent window gone, attach race condition, etc.)


Decisions
---------

1. **Return-to-parent when user manually switches away**: Return to the spawner (A), not the current window (C). Matches the "side mission" mental model. Only applies to interactive missions — headless missions have no return behavior.

2. **Initial window behavior**: Run `agenc mission new` immediately — the user lands in the repo picker. No idle lobby window.

3. **Multiple tmux sessions**: One session only (`agenc`). No support for multiple named sessions.

4. **Existing missions on attach**: No adoption. Only missions started via tmux get windows. Missions started outside tmux stay outside.

5. **Popup size**: Hardcoded defaults (`-w 80% -h 60%`). Not configurable.

6. **Keybinding strategy**: Deferred (tracked in agenc-95q). Build the commands first, decide on bindings after hands-on usage. Key consideration: tmux bindings are server-global (not per-session), so direct prefix+key bindings would affect all tmux sessions. A key table (`prefix + a` then action key) isolates cleanly but adds a keystroke.

7. **Window ordering**: Side missions are inserted immediately after their parent window using `tmux new-window -a`. Other windows use default creation order.
