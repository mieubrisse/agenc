# Structured Request and Wrapper Logging Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add structured JSON logging for all HTTP requests (with error messages) and wrapper lifecycle events to diagnose mission-closes-immediately bugs.

**Architecture:** HTTP middleware with error-returning handlers replaces `writeError`. A `loggingResponseWriter` captures status codes from success responses. The middleware writes JSON lines to `requests.log`. The wrapper gets start/exit log lines and switches to JSON format.

**Tech Stack:** Go stdlib (`log/slog`, `net/http`), no new dependencies.

---

### Task 1: Add `RequestsLogFilename` constant and path getter

**Files:**
- Modify: `internal/config/config.go:33` (constants block) and after line 167

**Step 1: Add the constant**

In `internal/config/config.go`, add to the constants block (after `ServerLogFilename`):

```go
RequestsLogFilename = "requests.log"
```

**Step 2: Add the path getter**

After `GetServerSocketFilepath`, add:

```go
// GetServerRequestsLogFilepath returns the path to the server HTTP request log file.
func GetServerRequestsLogFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ServerDirname, RequestsLogFilename)
}
```

**Step 3: Run checks**

Run: `make check`
Expected: PASS

**Step 4: Commit**

```
git add internal/config/config.go
git commit -m "Add RequestsLogFilename constant and path getter"
```

---

### Task 2: Create `httpError` type and delete `writeError`

**Files:**
- Modify: `internal/server/errors.go`

**Step 1: Replace errors.go contents**

Replace the entire file with:

```go
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type errorResponse struct {
	Message string `json:"message"`
}

// httpError is an error that carries an HTTP status code. Handlers return these
// to signal both the error message and the appropriate HTTP response status.
type httpError struct {
	status  int
	message string
}

func (e *httpError) Error() string {
	return e.message
}

// newHTTPError creates an error that will be rendered as an HTTP error response
// with the given status code and message.
func newHTTPError(status int, message string) error {
	return &httpError{status: status, message: message}
}

// newHTTPErrorf is like newHTTPError but accepts a format string.
func newHTTPErrorf(status int, format string, args ...any) error {
	return &httpError{status: status, message: fmt.Sprintf(format, args...)}
}

// httpStatusFromError extracts the HTTP status code from an error. If the error
// is not an *httpError, defaults to 500 Internal Server Error.
func httpStatusFromError(err error) int {
	if he, ok := err.(*httpError); ok {
		return he.status
	}
	return http.StatusInternalServerError
}

func writeJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
```

**Step 2: Run checks**

Run: `make check`
Expected: FAIL — all `writeError` call sites (64 in missions.go, 2 in repos.go) now reference an undefined function. This is expected; Task 4 will fix them.

**Step 3: Commit**

```
git add internal/server/errors.go
git commit -m "Replace writeError with httpError type"
```

---

### Task 3: Create logging middleware

**Files:**
- Create: `internal/server/middleware.go`

**Step 1: Create the middleware file**

```go
package server

import (
	"log/slog"
	"net/http"
	"time"
)

// loggingResponseWriter wraps http.ResponseWriter to capture the status code
// written by handlers that return success responses directly via writeJSON.
type loggingResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	if lrw.wroteHeader {
		return
	}
	lrw.status = code
	lrw.wroteHeader = true
	lrw.ResponseWriter.WriteHeader(code)
}

// appHandlerFunc is an HTTP handler that returns an error. Returning a non-nil
// error causes the middleware to write the appropriate HTTP error response and
// log the error message. Handlers should return newHTTPError for known error
// conditions; returning a plain error results in a 500 response.
type appHandlerFunc func(http.ResponseWriter, *http.Request) error

// appHandler wraps an appHandlerFunc into an http.Handler that logs every
// request and automatically writes error responses.
func appHandler(logger *slog.Logger, fn appHandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}

		err := fn(lrw, r)

		duration := time.Since(start)
		status := lrw.status

		if err != nil {
			status = httpStatusFromError(err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(errorResponse{Message: err.Error()})
		}

		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Int64("duration_ms", duration.Milliseconds()),
		}
		if err != nil {
			attrs = append(attrs, slog.String("error", err.Error()))
		}

		level := slog.LevelInfo
		if status >= 400 {
			level = slog.LevelError
		}

		logger.LogAttrs(r.Context(), level, "request", attrs...)
	})
}
```

Note: this file needs `"encoding/json"` in the import block too. Add it.

**Step 2: Run checks**

Run: `make check`
Expected: FAIL (same as before — `writeError` call sites still broken). The new file itself should compile.

**Step 3: Commit**

```
git add internal/server/middleware.go
git commit -m "Add HTTP request logging middleware with appHandler adapter"
```

---

### Task 4: Convert all handlers to return error

This is the largest task. Every handler registered in `registerRoutes` must change signature from `func(w http.ResponseWriter, r *http.Request)` to `func(w http.ResponseWriter, r *http.Request) error`, and every `writeError(w, status, msg); return` must become `return newHTTPError(status, msg)`.

**Files:**
- Modify: `internal/server/missions.go` (16 handlers, 62 writeError call sites)
- Modify: `internal/server/repos.go` (1 handler, 2 writeError call sites)
- Modify: `internal/server/server.go` (1 handler: handleHealth — already returns success only, just needs signature change)

**Conversion pattern for every handler:**

Before:
```go
func (s *Server) handleFoo(w http.ResponseWriter, r *http.Request) {
    // ...
    if err != nil {
        writeError(w, http.StatusBadRequest, "bad: "+err.Error())
        return
    }
    writeJSON(w, http.StatusOK, data)
}
```

After:
```go
func (s *Server) handleFoo(w http.ResponseWriter, r *http.Request) error {
    // ...
    if err != nil {
        return newHTTPError(http.StatusBadRequest, "bad: "+err.Error())
    }
    writeJSON(w, http.StatusOK, data)
    return nil
}
```

**Complete handler list (all in `internal/server/`):**

| Handler | File | writeError calls |
|---------|------|-----------------|
| `handleHealth` | `server.go` | 0 |
| `handleListMissions` | `missions.go` | 2 |
| `handleGetMission` | `missions.go` | 3 |
| `handleCreateMission` | `missions.go` | 5 |
| `handleStopMission` | `missions.go` | 4 |
| `handleDeleteMission` | `missions.go` | 5 |
| `handleReloadMission` | `missions.go` | 6 |
| `handleAttachMission` | `missions.go` | 5 |
| `handleDetachMission` | `missions.go` | 3 |
| `handleArchiveMission` | `missions.go` | 3 |
| `handleUnarchiveMission` | `missions.go` | 2 |
| `handleUpdateMission` | `missions.go` | 9 |
| `handleHeartbeat` | `missions.go` | 2 |
| `handleRecordPrompt` | `missions.go` | 3 |
| `handlePushEvent` | `repos.go` | 2 |

**Special cases:**

1. `handleCreateMission` calls `handleCreateClonedMission` which is a helper, not a registered handler. It currently takes `(w http.ResponseWriter, req CreateMissionRequest, createParams *database.CreateMissionParams)`. Change it to return `error` with the same non-standard parameter list. The caller (`handleCreateMission`) should propagate: `return s.handleCreateClonedMission(w, req, createParams)`.

2. `handleHealth` in `server.go` has no writeError calls — just add `error` return and `return nil`.

3. Handlers that call both `s.logger.Printf(...)` AND `writeError(...)` on the same error (e.g., `handleDeleteMission` line 462): keep the `s.logger.Printf` for the server.log (operational context), convert only the writeError.

4. `handleHeartbeat` and `handleRecordPrompt` use `w.WriteHeader(http.StatusNoContent)` for success (no body). These should continue to write directly — the loggingResponseWriter will capture the status code.

**Step 1: Convert all handlers**

Apply the conversion pattern to every handler listed above. For each `writeError(w, statusCode, msg)` followed by `return`:
- Replace with `return newHTTPError(statusCode, msg)`

For each handler's final success path:
- Add `return nil` at the end

**Step 2: Run checks**

Run: `make check`
Expected: FAIL — `registerRoutes` still uses `mux.HandleFunc` with the old signatures. Task 5 fixes this.

**Step 3: Commit**

```
git add internal/server/missions.go internal/server/repos.go internal/server/server.go
git commit -m "Convert all HTTP handlers to return error"
```

---

### Task 5: Wire up middleware in server startup

**Files:**
- Modify: `internal/server/server.go` (add `requestLogger` field, open log file, update `registerRoutes`)
- Modify: `cmd/server_start.go` (pass requests log filepath to server)

**Step 1: Add requestLogger field to Server struct**

In `server.go`, add to the `Server` struct:

```go
requestLogger *slog.Logger
```

Add `"log/slog"` to the import block.

**Step 2: Update NewServer to accept requests log filepath**

Change `NewServer` signature:

```go
func NewServer(agencDirpath string, socketPath string, logger *log.Logger, requestsLogFilepath string) *Server {
```

Open the requests log file and create the slog.Logger inside `Run()` (not the constructor — consistent with how the existing logger lifecycle works). Add a `requestsLogFilepath` field to the struct to carry it from constructor to `Run()`.

Actually, simpler: open the requests log file in `Run()` using the agencDirpath (which is already on the struct). That way the constructor doesn't change.

In `Run()`, after the database open block and before route registration, add:

```go
// Open structured request log
requestsLogFilepath := config.GetServerRequestsLogFilepath(s.agencDirpath)
requestsLogFile, err := os.OpenFile(requestsLogFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
if err != nil {
    return stacktrace.Propagate(err, "failed to open requests log file")
}
defer requestsLogFile.Close()
s.requestLogger = slog.New(slog.NewJSONHandler(requestsLogFile, nil))
```

**Step 3: Update registerRoutes to use appHandler**

Change every `mux.HandleFunc` to `mux.Handle` with `appHandler`:

Before:
```go
mux.HandleFunc("POST /missions", s.handleCreateMission)
```

After:
```go
mux.Handle("POST /missions", appHandler(s.requestLogger, s.handleCreateMission))
```

Apply to all 16 routes in `registerRoutes`.

**Step 4: Run checks**

Run: `make check`
Expected: PASS — everything should compile and tests should pass.

**Step 5: Commit**

```
git add internal/server/server.go
git commit -m "Wire up structured request logging middleware"
```

---

### Task 6: Wrapper lifecycle logging

**Files:**
- Modify: `internal/wrapper/wrapper.go`

**Step 1: Switch to JSONHandler**

At line 175, change:

```go
w.logger = slog.New(slog.NewTextHandler(logFile, nil))
```

To:

```go
w.logger = slog.New(slog.NewJSONHandler(logFile, nil))
```

**Step 2: Add "Wrapper started" log**

Immediately after the logger is created (after the line above), add:

```go
w.logger.Info("Wrapper started",
    "mission_id", database.ShortID(w.missionID),
    "repo", w.gitRepoName,
    "is_resume", isResume,
)
```

Add `"github.com/odyssey/agenc/internal/database"` to the import block if not already present.

**Step 3: Log Claude exit on natural exit**

At line 288, change:

```go
case <-w.claudeExited:
```

To:

```go
case exitErr := <-w.claudeExited:
```

Then in the `else` block (natural exit, around line 314-316), replace `return nil` with:

```go
exitCode := 0
if exitErr != nil {
    if ee, ok := exitErr.(*exec.ExitError); ok {
        exitCode = ee.ExitCode()
    } else {
        exitCode = -1
    }
}
w.logger.Info("Wrapper exiting",
    "reason", "claude_exited",
    "exit_code", exitCode,
    "exit_error", fmt.Sprintf("%v", exitErr),
)
return nil
```

Add `"fmt"` to the import block if not already present.

**Step 4: Log exit on signal path**

At lines 273-282 (the signal handler case), change:

```go
case sig := <-sigCh:
    if w.claudeCmd != nil && w.claudeCmd.Process != nil {
        _ = w.claudeCmd.Process.Signal(sig)
    }
    <-w.claudeExited
    return nil
```

To:

```go
case sig := <-sigCh:
    w.logger.Info("Wrapper exiting",
        "reason", "signal",
        "signal", sig.String(),
    )
    if w.claudeCmd != nil && w.claudeCmd.Process != nil {
        _ = w.claudeCmd.Process.Signal(sig)
    }
    <-w.claudeExited
    return nil
```

**Step 5: Also capture exitErr in the restart case**

At line 288-289, the `exitErr` variable is now captured. In the restart branch (`if w.state == stateRestarting`), it's fine to ignore it — the restart was intentional. No change needed there.

**Step 6: Run checks**

Run: `make check`
Expected: PASS

**Step 7: Commit**

```
git add internal/wrapper/wrapper.go
git commit -m "Add wrapper lifecycle logging and switch to JSON format"
```

---

### Task 7: Update system-architecture.md

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Add requests.log to the server section**

In the server file listing section (around line 82), after the `server.log` line, add:

```
- Request log: `$AGENC_DIRPATH/server/requests.log` (structured JSON, one line per HTTP request)
```

**Step 2: Add requests.log to the directory tree**

In the directory tree (around line 298), after `server.log`, add:

```
│   ├── requests.log                       # Structured HTTP request log (JSON lines)
```

**Step 3: Commit**

```
git add docs/system-architecture.md
git commit -m "Document requests.log in system architecture"
```

---

### Task 8: Manual smoke test

**Step 1: Build**

Run: `make build`

**Step 2: Verify request logging**

Start the server and trigger a request:

```
./agenc server stop
./agenc server start
./agenc mission ls
```

Then check `~/.agenc/server/requests.log`:

```
cat ~/.agenc/server/requests.log
```

Expected: JSON lines with method, path, status, duration_ms for the `GET /missions` request.

**Step 3: Verify wrapper logging**

Open a mission and close it. Check the wrapper log:

```
cat ~/.agenc/missions/<uuid>/wrapper.log
```

Expected: JSON lines including "Wrapper started" and "Wrapper exiting" entries.

**Step 4: Commit any fixes**

If smoke testing reveals issues, fix and commit.
