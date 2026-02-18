Tmux Mission Windows
====================

Overview
--------

AgenC manages a tmux session where each active mission runs in its own pane (in a new window). A general-purpose `agenc tmux window new` command creates new windows adjacent to the current one. `mission new` and the wrapper are largely unchanged — the tmux layer wraps them rather than modifying them.

This spec covers the foundational tmux layer — the session lifecycle and window/pane management.


Prerequisites
-------------

**Minimum tmux version: 3.0.** The `new-window -e` flag for environment variable passing requires tmux 3.0. `agenc tmux attach` checks `tmux -V` and exits with a clear error if the version is too old.


User Workflow
-------------

1. User runs `agenc tmux attach` from any terminal
2. AgenC creates the tmux session `agenc` if it doesn't exist, then attaches
3. The session starts by running `agenc mission new` — the user lands in the repo picker immediately
4. From within a running mission, the user (or Claude) runs `agenc tmux window new -- agenc mission new <repo>` to spawn a side mission in a new window
5. When the side mission ends (wrapper exits), the pane closes and tmux auto-selects an adjacent window


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
| `AGENC_DIRPATH` | Propagated from the attaching client's environment. Ensures all tmux windows use the same agenc base directory, even if the user has a custom `$AGENC_DIRPATH`. |

### Pane-Per-Mission Model

Each mission runs in its own tmux **pane**, created inside a new **window**. The pane is the process boundary — when the wrapper process exits, the pane dies.

When running inside the AgenC tmux session, the wrapper renames the window on startup to `<short_id> <repo-name>` (repo name only, no owner):
```
a1b2c3d4 agenc
f5e6d7c8 dotfiles
deadbeef
```

Blank missions show just the `short_id` with no repo suffix. The wrapper only renames when `$TMUX` is set — outside tmux, it leaves the window name alone so the user can organize however they please.

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
   b. Set session environment: `tmux set-environment -t agenc AGENC_DIRPATH <value>` (propagate from client)
5. Attach: `tmux attach-session -t agenc`. If this fails with "session not found" (user cancelled picker before attach), exit cleanly.

If already inside a tmux session (detected via `$TMUX`), print a message and exit — no nested attach.

**Binary path resolution:** tmux windows may not inherit the user's full PATH (especially on macOS where the tmux server starts early). All tmux commands that invoke `agenc` must use the absolute path to the binary, resolved once at session creation time.

### `agenc tmux detach`

Convenience alias for `tmux detach-client`. Mostly exists for discoverability.

### `agenc tmux rm`

Destroys the AgenC tmux session, stopping all running missions first.

Behavior:
1. List all windows in the `agenc` session
2. For each window, find the wrapper PID and stop it (same logic as `mission stop`)
3. Kill the tmux session: `tmux kill-session -t agenc`

This provides a clean shutdown. A raw `tmux kill-session -t agenc` sends SIGHUP to all processes — the wrapper handles SIGHUP (see SIGHUP Handling), so processes shut down gracefully either way. `agenc tmux rm` is a convenience that ensures everything is stopped in order.

### `agenc tmux window new`

Creates a new tmux window in the `agenc` session and runs a command inside it.

```
agenc tmux window new -- <command> [args...]
```

This is the core primitive for spawning side missions. It is general-purpose — the command can be anything, not just `agenc mission new`.

Behavior:
1. Must be run inside a tmux session (checks `$TMUX` is set)
2. Creates a new tmux window adjacent to the session's active window:

```bash
tmux new-window -a "/abs/path/to/command" "arg1" "arg2" ...
```

3. The new window becomes active (tmux switches to it automatically)
4. When the command exits, the pane closes and tmux auto-selects an adjacent window

The `-a` flag inserts the new window adjacent to the current one, so side missions appear next to their parent. tmux resolves the active window from the current client context, which is correct even from `run-shell` and `display-popup` invocations.

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

The wrapper has one tmux-specific behavior:

**Window renaming on startup.** When the wrapper starts inside any tmux session (checks `$TMUX` is set), it renames the window:

```go
shortID := missionRecord.ShortID
repoName := repoNameOnly(missionRecord.GitRepo) // "agenc", not "mieubrisse/agenc"
windowTitle := shortID + " " + repoName
exec.Command("tmux", "rename-window", "-t", os.Getenv("TMUX_PANE"), windowTitle).Run()
```

This only happens inside the AgenC tmux session. In regular tmux sessions, the wrapper leaves the window name alone.

When the wrapper process exits, the pane closes automatically (tmux kills panes whose command exits) and tmux auto-selects an adjacent window. No explicit cleanup or focus-return needed.


### SIGHUP Handling

The wrapper catches SIGINT, SIGTERM, and **SIGHUP**. tmux sends SIGHUP when a window or session is destroyed (e.g., user presses `prefix + &`, or the session is killed).

Without SIGHUP handling, the wrapper process terminates via Go's default handler, which means deferred cleanup (PID file removal) does not run, leaving stale PID files.

The wrapper treats SIGHUP identically to SIGINT — forward to Claude, wait for exit, then shut down gracefully.


Window Lifecycle
----------------

### Creation

Windows are created by `agenc tmux window new`. The wrapper renames the window to `<short_id> <repo-name>` on startup (only inside the AgenC tmux session).

### Destruction

Panes/windows are destroyed when:
- The wrapper process exits (tmux auto-closes the pane; window closes if it was the only pane)
- The user manually closes the window (`prefix + &`)
- `agenc mission stop` kills the wrapper (pane closes as a consequence)

### Reconnection

If the wrapper is still running but the user detached and re-attached, the window is still there — tmux preserves it. No special reconnection logic needed.


Database Changes
----------------

No schema changes required. The tmux pane/window mapping is ephemeral — it exists only while the tmux session is alive.

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

5. Implement `agenc tmux window new -- <command>` — create window adjacent to current, run command
6. Add SIGHUP to wrapper's signal handler (alongside SIGINT/SIGTERM)
7. Add wrapper window renaming on startup (when `$TMUX` is set)

### Phase 3: Testing

10. Test: `agenc tmux attach` -> mission new -> Claude -> exit -> pane closes
11. Test: `agenc tmux window new -- agenc mission new <repo>` -> side mission -> exit -> tmux selects adjacent window
13. Test: `agenc tmux rm` -> all missions stopped, session destroyed


Decisions
---------

1. **No explicit return-to-parent**: When a mission window closes, tmux auto-selects an adjacent window. This is simpler than tracking parent panes and provides equivalent UX.

2. **Initial window behavior**: Run `agenc mission new` immediately — the user lands in the repo picker. No idle lobby window.

3. **Multiple tmux sessions**: One session only (`agenc`). No support for multiple named sessions.

4. **Existing missions on attach**: No adoption. Only missions started via tmux get windows. Missions started outside tmux stay outside.

5. **Window ordering**: Side missions are inserted immediately after their parent window using `tmux new-window -a`. Other windows use default creation order.

6. **Window naming**: Wrapper renames the window on startup to `<short_id> <repo-name>` (repo name only, no owner). Only when inside the AgenC tmux session — regular tmux sessions are left alone.

7. **No changes to existing commands**: `mission new`, `mission resume`, `mission ls`, `mission stop`, and `mission archive` are unchanged. Tmux integration is handled by `agenc tmux window new` and small additions to the wrapper.

8. **No hotkeys or keybindings** for now (tracked in agenc-95q). Users invoke `agenc tmux window new` directly or via Claude.

9. **No `_run-wrapper`**: Not needed. `mission new` runs normally inside the tmux window — the tmux layer wraps it rather than modifying it.

10. **No `tmux switch`** for now. Window navigation uses standard tmux keybindings (`prefix + n`, `prefix + p`, `prefix + <number>`).
