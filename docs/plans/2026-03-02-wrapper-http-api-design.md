Wrapper HTTP API & Claude State in Mission Status
==================================================

Summary
-------

Replace the wrapper's custom JSON-over-raw-socket protocol with a standard HTTP API
over the same unix socket. Add a `GET /status` endpoint that exposes Claude's current
state (idle, busy, needs attention). The server queries this endpoint on demand when
building mission status responses, giving users visibility into what Claude is actually
doing.

Current State
-------------

- The wrapper listens on a unix socket using a custom JSON protocol (one JSON object
  per connection, not HTTP). It handles two commands: `restart` and `claude_update`.
- The wrapper tracks `claudeIdle` internally but never exposes it outside the process.
  It uses this state only for graceful restart timing and tmux window coloring.
- Mission status visible to users is: `RUNNING` / `STOPPED` / `ARCHIVED`. `RUNNING`
  is determined by a PID file check. There is no visibility into what Claude is doing.
- Callers: the server sends restart commands via the socket; the
  `mission send claude-update` CLI command sends hook events via the socket.

Design
------

### Wrapper HTTP API

Replace `socket.go` with an `net/http` server on the same unix socket path. Three
endpoints:

| Endpoint              | Method | Purpose                              |
|-----------------------|--------|--------------------------------------|
| `GET /status`         | GET    | Returns Claude state and wrapper state |
| `POST /restart`       | POST   | Triggers graceful or hard restart    |
| `POST /claude-update` | POST   | Hook reports Claude state change     |

**`GET /status` response:**

```json
{
  "claude_state": "idle",
  "wrapper_state": "running",
  "has_conversation": true
}
```

- `claude_state`: `"idle"` | `"busy"` | `"needs_attention"`
- `wrapper_state`: `"running"` | `"restart_pending"` | `"restarting"`
- `has_conversation`: boolean

**`POST /restart` request body:**

```json
{
  "mode": "graceful",
  "reason": "credentials_changed"
}
```

**`POST /claude-update` request body:**

```json
{
  "event": "Stop",
  "notification_type": ""
}
```

Both POST endpoints return `{"status": "ok"}` or `{"status": "error", "error": "..."}`.

### Wrapper Implementation Changes

**`socket.go`** — Replace entirely with an HTTP server using `net/http.ServeMux`
on the existing unix socket.

- `GET /status` reads `claudeIdle`, `state`, `hasConversation`, and a new
  `needsAttention` field. Protected by a `sync.RWMutex` for concurrent access from
  HTTP handlers.
- `POST /restart` and `POST /claude-update` decode the request body, send through
  `commandCh` (same channel pattern as today), wait for the response, and write it
  back as JSON.

**`wrapper.go`** — Minimal changes:

- Add `sync.RWMutex` to protect fields read by `GET /status` concurrently with the
  main event loop writing them.
- Add `needsAttention` bool field. Set on `Notification` events with attention types
  (`permission_prompt`, `idle_prompt`, `elicitation_dialog`). Clear on
  `PostToolUse`, `PostToolUseFailure`, and `UserPromptSubmit`.
- `listenSocket()` call becomes HTTP server startup.
- `handleCommand`, `handleRestartCommand`, `handleClaudeUpdate` stay mostly the same.

**`client.go`** — Replace raw socket client with HTTP client using
`net/http.Transport` with unix socket dialer. Same pattern as
`internal/server/client.go`. Expose typed methods:

- `GetStatus(socketPath) (*StatusResponse, error)`
- `Restart(socketPath, mode, reason) error`
- `SendClaudeUpdate(socketPath, event, notificationType) error`

Timeout variants for hook callers that need short timeouts.

### Server Integration

**`MissionResponse`** gets one new field:

```go
ClaudeState *string `json:"claude_state"`
```

Null when wrapper is not running. `"idle"` / `"busy"` / `"needs_attention"` when it is.

**Population logic:** Only missions with a running wrapper (PID check) get queried.
The server calls `GET /status` on the wrapper's unix socket with a ~500ms timeout.
For list endpoints, queries run concurrently (goroutine per running mission).
If the wrapper doesn't respond, `claude_state` stays null.

No database changes. `claude_state` is purely transient — queried on demand, never
stored.

### CLI Display Changes

`getMissionStatus` in `cmd/mission_ls.go` currently returns `RUNNING` / `STOPPED` /
`ARCHIVED` via local PID check. Update it to use the `claude_state` field from
the API response:

- `status == "archived"` -> `ARCHIVED`
- `status == "active"` and `claude_state == null` -> `STOPPED`
- `status == "active"` and `claude_state != null` -> `RUNNING (idle)` /
  `RUNNING (busy)` / `RUNNING (needs attention)`

### Caller Updates

`cmd/mission_send_claude_update.go` currently calls `wrapper.SendCommandWithTimeout`
with the raw socket protocol. Update to use the new HTTP client's `SendClaudeUpdate`
method. Same timeout, same silent-fail behavior.

### Migration

No backwards compatibility shim needed. The socket is an internal protocol between
components that all ship in the same binary. When rebuilt, both sides update
atomically.

Edge case: a running wrapper on the old protocol when the new binary queries it.
The HTTP client gets a garbled response or connection error, which falls into the
"wrapper unreachable" path (claude_state = null). User restarts the wrapper to pick
up the new protocol. No crash, no data loss.

No database migration — claude_state is never persisted.
