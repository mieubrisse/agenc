Server-Side Mission Launch
===========================

Problem
-------

`agenc mission new` runs the wrapper in-process — the CLI itself becomes the wrapper and spawns Claude as a child process. When called from within a Claude Code session, this creates a nested Claude launch that Claude detects and refuses: "Claude Code cannot be launched inside another Claude Code session."

The server already has pool-based wrapper management (`spawnWrapper`, `ensureWrapperInPool`, `linkPoolWindow`), but the interactive CLI path bypasses it entirely.

Decision
--------

All missions run their wrapper in a tmux pool window, managed by the server. The CLI never runs a wrapper in-process. The only variable is whether the pool window gets linked into a user's tmux session.

State Model
-----------

Missions exist in one of three runtime states:

| State | Description |
|-------|-------------|
| **Stopped** | No wrapper running |
| **Background** | Wrapper running in pool, no window linked |
| **Attached** | Wrapper running in pool, window linked to a tmux session |

State transitions:

| Command | Transition | Requires tmux? |
|---------|------------|----------------|
| `mission new` | (new) → Attached | Yes (falls back to Background if not in tmux) |
| `mission new --headless` | (new) → Background | No |
| `mission attach` | Stopped → Attached, Background → Attached | Yes |
| `mission detach` | Attached → Background | Already exists |
| `mission stop` | Attached → Stopped, Background → Stopped | Already exists |

`mission resume` becomes `mission attach` under the hood — the attach endpoint handles starting a stopped wrapper via `ensureWrapperInPool`.

Design
------

### CLI changes: `cmd/mission_new.go`

`createAndLaunchMission`, `createAndLaunchAdjutantMission`, and `runMissionNewWithClone` all currently end with `wrapper.NewWrapper().Run(false)`. Replace with:

1. Detect tmux session: check `$TMUX` env var, parse session name via `tmux display-message -p '#{session_name}'`
2. Populate `CreateMissionRequest.TmuxSession` (empty if not in tmux or `--headless`)
3. Server creates mission + spawns pool window + links if `TmuxSession` provided
4. CLI prints "Created mission: \<id\>" and exits
5. If `--focus` flag is set and in tmux, run `tmux select-window` to switch to the new window

New flag: `--focus` — when set and in tmux, focus the newly-linked window after creation. Only meaningful for non-headless mode.

The `--headless` flag stays. When set, the CLI omits `TmuxSession` from the request regardless of tmux context.

### CLI changes: `cmd/mission_resume.go`

`resumeMission` currently calls `wrapper.NewWrapper().Run(true)` in-process. Replace with:

1. Must be in tmux (error if not)
2. Call `POST /missions/{id}/attach` with the current tmux session name
3. The attach endpoint handles starting the wrapper if stopped (`ensureWrapperInPool`) and linking the window
4. If `--focus` flag is set, focus the window after attach

### Server changes: `internal/server/missions.go`

`spawnWrapper` currently errors when `TmuxSession` is empty and `Headless` is false:

```go
if tmuxSession == "" {
    return fmt.Errorf("tmux_session is required for interactive missions")
}
```

Change to: when `TmuxSession` is empty, create the pool window but skip the link step. This handles both `--headless` and the "not in tmux" fallback for `mission new`.

No changes needed to `handleAttachMission` — it already requires `TmuxSession`, calls `ensureWrapperInPool`, and links the window.

No changes needed to `handleCreateMission` — it already calls `spawnWrapper` and passes the request through.

### What gets removed

- `wrapper` import from `cmd/mission_new.go` and `cmd/mission_resume.go`
- All in-process `wrapper.NewWrapper().Run()` calls from CLI commands
- The old headless code path in `spawnWrapper` that forks a detached `agenc mission resume --headless` process (replaced by pool window creation)

### What stays unchanged

- Wrapper code (`internal/wrapper/`) — unchanged, still runs inside pool windows via `agenc mission resume`
- Claude spawning (`internal/mission/mission.go`) — unchanged
- `handleAttachMission`, `handleDetachMission`, `handleStopMission` — unchanged
- Database schema — unchanged
- Heartbeat, prompt recording, credential sync — unchanged

### Scope

- `cmd/mission_new.go`: Remove wrapper import, add tmux detection helper, replace `Run()` calls with server-only flow, add `--focus` flag
- `cmd/mission_resume.go`: Remove wrapper import, replace `resumeMission` to call attach endpoint, add `--focus` flag, require tmux
- `internal/server/missions.go`: Change `spawnWrapper` to allow empty `TmuxSession` (pool-only, no link)
- Helper function for tmux session detection (shared between new and resume commands)
