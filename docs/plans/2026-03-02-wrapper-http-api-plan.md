Wrapper HTTP API Implementation Plan
=====================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the wrapper's custom JSON socket protocol with HTTP-over-unix-socket and expose Claude state to mission status responses.

**Architecture:** The wrapper gets an `net/http` server on its existing unix socket. Three endpoints: `GET /status` (read-only state query), `POST /restart`, `POST /claude-update`. The server queries wrapper status on demand when building `MissionResponse`. A `sync.RWMutex` protects wrapper state for concurrent HTTP reads.

**Tech Stack:** Go stdlib `net/http`, existing `internal/server.Client` pattern for the wrapper HTTP client.

---

### Task 1: Add wrapper HTTP server replacing custom socket protocol

**Files:**
- Rewrite: `internal/wrapper/socket.go`
- Modify: `internal/wrapper/wrapper.go`

**Step 1: Write the failing test**

Rewrite `internal/wrapper/wrapper_integration_test.go` to use HTTP instead of raw socket. Start with a single test for `GET /status`. The test creates a wrapper, starts the HTTP server, and queries status via HTTP client.

Replace `waitForSocket` to use HTTP health check instead of raw socket connect. Replace `sendAndWait` helpers to use HTTP POST.

Add this test function:

```go
func TestGetStatus(t *testing.T) {
	setup := setupTest(t)
	defer setup.cleanup()

	w := createTestWrapper(setup.agencDirpath, setup.missionID, "github.com/test/repo")
	w.state = stateRunning
	w.claudeIdle = true
	w.hasConversation = true

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w.commandCh = make(chan commandWithResponse, 1)
	go startHTTPServer(ctx, setup.socketFilepath, w, w.logger)
	time.Sleep(100 * time.Millisecond)

	// Query status via HTTP
	client := NewWrapperClient(setup.socketFilepath, 1*time.Second)
	status, err := client.GetStatus()
	if err != nil {
		t.Fatalf("failed to get status: %v", err)
	}

	if status.ClaudeState != "idle" {
		t.Errorf("expected claude_state=idle, got %s", status.ClaudeState)
	}
	if status.WrapperState != "running" {
		t.Errorf("expected wrapper_state=running, got %s", status.WrapperState)
	}
	if !status.HasConversation {
		t.Error("expected has_conversation=true")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `make check` (via `dangerouslyDisableSandbox: true`)
Expected: FAIL — `startHTTPServer`, `NewWrapperClient`, `StatusResponse` do not exist yet.

**Step 3: Implement the HTTP server in socket.go**

Rewrite `internal/wrapper/socket.go` entirely. Replace the raw socket listener with:

```go
package wrapper

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"
)

// StatusResponse is the JSON response from GET /status.
type StatusResponse struct {
	ClaudeState     string `json:"claude_state"`
	WrapperState    string `json:"wrapper_state"`
	HasConversation bool   `json:"has_conversation"`
}

// RestartRequest is the JSON body for POST /restart.
type RestartRequest struct {
	Mode   string `json:"mode,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// ClaudeUpdateRequest is the JSON body for POST /claude-update.
type ClaudeUpdateRequest struct {
	Event            string `json:"event"`
	NotificationType string `json:"notification_type,omitempty"`
}

// CommandResponse is the JSON response for POST endpoints.
type CommandResponse struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// commandWithResponse pairs a Command with a channel for sending back the CommandResponse.
type commandWithResponse struct {
	cmd        Command
	responseCh chan<- CommandResponse
}

// Command is the internal representation of a wrapper command (used by the
// main event loop). Kept for compatibility with handleCommand/handleRestartCommand/handleClaudeUpdate.
type Command struct {
	Command          string
	Mode             string
	Reason           string
	Event            string
	NotificationType string
}

const (
	httpReadTimeout  = 5 * time.Second
	httpWriteTimeout = 5 * time.Second
)

// startHTTPServer creates an HTTP server on the unix socket and blocks until
// ctx is cancelled. The wrapper's state fields are protected by stateMu.
func startHTTPServer(ctx context.Context, socketFilepath string, w *Wrapper, logger *slog.Logger) {
	os.Remove(socketFilepath)

	listener, err := net.Listen("unix", socketFilepath)
	if err != nil {
		logger.Warn("Failed to create wrapper socket", "path", socketFilepath, "error", err)
		return
	}

	if err := os.Chmod(socketFilepath, 0600); err != nil {
		logger.Warn("Failed to set socket permissions", "path", socketFilepath, "error", err)
		listener.Close()
		os.Remove(socketFilepath)
		return
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", func(rw http.ResponseWriter, r *http.Request) {
		w.stateMu.RLock()
		resp := StatusResponse{
			ClaudeState:     w.getClaudeStateString(),
			WrapperState:    w.getWrapperStateString(),
			HasConversation: w.hasConversation,
		}
		w.stateMu.RUnlock()

		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(resp)
	})

	mux.HandleFunc("POST /restart", func(rw http.ResponseWriter, r *http.Request) {
		var req RestartRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(rw).Encode(CommandResponse{Status: "error", Error: "invalid JSON"})
			return
		}

		cmd := Command{
			Command: "restart",
			Mode:    req.Mode,
			Reason:  req.Reason,
		}

		responseCh := make(chan CommandResponse, 1)
		w.commandCh <- commandWithResponse{cmd: cmd, responseCh: responseCh}
		resp := <-responseCh

		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(resp)
	})

	mux.HandleFunc("POST /claude-update", func(rw http.ResponseWriter, r *http.Request) {
		var req ClaudeUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(rw).Encode(CommandResponse{Status: "error", Error: "invalid JSON"})
			return
		}

		cmd := Command{
			Command:          "claude_update",
			Event:            req.Event,
			NotificationType: req.NotificationType,
		}

		responseCh := make(chan CommandResponse, 1)
		w.commandCh <- commandWithResponse{cmd: cmd, responseCh: responseCh}
		resp := <-responseCh

		rw.Header().Set("Content-Type", "application/json")
		json.NewEncoder(rw).Encode(resp)
	})

	server := &http.Server{
		Handler:      mux,
		ReadTimeout:  httpReadTimeout,
		WriteTimeout: httpWriteTimeout,
	}

	go func() {
		<-ctx.Done()
		server.Close()
		os.Remove(socketFilepath)
	}()

	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		logger.Warn("HTTP server error", "error", err)
	}
}
```

**Step 4: Add `stateMu` and state helper methods to wrapper.go**

In `internal/wrapper/wrapper.go`, add to the `Wrapper` struct:

```go
// stateMu protects fields read by the HTTP GET /status handler concurrently
// with the main event loop writing them: claudeIdle, hasConversation, state,
// needsAttention.
stateMu sync.RWMutex

// needsAttention tracks whether Claude needs user attention (permission prompt,
// idle prompt, etc.). Set by Notification events, cleared by UserPromptSubmit
// and PostToolUse events.
needsAttention bool
```

Add state string helper methods:

```go
// getClaudeStateString returns the Claude state as a string for the status API.
// Must be called with stateMu held (at least RLock).
func (w *Wrapper) getClaudeStateString() string {
	if w.needsAttention {
		return "needs_attention"
	}
	if w.claudeIdle {
		return "idle"
	}
	return "busy"
}

// getWrapperStateString returns the wrapper state as a string for the status API.
// Must be called with stateMu held (at least RLock).
func (w *Wrapper) getWrapperStateString() string {
	switch w.state {
	case stateRestartPending:
		return "restart_pending"
	case stateRestarting:
		return "restarting"
	default:
		return "running"
	}
}
```

Update `handleClaudeUpdate` to set/clear `needsAttention` and acquire write lock:

- `UserPromptSubmit`: set `needsAttention = false`
- `PostToolUse`, `PostToolUseFailure`: set `needsAttention = false`
- `Notification` with attention types: set `needsAttention = true`
- `Stop`: set `needsAttention = false` (idle trumps needs-attention)

Wrap all state mutations in `handleClaudeUpdate` and `handleRestartCommand` with `w.stateMu.Lock()` / `w.stateMu.Unlock()`.

Update `handleCommand` to return `CommandResponse` instead of `Response` (rename the type).

Change the `listenSocket` call in `Run()` (line 215) to `startHTTPServer`:

```go
go startHTTPServer(ctx, socketFilepath, w, w.logger)
```

**Step 5: Run test to verify it passes**

Run: `make check` (via `dangerouslyDisableSandbox: true`)
Expected: `TestGetStatus` PASS. Other tests still fail (they use old protocol).

**Step 6: Commit**

```
git add internal/wrapper/socket.go internal/wrapper/wrapper.go
git commit -m "Replace wrapper custom socket protocol with HTTP server"
```

---

### Task 2: Add wrapper HTTP client

**Files:**
- Rewrite: `internal/wrapper/client.go`

**Step 1: Write failing tests for the HTTP client**

Add tests in `internal/wrapper/wrapper_integration_test.go` for `POST /restart` and `POST /claude-update` using the new HTTP client.

**Step 2: Run tests to verify they fail**

Run: `make check`
Expected: FAIL — `NewWrapperClient`, `Restart`, `SendClaudeUpdate` do not exist.

**Step 3: Implement the HTTP client**

Rewrite `internal/wrapper/client.go`:

```go
package wrapper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"time"

	"github.com/mieubrisse/stacktrace"
)

// ErrWrapperNotRunning is returned when the wrapper socket does not exist
// or the connection is refused.
var ErrWrapperNotRunning = errors.New("wrapper is not running")

// WrapperClient is an HTTP client that connects to a wrapper's unix socket.
type WrapperClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewWrapperClient creates a client for the wrapper at the given socket path.
func NewWrapperClient(socketFilepath string, timeout time.Duration) *WrapperClient {
	return &WrapperClient{
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.DialTimeout("unix", socketFilepath, timeout)
				},
			},
			Timeout: timeout,
		},
		baseURL: "http://wrapper",
	}
}

// GetStatus queries the wrapper's current state.
func (c *WrapperClient) GetStatus() (*StatusResponse, error) {
	resp, err := c.httpClient.Get(c.baseURL + "/status")
	if err != nil {
		if isConnectionError(err) {
			return nil, ErrWrapperNotRunning
		}
		return nil, stacktrace.Propagate(err, "failed to query wrapper status")
	}
	defer resp.Body.Close()

	var status StatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, stacktrace.Propagate(err, "failed to decode wrapper status")
	}
	return &status, nil
}

// Restart sends a restart command to the wrapper.
func (c *WrapperClient) Restart(mode, reason string) error {
	req := RestartRequest{Mode: mode, Reason: reason}
	body, _ := json.Marshal(req)

	resp, err := c.httpClient.Post(c.baseURL+"/restart", "application/json", bytes.NewReader(body))
	if err != nil {
		if isConnectionError(err) {
			return ErrWrapperNotRunning
		}
		return stacktrace.Propagate(err, "failed to send restart command")
	}
	defer resp.Body.Close()

	var cmdResp CommandResponse
	if err := json.NewDecoder(resp.Body).Decode(&cmdResp); err != nil {
		return stacktrace.Propagate(err, "failed to decode restart response")
	}
	if cmdResp.Status == "error" {
		return stacktrace.NewError("restart failed: %s", cmdResp.Error)
	}
	return nil
}

// SendClaudeUpdate sends a Claude hook event to the wrapper.
func (c *WrapperClient) SendClaudeUpdate(event, notificationType string) error {
	req := ClaudeUpdateRequest{Event: event, NotificationType: notificationType}
	body, _ := json.Marshal(req)

	resp, err := c.httpClient.Post(c.baseURL+"/claude-update", "application/json", bytes.NewReader(body))
	if err != nil {
		if isConnectionError(err) {
			return ErrWrapperNotRunning
		}
		return stacktrace.Propagate(err, "failed to send claude update")
	}
	defer resp.Body.Close()

	var cmdResp CommandResponse
	if err := json.NewDecoder(resp.Body).Decode(&cmdResp); err != nil {
		return stacktrace.Propagate(err, "failed to decode claude update response")
	}
	if cmdResp.Status == "error" {
		return stacktrace.NewError("claude update failed: %s", cmdResp.Error)
	}
	return nil
}

// isConnectionError checks if an error indicates the wrapper is not reachable.
func isConnectionError(err error) bool {
	if os.IsNotExist(err) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		var sysErr *os.SyscallError
		if errors.As(opErr.Err, &sysErr) {
			return sysErr.Syscall == "connect"
		}
	}
	return false
}
```

**Step 4: Run tests to verify they pass**

Run: `make check`
Expected: PASS

**Step 5: Commit**

```
git add internal/wrapper/client.go
git commit -m "Replace wrapper raw socket client with HTTP client"
```

---

### Task 3: Update all integration tests to use HTTP protocol

**Files:**
- Rewrite: `internal/wrapper/wrapper_integration_test.go`

**Step 1: Rewrite all tests**

Update every test in `wrapper_integration_test.go` to:

1. Use `startHTTPServer` instead of `listenSocket`
2. Use `NewWrapperClient` + typed methods instead of `SendCommand`/`SendCommandWithTimeout`
3. Update `waitForSocket` to poll `GET /status` via HTTP
4. Update `sendAndWait` helper (in `TestClaudeUpdateEventsStateTracking`) to use HTTP POST
5. `TestInvalidJSON` — send raw malformed HTTP request or use HTTP POST with invalid JSON body
6. `TestJSONProtocol` — verify `GET /status` returns valid JSON via HTTP
7. `TestSocketProtocol` — rename to `TestHTTPProtocol`, use HTTP client for all subtests

Key changes for each test:

- `TestGracefulRestart`: replace `SendCommandWithTimeout` with `client.Restart("graceful", "test")` and `client.SendClaudeUpdate("Stop", "")`
- `TestHardRestart`: replace with `client.Restart("hard", "test hard restart")`
- `TestRestartIdempotency`: replace both restart sends with `client.Restart`
- `TestSocketProtocol` → `TestHTTPProtocol`: use typed client methods
- `TestClaudeUpdateEventsStateTracking`: use `client.SendClaudeUpdate` and verify state via `client.GetStatus()`
- `TestJSONProtocol`: use `client.SendClaudeUpdate` and verify response
- `TestInvalidJSON`: send raw HTTP POST with malformed JSON body, verify 400 response

**Step 2: Run tests**

Run: `make check`
Expected: ALL tests PASS

**Step 3: Commit**

```
git add internal/wrapper/wrapper_integration_test.go
git commit -m "Update wrapper integration tests for HTTP protocol"
```

---

### Task 4: Update CLI caller to use HTTP client

**Files:**
- Modify: `cmd/mission_send_claude_update.go`

**Step 1: Write test or verify manually**

The `mission_send_claude_update.go` command is called by Claude hooks. It currently uses `wrapper.SendCommandWithTimeout`. Update it to use `wrapper.NewWrapperClient` + `SendClaudeUpdate`.

**Step 2: Update the command**

In `cmd/mission_send_claude_update.go`, replace:

```go
socketCmd := wrapper.Command{
	Command:          "claude_update",
	Event:            event,
	NotificationType: notificationType,
}
resp, err := wrapper.SendCommandWithTimeout(socketFilepath, socketCmd, claudeUpdateClientTimeout)
```

With:

```go
client := wrapper.NewWrapperClient(socketFilepath, claudeUpdateClientTimeout)
err = client.SendClaudeUpdate(event, notificationType)
```

Remove unused imports (`wrapper.Command` is no longer needed from outside the package).

**Step 3: Run tests**

Run: `make check`
Expected: PASS

**Step 4: Commit**

```
git add cmd/mission_send_claude_update.go
git commit -m "Update claude-update CLI command to use wrapper HTTP client"
```

---

### Task 5: Add `ClaudeState` to `MissionResponse` and server enrichment

**Files:**
- Modify: `internal/server/missions.go`

**Step 1: Write failing test**

Add a test in `internal/server/server_test.go` (or wherever server handler tests live) that verifies `GET /missions/{id}` includes a `claude_state` field. Since the wrapper won't be running in tests, verify it returns `null`.

If no server handler test infrastructure exists, this can be verified manually or via the CLI after integration.

**Step 2: Add `ClaudeState` field to `MissionResponse`**

In `internal/server/missions.go`, add to `MissionResponse`:

```go
ClaudeState *string `json:"claude_state"`
```

**Step 3: Add enrichment logic**

Add a method to query a running wrapper's status:

```go
// queryWrapperClaudeState queries the wrapper for a running mission's Claude state.
// Returns nil if the wrapper is not running or unreachable.
func (s *Server) queryWrapperClaudeState(missionID string) *string {
	pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, missionID)
	pid, err := ReadPID(pidFilepath)
	if err != nil || pid == 0 || !IsProcessRunning(pid) {
		return nil
	}

	socketFilepath := config.GetMissionSocketFilepath(s.agencDirpath, missionID)
	client := wrapper.NewWrapperClient(socketFilepath, 500*time.Millisecond)
	status, err := client.GetStatus()
	if err != nil {
		return nil
	}
	return &status.ClaudeState
}
```

Update `toMissionResponse` to accept the server (or make enrichment a separate step). Add a new function:

```go
func (s *Server) enrichMissionResponse(resp *MissionResponse) {
	resp.ClaudeState = s.queryWrapperClaudeState(resp.ID)
}
```

Call `enrichMissionResponse` in:
- `handleGetMission` — after `toMissionResponse`
- `handleListMissions` — after `toMissionResponses`, concurrently via goroutines for each mission

For `handleListMissions`, enrich concurrently:

```go
var wg sync.WaitGroup
for i := range responses {
	wg.Add(1)
	go func(idx int) {
		defer wg.Done()
		s.enrichMissionResponse(&responses[idx])
	}(i)
}
wg.Wait()
```

Also update `toMissionResponse` in `ToMission()` to round-trip `ClaudeState` if needed (or not — it's server-only enrichment, not stored in DB).

**Step 4: Run tests**

Run: `make check`
Expected: PASS

**Step 5: Commit**

```
git add internal/server/missions.go
git commit -m "Add claude_state to mission API responses via wrapper HTTP query"
```

---

### Task 6: Update CLI to display Claude state

**Files:**
- Modify: `cmd/mission_ls.go`
- Modify: `cmd/mission_helpers.go` (if needed)
- Modify: `cmd/mission_inspect.go`

**Step 1: Update `getMissionStatus`**

The current `getMissionStatus` does a local PID check. Now that the server API returns `claude_state`, the CLI can use it directly. However, `getMissionStatus` is called from multiple places and receives `(missionID, dbStatus)` — it doesn't have the full `MissionResponse`.

Two options:
- (a) Change `getMissionStatus` to also accept `*string` for `claudeState`
- (b) Refactor call sites to pass the full response

Option (a) is simpler. Update the signature:

```go
func getMissionStatus(missionID string, dbStatus string, claudeState *string) string {
	if dbStatus == "archived" {
		return "ARCHIVED"
	}
	if claudeState != nil {
		switch *claudeState {
		case "idle":
			return "RUNNING (idle)"
		case "busy":
			return "RUNNING (busy)"
		case "needs_attention":
			return "RUNNING (attention)"
		default:
			return "RUNNING"
		}
	}
	// Fallback: check PID (for callers that don't have claude_state)
	pidFilepath := config.GetMissionPIDFilepath(agencDirpath, missionID)
	pid, err := server.ReadPID(pidFilepath)
	if err == nil && pid != 0 && server.IsProcessRunning(pid) {
		return "RUNNING"
	}
	return "STOPPED"
}
```

Update all call sites to pass `nil` where `claudeState` is not available, and the actual value where it is (in `runMissionLs` which fetches from the server API).

This requires `MissionResponse` to be available in the CLI. Currently `runMissionLs` uses `fetchMissions()` which returns `[]*database.Mission`. The `database.Mission` struct doesn't have `ClaudeState`. Options:
- Add `ClaudeState *string` to `database.Mission` as a transient field (like `ResolvedSessionTitle`)
- Or change the CLI to work with `MissionResponse` directly

The transient field approach is simpler — add `ClaudeState *string` to `database.Mission`, populate it in `ToMission()`, and use it in the CLI.

**Step 2: Update `database.Mission`**

Add to `internal/database/missions.go`:

```go
// ClaudeState is a transient field populated by the server API. Not stored in DB.
ClaudeState *string `json:"-"`
```

Update `MissionResponse.ToMission()` in `internal/server/missions.go` to copy it:

```go
func (mr *MissionResponse) ToMission() *database.Mission {
	m := &database.Mission{
		// ... existing fields ...
	}
	m.ClaudeState = mr.ClaudeState
	return m
}
```

**Step 3: Update call sites**

In `cmd/mission_ls.go`:
```go
status := getMissionStatus(m.ID, m.Status, m.ClaudeState)
```

In `cmd/mission_inspect.go`:
```go
status := getMissionStatus(missionID, mission.Status, mission.ClaudeState)
```

In `cmd/mission_helpers.go`, `cmd/tmux_switch.go`, `cmd/cron_ls.go`, `cmd/cron_history.go`, `cmd/mission_update_config.go` — update calls to pass `nil` or `m.ClaudeState` as appropriate.

**Step 4: Update color mapping**

In `cmd/mission_ls.go`, the color switch currently matches on `"RUNNING"`, `"ARCHIVED"`. Update to handle the new status strings:

```go
case strings.HasPrefix(status, "RUNNING"):
	// green
```

**Step 5: Run tests**

Run: `make check`
Expected: PASS

**Step 6: Commit**

```
git add cmd/mission_ls.go cmd/mission_helpers.go cmd/mission_inspect.go
git add cmd/tmux_switch.go cmd/cron_ls.go cmd/cron_history.go cmd/mission_update_config.go
git add internal/database/missions.go internal/server/missions.go
git commit -m "Display Claude state in mission ls and inspect output"
```

---

### Task 7: Update system architecture doc

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Update the wrapper section**

Update the wrapper communication description to reflect HTTP-over-unix-socket instead of custom JSON protocol. Mention the three endpoints. Update any mermaid diagrams that show the socket protocol.

**Step 2: Commit**

```
git add docs/system-architecture.md
git commit -m "Update architecture doc for wrapper HTTP API"
```

---

### Task 8: Final verification

**Step 1: Build the full binary**

Run: `make build`
Expected: Clean build, no errors.

**Step 2: Run all tests**

Run: `make check`
Expected: All tests pass.

**Step 3: Manual smoke test**

1. Start the server: `./agenc server start`
2. Create a mission: `./agenc mission new <repo>`
3. Run `./agenc mission ls` — verify running mission shows `RUNNING (busy)` or `RUNNING (idle)`
4. Stop the mission: `./agenc mission stop <id>`
5. Run `./agenc mission ls` — verify it shows `STOPPED`

**Step 4: Commit any remaining changes and push**

```
git push
```
