Structured Request and Wrapper Logging
=======================================

Problem
-------

When a mission closes immediately after opening, there is no way to diagnose why.
The server does not log HTTP requests or their outcomes. The wrapper does not log
its own start/exit or Claude's exit code. Errors from failed mission creation are
written to the HTTP response body, which the CLI prints to a tmux window that
closes immediately — leaving no persistent record.

Design
------

### Server: structured HTTP request logging

**Error-returning handler pattern.** Handlers change signature from
`func(w, r)` to `func(w, r) error`. An `appHandler` adapter wraps each handler,
providing:

- Request timing (start → end)
- ResponseWriter wrapping to capture status codes from success responses
- Automatic error response writing when handlers return an error
- Structured JSON logging of every request to a dedicated `requests.log`

**`httpError` type.** Carries an HTTP status code and message. Handlers return
`newHTTPError(status, message)` instead of calling `writeError`. Default status
is 500 when a plain error is returned.

**Log format.** JSON lines via `slog.JSONHandler`. One line per request:

```json
{"time":"...","level":"INFO","msg":"request","method":"POST","path":"/missions","status":201,"duration_ms":42}
{"time":"...","level":"ERROR","msg":"request","method":"POST","path":"/missions","status":500,"duration_ms":156,"error":"failed to create mission: disk full"}
```

**Log file.** `$AGENC_DIRPATH/server/requests.log`, separate from the existing
`server.log` which continues to capture background loop activity and lifecycle
events.

**`writeError` is deleted.** All error response writing moves into the adapter.

### Wrapper: lifecycle logging

**Log wrapper start.** Immediately after the logger is initialized, log
`"Wrapper started"` with mission ID, repo name, and whether it is a resume.

**Capture Claude's exit.** Change the natural-exit path from discarding the
channel value (`case <-w.claudeExited:`) to capturing it
(`case exitErr := <-w.claudeExited:`). Log `"Wrapper exiting"` with reason
(`claude_exited`, `signal`, or `error`) and Claude's exit code extracted from
`*exec.ExitError`.

**Switch to JSON.** Swap `slog.NewTextHandler` for `slog.NewJSONHandler` so the
wrapper log is machine-parseable, consistent with the new request log.

Files Changed
-------------

- `internal/server/errors.go` — delete `writeError`, add `httpError` type
- `internal/server/middleware.go` (new) — `loggingResponseWriter`, `appHandler` adapter
- `internal/server/server.go` — add `requestLogger` field, open `requests.log`, update route registration
- `internal/config/config.go` — add `RequestsLogFilename` constant and path getter
- `internal/server/missions.go` + all handler files — return `error`, replace `writeError` with `return newHTTPError(...)`
- `internal/wrapper/wrapper.go` — add start/exit logs, capture exit error, swap to JSONHandler
- `docs/system-architecture.md` — document `requests.log`

No new dependencies. All stdlib (`slog`, `net/http`).
