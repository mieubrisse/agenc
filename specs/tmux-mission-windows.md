Tmux Mission Windows
====================

Overview
--------

AgenC manages a tmux session where each active mission runs in its own window. Users attach to this session and use hotkeys to create new missions (via popup overlay), navigate between them, and automatically return to the original window when a side mission ends.

This spec covers the foundational tmux layer — the session lifecycle, window-per-mission mapping, and the "side mission" return behavior. It is a prerequisite for the broader tmux hotkey system described in the Hyperspace spec (fork, interrogate, pop/push, etc.).


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

The window name uses the mission's `short_id` as a prefix so it's always unique and greppable. The rest is the display-formatted repo name (or `(blank)` for blank missions).

**Window environment variables:**

| Variable | Purpose |
|----------|---------|
| `AGENC_MISSION_UUID` | Full mission UUID for this window |
| `AGENC_PARENT_WINDOW` | tmux window ID of the window that spawned this mission (for return-on-exit) |

### The Initial Window

When the session is first created, it runs `agenc mission new` — the user lands directly in the repo picker. There is no idle "lobby" window. Once a repo is selected, the initial window becomes the first mission window.

If the user cancels the picker, the window closes and the session is destroyed (no empty windows lingering).


Commands
--------

### `agenc tmux attach`

Attaches to the AgenC tmux session, creating it if it doesn't exist.

```
agenc tmux attach
```

Behavior:
1. Check if tmux session `agenc` exists (`tmux has-session -t agenc`)
2. If not, create it:
   a. `tmux new-session -d -s agenc "agenc mission new"`
   b. Set session environment: `tmux set-environment -t agenc AGENC_TMUX 1`
   c. Inject hotkey bindings (see Hotkeys section)
3. Attach: `tmux attach-session -t agenc`

If already inside the agenc session (detected via `$AGENC_TMUX`), print a message and exit — no nested attach.

### `agenc tmux detach`

Convenience alias for `tmux detach-client`. Mostly exists for discoverability.

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
    "agenc mission _run-wrapper <uuid>"
```

The `-a` flag inserts the new window immediately after the parent window, so side missions appear adjacent to the mission that spawned them.

4. The new window becomes active (tmux switches to it automatically)
5. The command exits (returning control to the popup or shell)

### The `_run-wrapper` Internal Command

A new internal (hidden) command that runs the wrapper directly:

```
agenc mission _run-wrapper <uuid>
```

This command:
1. Opens the database, fetches the mission record
2. Creates and runs the wrapper (same as today's `createAndLaunchMission` tail end)
3. When the wrapper exits (Claude done), executes the return-to-parent logic (see below)

This is hidden from `--help` — it's an implementation detail for tmux integration.


Return-to-Parent on Mission Exit
---------------------------------

Return-to-parent only applies to **interactive** missions. Headless missions have no tmux window and no return behavior.

When an interactive mission's wrapper exits, it checks for `AGENC_PARENT_WINDOW` in its environment:

1. If set, switch tmux to that window: `tmux select-window -t <parent-window-id>`
2. The current window closes naturally because the command exited (tmux auto-removes windows whose command exits)

This logic lives in the wrapper (not the Claude process), because the wrapper outlives individual Claude restarts — Claude may be restarted multiple times during a mission's lifecycle due to config hot-reload.

This creates the "side mission" UX: press hotkey → new window opens → do work → Claude exits → you're back where you started.

**Edge cases:**
- Parent window no longer exists (user closed it): Do nothing. tmux will select the next window automatically.
- Multiple side missions deep (A → B → C): Each tracks its own parent. When C ends, returns to B. When B ends, returns to A. Stack-like behavior emerges naturally.
- User manually switches to window C before mission B ends: Return still goes to A (the spawner). This matches the "side mission" mental model — the spawner is the conceptual parent, regardless of where the user navigated in the meantime.


Hotkeys
-------

### Mission New Popup

**Binding:** `prefix + N`

Opens a tmux popup overlay running `agenc mission new`:

```bash
tmux bind-key N display-popup -E -w 80% -h 60% "agenc mission new"
```

The `-E` flag closes the popup when the command exits. Since `mission new` in tmux mode exits immediately after creating the window, the popup closes right away and the new mission window appears.

The fzf picker (repo selection) runs inside the popup, providing a clean overlay UX.

### Mission List / Switch

**Binding:** `prefix + J` (jump)

Opens a popup with `agenc mission ls` piped through fzf for quick switching:

```bash
tmux bind-key J display-popup -E -w 80% -h 60% "agenc tmux switch"
```

Where `agenc tmux switch` is a new command that shows active missions in fzf and switches to the selected window.

### Configuration Injection

Bindings are injected programmatically when the session is created (or via `agenc tmux inject-config`):

```go
bindings := map[string]string{
    "N": `display-popup -E -w 80% -h 60% "agenc mission new"`,
    "J": `display-popup -E -w 80% -h 60% "agenc tmux switch"`,
}

for key, cmd := range bindings {
    exec.Command("tmux", "bind-key", "-t", "agenc", key, cmd).Run()
}
```

This keeps all AgenC key bindings in Go code, not in a tmux config file. The user's `~/.tmux.conf` is untouched.


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

When run inside the agenc tmux session, `mission resume` should behave like `mission new` in tmux mode — create a new window for the resumed mission rather than blocking in the current terminal.

### `mission ls`

Could gain a column showing whether a mission has an active tmux window. Optional — not required for the initial implementation.

### `mission archive`

If the mission has a running tmux window, `archive` should stop it first (same as today's behavior of stopping before archiving).


Database Changes
----------------

No schema changes required. The tmux window mapping is ephemeral — it exists only while the tmux session is alive. The mapping can be reconstructed from tmux at any time:

```bash
# Find window for a mission
tmux list-windows -t agenc -F '#{window_name} #{window_id}' | grep "^<short-id>"
```

The `AGENC_PARENT_WINDOW` is passed via environment variable, not stored in the database. Parent tracking is a tmux-session-lifetime concern, not a persistent one.


Implementation Plan
-------------------

### Phase 1: Session & Attach

1. Add `tmux` command group to CLI (`agenc tmux`)
2. Implement `agenc tmux attach` — create session + lobby + attach
3. Implement `agenc tmux inject-config` — bind hotkeys
4. Implement `agenc tmux detach` — convenience wrapper

### Phase 2: Mission-in-Window

5. Add `agenc mission _run-wrapper <uuid>` hidden command
6. Modify `mission new` to detect `AGENC_TMUX` and open new window
7. Implement return-to-parent on wrapper exit
8. Test: hotkey → popup → pick repo → new window → Claude → exit → return

### Phase 3: Navigation

9. Implement `agenc tmux switch` — fzf picker to jump between mission windows
10. Bind `prefix + J` for mission switching

### Phase 4: Polish

11. Modify `mission resume` for tmux-aware behavior
12. Add tmux window indicator to `mission ls` (optional)
13. Handle edge cases (parent window gone, multiple side missions, etc.)


Decisions
---------

1. **Return-to-parent when user manually switches away**: Return to the spawner (A), not the current window (C). Matches the "side mission" mental model. Only applies to interactive missions — headless missions have no return behavior.

2. **Initial window behavior**: Run `agenc mission new` immediately — the user lands in the repo picker. No idle lobby window.

3. **Multiple tmux sessions**: One session only (`agenc`). No support for multiple named sessions.

4. **Existing missions on attach**: No adoption. Only missions started via tmux get windows. Missions started outside tmux stay outside.

5. **Popup size**: Hardcoded defaults (`-w 80% -h 60%`). Not configurable.

6. **Keybinding strategy**: Deferred (tracked in agenc-95q). Build the commands first, decide on bindings after hands-on usage. Key consideration: tmux bindings are server-global (not per-session), so direct prefix+key bindings would affect all tmux sessions. A key table (`prefix + a` then action key) isolates cleanly but adds a keystroke.

7. **Window ordering**: Side missions are inserted immediately after their parent window using `tmux new-window -a`. Other windows use default creation order.
