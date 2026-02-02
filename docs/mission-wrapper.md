Mission Wrapper
===============

The mission wrapper is a process that supervises a Claude Code shell session. It watches for configuration changes and gracefully restarts Claude when changes are detected and the shell is idle. This allows agent templates and config to be updated without interrupting active work or losing conversation context.

Architecture
------------

Three components cooperate:

1. **Background daemon** (`agenc daemon start`) — a long-running process that fetches template repos from GitHub and syncs Claude config.
2. **Mission wrapper** (one per mission) — a foreground process that spawns Claude as a child and manages its lifecycle.
3. **Claude Code** — the actual shell, running as a child process of the wrapper.

The wrapper runs `claude` (or `claude -c` for resume) via `os/exec`, wiring stdin/stdout/stderr directly to the terminal. It sets `CLAUDE_CONFIG_DIR` to the agenc-managed config directory (`~/.agenc/claude/`) rather than the user's `~/.claude/`, which lets agenc inject hooks and merge settings without modifying the user's config.

Idle Detection
--------------

The wrapper needs to know when Claude is between turns (idle) vs. actively generating (busy). It accomplishes this via Claude Code's hook system.

The daemon's config sync injects two hooks into the agenc-managed `settings.json`:

- **`Stop` hook** — writes `idle` to a `claude-state` file when Claude finishes responding.
- **`UserPromptSubmit` hook** — writes `busy` to `claude-state` when the user submits a prompt.

The `claude-state` file lives at `~/.agenc/missions/<uuid>/claude-state`, one directory above Claude's project root (`agent/`). The hooks reference it via `$CLAUDE_PROJECT_DIR/../claude-state`.

The wrapper watches this file using `fsnotify` on the parent directory (not the file itself, because shell redirects like `echo idle > file` may atomically replace the file, which would break a direct file watch).

Change Detection
----------------

There are two mission types, each with different change detection strategies:

### Template-based missions

The agent config lives in a separate GitHub repo (the "template"). The daemon fetches the template repo from GitHub every 60 seconds. The wrapper polls the local clone's commit hash every 10 seconds and compares it to the stored hash from the last sync. Total worst-case latency from push to detection: ~70 seconds.

When a change is detected, the wrapper rsyncs the template into the mission's `agent/` directory. The rsync excludes:
- `workspace/` — Claude's work area is never overwritten
- `.git/` — git metadata
- `.claude/settings.local.json` — mission-local settings overrides

### Embedded-agent missions

The agent config lives directly in the worktree (e.g., `CLAUDE.md`, `.mcp.json`, `.claude/`). No template repo is involved. The wrapper uses `fsnotify` (kqueue on macOS) to directly watch for filesystem changes to these config files. A 500ms debounce timer batches rapid edits into a single reload signal.

State Machine
-------------

The wrapper is a three-state machine:

```
StateRunning ──[config changed + idle]──> StateRestarting
StateRunning ──[config changed + busy]──> StateRestartPending
StateRestartPending ──[becomes idle]────> StateRestarting
StateRestarting ──[claude exits]────────> StateRunning (relaunched)
```

- **`StateRunning`** — Claude is alive, no restart needed.
- **`StateRestartPending`** — Config has changed, but Claude is busy. Waiting for idle.
- **`StateRestarting`** — Wrapper has sent `SIGINT` to Claude and is waiting for it to exit so it can relaunch.

Restart Procedure
-----------------

1. Config change detected.
2. For template missions: rsync updated template into `agent/`.
3. Check `claude-state`. If busy, transition to `StateRestartPending` and wait.
4. When idle: send `SIGINT` to the Claude child process.
5. Wait for the process to exit.
6. Spawn `claude -c` (continue last conversation) as a new child process.
7. Transition back to `StateRunning`.

Because restarts only happen when Claude is idle, no generation is interrupted. `claude -c` resumes with the full conversation history, so the user experience is seamless.

Signal Handling
---------------

The wrapper catches `SIGINT` and `SIGTERM` to prevent Go's default immediate termination. Since Claude runs in the same process group, terminal `Ctrl-C` reaches it directly. The wrapper simply forwards the signal and waits for Claude to exit before shutting down itself. This ensures the wrapper never outlives its Claude child or terminates prematurely during a user-initiated interrupt.
