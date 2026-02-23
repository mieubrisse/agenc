Server/Client Architecture
==========================

Motivation
----------

AgenC's current process model (CLI + daemon + per-mission wrapper) has several pain points:

1. **Command palette commands block the UI.** Commands like `ctrl-s` (mission stop) run in the focused tmux window. When the mission closes, output bleeds into the next window.
2. **Mission stop can hang,** freezing the entire interface while waiting for graceful shutdown.
3. **No attach/detach model.** Users think in terms of "start/stop" rather than "attach/detach." There's no way to leave a mission running in the background and reconnect later.
4. **Database access is distributed.** The CLI, daemon, and every wrapper all write to `database.sqlite`. This makes it unsafe for missions to launch other missions without giving every Claude session write access to the central database.
5. **Background tasks are split** between the daemon (repo sync, config watcher, etc.) and the wrapper (heartbeats, credential sync). A single server process managing both simplifies the runtime.
6. **Future inter-agent messaging** requires a central process that can route messages between missions.


Design Decisions
----------------

### Server model: Thin API Server

The server is a thin HTTP layer over existing packages. It exposes REST endpoints that call the same functions the CLI calls today. The CLI becomes a stateless HTTP client.

- Server = current daemon loops + HTTP listener + wrapper management
- Wrappers remain separate OS processes (the server spawns and manages them)
- Database access is centralized in the server
- The CLI never touches SQLite or spawns wrappers directly

Alternatives considered:
- **Full process supervisor** (wrapper logic as server goroutines) — rejected because a server crash would kill all missions, and the wrapper needs tmux pane association.
- **Message bus architecture** (event-driven internals) — rejected as overkill for current needs. Can be added behind the HTTP API later.

### Protocol: HTTP REST on unix socket

The server listens at `$AGENC_DIRPATH/server/server.sock`. HTTP REST was chosen over raw JSON-over-socket because:

- HTTP works on both unix sockets and TCP with the same code
- Enables future remote access by adding a TCP listener
- Standard tooling (curl, HTTP client libraries) works out of the box
- Docker uses this exact pattern

### Tmux is a hard requirement

All missions run inside tmux. The CLI checks for `$TMUX` on startup and errors if not set. This simplifies the architecture — every mission always has a tmux window, and attach/detach always works via `link-window`/`unlink-window`.

### No server auto-upgrade

The server runs whatever binary version it was started with. The user must explicitly run `agenc server restart` after upgrading the binary.

### Wrapper stays a separate OS process

Each mission gets its own wrapper process. Reasons:

- **Fault isolation** — a wrapper crash affects one mission, not all
- **Tmux integration** — the wrapper runs inside a tmux window for pane coloring and window management
- **Incremental migration** — wrapper code barely changes

Wrappers are spawned as detached processes (survive server crashes). The server re-adopts orphaned wrappers on restart by scanning PID files.

### No heartbeat writes from wrapper

The server tracks wrapper liveness directly:

- For wrappers it spawned: it holds process handles and uses `cmd.Wait()`
- For adopted orphans (after server restart): scans PID files + `kill -0 pid`
- `last_active` updates (user engagement timestamps) flow from wrapper to server via HTTP, best-effort

This eliminates wrapper database access entirely.

### Hook events stay direct to wrapper socket

Claude hooks (`Stop`, `UserPromptSubmit`, `Notification`, `PostToolUse`, `PostToolUseFailure`) send events directly to the wrapper's unix socket at `missions/<uuid>/wrapper.sock`. These are high-frequency, wrapper-internal events (idle tracking, pane coloring) that the server doesn't need to see. The existing socket protocol is unchanged.


Process Model
-------------

### Server

Single long-running process:

- Listens on `$AGENC_DIRPATH/server/server.sock` for HTTP REST requests
- Runs all five current daemon loops as internal goroutines (repo sync, config auto-commit, config watcher, keybindings writer, mission summarizer)
- Owns all database access
- Spawns and manages wrapper processes
- Exposes mission lifecycle API

Runtime directory:

```
$AGENC_DIRPATH/server/
├── server.sock        # Unix socket for HTTP API
├── server.pid         # Server process PID
└── server.log         # Server log
```

Server management is PID-based (no HTTP endpoints for start/stop/status):

- **Start**: CLI forks the server process (same pattern as current daemon fork)
- **Stop**: CLI reads PID file, sends SIGTERM. Server handles graceful shutdown internally.
- **Status**: CLI checks PID file + `kill -0`

### CLI

Thin, stateless HTTP client:

- Parses user input, makes HTTP requests to the server, prints responses
- Never touches the database
- Never spawns wrappers
- Handles terminal UI concerns client-side (table formatting, fzf pickers)
- Passes tmux session context with requests that need it (create, attach)
- Auto-starts the server on first command if not running (like Docker)

### Wrapper

Per-mission foreground process (one per active mission):

- Supervises Claude child process
- Handles hook events via unix socket (`wrapper.sock`)
- Manages credential sync, tmux pane coloring, window naming
- Sends push events to server (instead of updating repo library directly)
- No database access, no repo library access


API Surface
-----------

HTTP REST over unix socket. Docker-style error responses: `{"message": "human-readable error"}` with standard HTTP status codes (400, 404, 409, 500).

### Mission lifecycle

| Method | Path | Purpose |
|--------|------|---------|
| `POST /missions` | Create + start + attach. Body includes repo, prompt, tmux session to link into. |
| `POST /missions/:id/attach` | Ensure wrapper is running (lazy start), link window into caller's session. |
| `POST /missions/:id/detach` | Unlink window from caller's session. Wrapper keeps running. |
| `POST /missions/:id/stop` | Signal wrapper to stop. Returns immediately. |
| `POST /missions/:id/reload` | Rebuild config + restart wrapper. |
| `DELETE /missions/:id` | Remove mission data. Only works on stopped/archived missions. |

### Mission queries

| Method | Path | Purpose |
|--------|------|---------|
| `GET /missions` | List missions. Supports `?tmux_pane=` filter for pane-to-mission resolution. |
| `GET /missions/:id` | Mission detail. |

### Repo events

| Method | Path | Purpose |
|--------|------|---------|
| `POST /repos/:name/push-event` | Wrapper notifies server that a push happened. Server updates repo library. |

### Not exposed as HTTP endpoints

- **Server start/stop/status** — PID-based CLI operations
- **Config get/set** — direct file manipulation; server watches `config.yml` via fsnotify


Tmux Pool Architecture (Phase 4)
---------------------------------

> Note: The tmux pool is a separate phase, built after the server is working. During earlier phases, the server creates tmux windows in the user's session (same UX as today).

A background tmux session (`agenc-pool`) acts as the home for all wrapper processes.

### Flow

1. **Server startup** — creates `agenc-pool` session (or adopts existing one)
2. **Mission start** — server creates a window in `agenc-pool` and runs the wrapper there
3. **Mission attach** — `tmux link-window` from `agenc-pool` into the user's session
4. **Mission detach** — `tmux unlink-window` removes it from the user's session; wrapper keeps running
5. **Mission stop** — server signals wrapper; pool window closes when wrapper exits

### Lazy start + idle timeout

- **Attach** checks if a wrapper is running. If not, spawns one (resume mode) before linking.
- **Idle timeout**: after N minutes with no `UserPromptSubmit` events, the server gracefully stops the wrapper. Next attach re-spawns.

### Window-level linking only

Tmux has no `link-pane` primitive. Panes cannot be shared across windows. Missions appear as full windows via `link-window`/`unlink-window`. This means:

- Side Claude and Side Adjutant palette commands are removed
- `agenc tmux pane new` is removed

### `send-keys` capability

The server can programmatically send input to any mission via `tmux send-keys -t agenc-pool:<window>`. This is the foundation for future inter-agent communication.


Migration Strategy
------------------

Incremental strangler pattern. At every phase boundary, the system works end-to-end.

### Phase 0: Server skeleton

- New `internal/server/` package with HTTP listener on unix socket
- Health check endpoint
- `agenc server start/stop/status` commands (PID-based)
- Daemon still runs separately; everything works as before

### Phase 1: Move mission queries to server

- `GET /missions` and `GET /missions/:id`
- Server opens the database; CLI stops opening it for these commands
- `agenc mission ls` and `agenc mission show` become HTTP clients
- Everything else still goes through the old path

### Phase 2: Move mission lifecycle to server

- `POST /missions`, `POST /missions/:id/stop`, `DELETE /missions/:id`, `POST /missions/:id/reload`
- Server spawns wrappers in the user's tmux session (no pool yet — same UX as today)
- CLI passes tmux session info so the server knows where to create the window
- Add `POST /repos/:name/push-event`

### Phase 3: Absorb the daemon

- Move five background loops into the server process
- Server becomes the single long-running process
- `agenc daemon start/stop/status` become `agenc server start/stop/status`
- Remove daemon code and `$AGENC_DIRPATH/daemon/` directory

### Phase 4: Tmux pool + attach/detach

- Introduce `agenc-pool` tmux session
- Server spawns wrappers in the pool instead of the user's session
- Add `POST /missions/:id/attach` and `POST /missions/:id/detach`
- Add lazy start + idle timeout
- Remove Side Claude, Side Adjutant, `agenc tmux pane new`

### Phase 5: Cleanup

- Remove all direct database access from CLI
- CLI is a pure HTTP client + terminal UI

### SQLite during migration

During Phases 1-2, both the server and CLI/daemon access the database. Keep transactions short to avoid blocking. All writes move to the server by Phase 3.


Risks and Mitigations
---------------------

### Server availability

Every CLI command requires the server. If it's down, nothing works.

**Mitigation:** Auto-start the server on first CLI command (like Docker). CLI checks the socket; if it can't connect, starts the server and retries.

### Wrapper spawning from background process

The server needs to create tmux windows in sessions it's not running inside of.

**Mitigation:** `tmux new-window -t <session>:<index>` works from any process. The CLI passes its tmux session name with create/attach requests.

### Server crash with running missions

Wrappers are detached processes and survive server crashes. On restart, the server scans PID files and re-adopts orphans.

### Error reporting

Server errors come back as HTTP responses. The CLI must faithfully surface them.

**Mitigation:** Docker-style error responses — `{"message": "..."}` with HTTP status codes. The CLI prints the message directly. An internal `errdefs` package maps Go error types to HTTP status codes.
