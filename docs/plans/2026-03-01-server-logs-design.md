Server Logs Endpoint & CLI Command
====================================

Purpose
-------

Expose server logs via an HTTP endpoint so agents can programmatically fetch and debug server behavior. Provide a thin CLI wrapper (`agenc server logs print`) for human use.

Endpoint
--------

`GET /server/logs?source=server|requests&mode=tail|all`

- **source** (default: `server`) — which log file to read
  - `server` → `server.log` (operational log)
  - `requests` → `requests.log` (structured JSON HTTP request log)
- **mode** (default: `tail`) — how much to return
  - `tail` → last 200 lines
  - `all` → entire file
- Returns `Content-Type: text/plain`
- Returns 404 if the log file doesn't exist yet

CLI Command
-----------

`agenc server logs print [--requests] [--all]`

- Default: prints tail of `server.log`
- `--requests` → switches source to `requests.log`
- `--all` → prints entire file instead of tail
- Calls the server endpoint via `Client.GetRaw()`
- Uses `serverClient()` (auto-starts server if needed)

Client Addition
---------------

- `GetRaw(path string) ([]byte, error)` — like `Get()` but returns raw bytes instead of JSON-decoding
- `GetServerLogs(source string, all bool) ([]byte, error)` — convenience method that builds the query string and calls `GetRaw`

Files
-----

| File | Change |
|------|--------|
| `internal/server/server.go` | Register `GET /server/logs` route |
| `internal/server/handle_server_logs.go` | New handler |
| `internal/server/client.go` | Add `GetRaw()` and `GetServerLogs()` |
| `cmd/server_logs.go` | New — `server logs` group command |
| `cmd/server_logs_print.go` | New — `server logs print` subcommand |
| `docs/system-architecture.md` | Add endpoint to route list |
