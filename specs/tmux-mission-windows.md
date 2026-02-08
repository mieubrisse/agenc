Tmux Mission Windows
====================

Overview
--------

AgenC manages a tmux session where each active mission runs in its own pane (in a new window). A general-purpose `agenc tmux window new` command creates new windows and tracks parent panes for automatic return-on-exit. `mission new` and the wrapper are largely unchanged — the tmux layer wraps them rather than modifying them.

This spec covers the foundational tmux layer — the session lifecycle, window/pane management, and the "side mission" return behavior.


Prerequisites
-------------

**Minimum tmux version: 3.0.** The `new-window -e` flag for environment variable passing requires tmux 3.0. `agenc tmux attach` checks `tmux -V` and exits with a clear error if the version is too old.


User Workflow
-------------

1. User runs `agenc tmux attach` from any terminal
2. AgenC creates the tmux session `agenc` if it doesn't exist, then attaches
3. The session starts by running `agenc mission new` — the user lands in the repo picker immediately
4. From within a running mission, the user (or Claude) runs `agenc tmux window new -- agenc mission new <repo>` to spawn a side mission in a new window
5. When the side mission ends (wrapper exits), the pane is killed and tmux focuses back to the pane that spawned it


Architecture
------------

### The AgenC Tmux Session

AgenC owns a single tmux session named `agenc`. This session is:

- **Created lazily** — only when `agenc tmux attach` runs and no session exists
- **Long-lived** — persists across attach/detach cycles; only destroyed explicitly via `agenc tmux rm` or when all windows close
- **Named deterministically** — always `agenc`, making it easy to target from scripts

The session stores AgenC-specific state via tmux environment variables:

| Variable | Purpose |
|----------|---------|
| `AGENC_TMUX` | Set to `1`. Indicates processes are running inside the AgenC-managed tmux session. |
| `AGENC_DIRPATH` | Propagated from the attaching client's environment. Ensures all tmux windows use the same agenc base directory, even if the user has a custom `$AGENC_DIRPATH`. |

### Pane-Per-Mission Model

Each mission runs in its own tmux **pane**, created inside a new **window**. The pane is the process boundary — when the wrapper process exits, the pane dies.

The wrapper renames the window on startup to `<short_id> <repo-name>`:
```
a1b2c3d4 mieubrisse/agenc
f5e6d7c8 mieubrisse/dotfiles
deadbeef
```

Blank missions show just the `short_id` with no repo suffix.

**Pane environment variables** (set by `agenc tmux window new`):

| Variable | Purpose |
|----------|---------|
| `AGENC_PARENT_PANE` | tmux pane ID (`%N` format) of the pane that spawned this window. Used for return-on-exit. |

Note: `AGENC_MISSION_UUID` is set inside `buildClaudeCmd` and is only available to the Claude child process, not to the tmux pane environment.

### The Initial Window

When the session is first created, the first window runs `agenc mission new` — the user lands directly in the repo picker. There is no idle "lobby" window. Once a repo is selected, `mission new` blocks normally (wrapper runs Claude).

If the user cancels the picker, `agenc mission new` exits, tmux auto-closes the pane/window, and since it's the last window, the session is destroyed.

**Race condition note:** `agenc tmux attach` creates the session detached (`-d`) then attaches in a separate step. If the user cancels the picker before the attach completes, the session may be destroyed, causing the attach to fail. `agenc tmux attach` catches "session not found" errors from the attach step and exits cleanly.


Commands
--------

### `agenc tmux attach`

Attaches to the AgenC tmux session, creating it if it doesn't exist.

```
agenc tmux attach
```

Behavior:
1. Check tmux version (`tmux -V`). Exit with error if < 3.0.
2. Resolve the absolute path to the `agenc` binary via `os.Executable()`. This path is used in all tmux commands to avoid PATH issues inside tmux windows.
3. Check if tmux session `agenc` exists (`tmux has-session -t agenc`)
4. If not, create it:
   a. `tmux new-session -d -s agenc "/abs/path/to/agenc mission new"`
   b. Set session environment: `tmux set-environment -t agenc AGENC_TMUX 1`
   c. Set session environment: `tmux set-environment -t agenc AGENC_DIRPATH <value>` (propagate from client)
5. Attach: `tmux attach-session -t agenc`. If this fails with "session not found" (user cancelled picker before attach), exit cleanly.

If already inside the agenc session (detected via `$AGENC_TMUX`), print a message and exit — no nested attach.

**Binary path resolution:** tmux windows may not inherit the user's full PATH (especially on macOS where the tmux server starts early). All tmux commands that invoke `agenc` must use the absolute path to the binary, resolved once at session creation time.

### `agenc tmux detach`

Convenience alias for `tmux detach-client`. Mostly exists for discoverability.

### `agenc tmux rm`

Destroys the AgenC tmux session, stopping all running missions first.

Behavior:
1. List all windows in the `agenc` session
2. For each window, find the wrapper PID and stop it (same logic as `mission stop`)
3. Kill the tmux session: `tmux kill-session -t agenc`

This prevents orphaned wrapper processes. Without this command, a raw `tmux kill-session -t agenc` sends SIGHUP to all processes in the session — which the wrapper handles (see SIGHUP Handling), but `agenc tmux rm` provides a cleaner shutdown with proper mission state updates.

### `agenc tmux window new`

Creates a new tmux window in the `agenc` session and runs a command inside it.

```
agenc tmux window new -- <command> [args...]
```

This is the core primitive for spawning side missions. It is general-purpose — the command can be anything, not just `agenc mission new`.

Behavior:
1. Must be run inside the `agenc` tmux session (checks `$AGENC_TMUX == 1`)
2. Gets the current pane ID from the `$TMUX_PANE` environment variable
3. Gets the current window ID via `tmux display-message -t $TMUX_PANE -p '#{window_id}'`
4. Creates a new tmux window immediately after the current one:

```bash
tmux new-window -a -t <current-window-id> \
    -e "AGENC_PARENT_PANE=<current-pane-id>" \
    "/abs/path/to/command" "arg1" "arg2" ...
```

5. The new window becomes active (tmux switches to it automatically)
6. The command exits

The `-a` flag inserts the new window adjacent to the current one, so side missions appear next to their parent.

**Example usage from Claude or user:**
```
agenc tmux window new -- agenc mission new mieubrisse/agenc
agenc tmux window new -- agenc mission new --prompt "Fix the auth bug" mieubrisse/agenc
```


How `mission new` Works with Tmux
----------------------------------

`mission new` does **not** need special tmux detection or branching. It runs exactly as it does today — creates the mission record, creates the mission directory, launches the wrapper, and blocks until the wrapper exits.

The tmux integration happens one layer up: the caller runs `agenc tmux window new -- agenc mission new ...`, which handles window creation, parent tracking, and return-on-exit. `mission new` is unaware it's running inside a tmux window.

### Wrapper Changes

The wrapper gets two small additions:

**1. Window renaming on startup.** When the wrapper starts and detects it's inside a tmux pane (checks `$TMUX_PANE`), it renames the window:

```go
shortID := missionRecord.ShortID
repoName := displayGitRepo(missionRecord.GitRepo)
windowTitle := shortID + " " + repoName
exec.Command("tmux", "rename-window", "-t", os.Getenv("TMUX_PANE"), windowTitle).Run()
```

This happens unconditionally when `$TMUX_PANE` is set — no `AGENC_TMUX` check needed.

**2. Return-to-parent on exit.** When the wrapper exits (after `Run()` returns), it checks `$AGENC_PARENT_PANE`:

```go
parentPane := os.Getenv("AGENC_PARENT_PANE")
if parentPane != "" {
    // Get the window containing the parent pane
    windowID, err := exec.Command("tmux", "display-message", "-t", parentPane, "-p", "#{window_id}").Output()
    if err == nil {
        exec.Command("tmux", "select-window", "-t", strings.TrimSpace(string(windowID))).Run()
        exec.Command("tmux", "select-pane", "-t", parentPane).Run()
    }
}
```

If the parent pane no longer exists, the tmux commands fail silently and tmux selects the next window automatically.

**3. Pane cleanup.** When the wrapper process exits, the pane normally closes automatically (tmux kills panes whose command exits). As a defensive fallback, the wrapper can explicitly kill its own pane before exiting:

```go
if paneID := os.Getenv("TMUX_PANE"); paneID != "" {
    exec.Command("tmux", "kill-pane", "-t", paneID).Run()
}
```

This ensures the pane is cleaned up even if tmux's `remain-on-exit` option is set.


Return-to-Parent on Mission Exit
---------------------------------

Return-to-parent only applies to **interactive** missions. Headless missions have no tmux pane and no return behavior.

When an interactive mission's wrapper exits, it:

1. Reads `$AGENC_PARENT_PANE` from its environment
2. If set, resolves the parent pane's window via `tmux display-message -t <pane> -p '#{window_id}'`
3. Focuses that window and pane: `tmux select-window` then `tmux select-pane`
4. Kills its own pane (defensive cleanup)

This logic lives in the wrapper (not the Claude process), because the wrapper outlives individual Claude restarts — Claude may be restarted multiple times during a mission's lifecycle due to config hot-reload.

### SIGHUP Handling

The wrapper currently catches SIGINT and SIGTERM. In a tmux environment, it must also catch **SIGHUP**, which tmux sends when a window or session is destroyed (e.g., user presses `prefix + &`, or the session is killed).

Without SIGHUP handling, the wrapper process terminates via Go's default handler, which means:
- PID file cleanup (`defer os.Remove`) does not run -> stale PID files
- Return-to-parent logic does not run
- Database connection is not closed cleanly

The wrapper should add `syscall.SIGHUP` to its signal handler and treat it identically to SIGINT — forward to Claude, wait for exit, then shut down gracefully. The return-to-parent `tmux select-pane` should still be attempted (the session may still be alive even if this window is being killed).

**Edge cases:**
- Parent pane no longer exists (user closed it): tmux commands fail silently. tmux selects the next window automatically.
- Multiple side missions deep (A -> B -> C): Each tracks its own parent pane. When C ends, returns to B. When B ends, returns to A. Stack-like behavior emerges naturally.
- User manually switches to window C before mission B ends: Return still goes to A (the spawner). This matches the "side mission" mental model.


Window Lifecycle
----------------

### Creation

Windows are created by `agenc tmux window new`. The wrapper renames the window to `<short_id> <repo-name>` on startup.

### Destruction

Panes/windows are destroyed when:
- The wrapper process exits (pane closes, window closes if it was the only pane)
- The wrapper explicitly kills its own pane (defensive cleanup)
- The user manually closes the window (`prefix + &`)
- `agenc mission stop` kills the wrapper (pane closes as a consequence)

### Reconnection

If the wrapper is still running but the user detached and re-attached, the window is still there — tmux preserves it. No special reconnection logic needed.


Database Changes
----------------

No schema changes required. The tmux pane/window mapping is ephemeral — it exists only while the tmux session is alive. Parent tracking is passed via the `AGENC_PARENT_PANE` environment variable, not stored in the database.

### Concurrent Database Access

Multiple wrapper processes will have concurrent database connections (one per running mission, plus the daemon). The existing SQLite WAL mode (`journal_mode=wal`) and `busy_timeout(5000)` configuration handle this correctly. Heartbeat writes are spaced at 30-second intervals, providing natural spacing. No schema or configuration changes needed.


Implementation Plan
-------------------

### Phase 1: Session Management

1. Add `tmux` command group to CLI (`agenc tmux`)
2. Implement `agenc tmux attach` — version check, create session, set env vars, attach
3. Implement `agenc tmux detach` — convenience wrapper
4. Implement `agenc tmux rm` — stop all missions, destroy session

### Phase 2: Window New

5. Implement `agenc tmux window new -- <command>` — create window, set `AGENC_PARENT_PANE`, run command
6. Add SIGHUP to wrapper's signal handler (alongside SIGINT/SIGTERM)
7. Add wrapper window renaming on startup (when `$TMUX_PANE` is set)
8. Add return-to-parent on wrapper exit (when `$AGENC_PARENT_PANE` is set)
9. Add defensive pane cleanup on wrapper exit

### Phase 3: Testing

10. Test: `agenc tmux attach` -> mission new -> Claude -> exit -> pane closes
11. Test: `agenc tmux window new -- agenc mission new <repo>` -> side mission -> exit -> returns to parent
12. Test: nested side missions (A -> B -> C) -> stack-like return behavior
13. Test: `agenc tmux rm` -> all missions stopped, session destroyed


Decisions
---------

1. **Return-to-parent when user manually switches away**: Return to the spawner (A), not the current window (C). Matches the "side mission" mental model. Only applies to interactive missions.

2. **Initial window behavior**: Run `agenc mission new` immediately — the user lands in the repo picker. No idle lobby window.

3. **Multiple tmux sessions**: One session only (`agenc`). No support for multiple named sessions.

4. **Existing missions on attach**: No adoption. Only missions started via tmux get windows. Missions started outside tmux stay outside.

5. **Window ordering**: Side missions are inserted immediately after their parent window using `tmux new-window -a`. Other windows use default creation order.

6. **Window naming**: Wrapper renames the window on startup to `<short_id> <repo-name>`.

7. **No changes to existing commands**: `mission new`, `mission resume`, `mission ls`, `mission stop`, and `mission archive` are unchanged. Tmux integration is handled by `agenc tmux window new` and small additions to the wrapper.

8. **No hotkeys or keybindings** for now (tracked in agenc-95q). Users invoke `agenc tmux window new` directly or via Claude.

9. **No `_run-wrapper`**: Not needed. `mission new` runs normally inside the tmux window — the tmux layer wraps it rather than modifying it.

10. **No `tmux switch`** for now. Window navigation uses standard tmux keybindings (`prefix + n`, `prefix + p`, `prefix + <number>`).
