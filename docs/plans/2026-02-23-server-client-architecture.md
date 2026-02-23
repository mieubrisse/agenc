Server/Client Architecture Implementation Plan
===============================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate AgenC from a CLI-direct model to a server/client architecture where the CLI is a stateless HTTP client and a long-running server owns the database, mission lifecycle, and background loops.

**Architecture:** A thin HTTP REST server listens on a unix socket at `$AGENC_DIRPATH/server/server.sock`. The CLI sends HTTP requests to the server. Wrappers remain separate OS processes. The server absorbs the daemon's background loops. Migration is incremental (strangler pattern) across 5 phases.

**Tech Stack:** Go stdlib `net/http` (ServeMux with Go 1.22+ routing), `encoding/json`, unix sockets via `net.Listen("unix", ...)`. No external HTTP framework.

**Design doc:** `docs/plans/2026-02-23-server-client-architecture-design.md`

---

Phase 0: Server Skeleton
-------------------------

### Task 0.1: Add server config path helpers

**Files:**
- Modify: `internal/config/config.go`

**Step 1: Add server constants and path helpers**

Add these constants alongside the existing `DaemonDirname` constants (around line 25):

```go
ServerDirname      = "server"
ServerPIDFilename  = "server.pid"
ServerLogFilename  = "server.log"
ServerSocketFilename = "server.sock"
```

Add these path helper functions after the existing `GetDaemon*` functions (after line 137):

```go
func GetServerDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ServerDirname)
}

func GetServerPIDFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ServerDirname, ServerPIDFilename)
}

func GetServerLogFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ServerDirname, ServerLogFilename)
}

func GetServerSocketFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ServerDirname, ServerSocketFilename)
}
```

**Step 2: Add server directory to EnsureDirStructure**

In `EnsureDirStructure` (line 67), add to the `dirs` slice:

```go
filepath.Join(agencDirpath, ServerDirname),
```

**Step 3: Verify build**

Run: `make build`

**Step 4: Commit**

```
git add internal/config/config.go
git commit -m "Add server directory path helpers to config package"
```

---

### Task 0.2: Create server package with HTTP listener

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/errors.go`

**Step 1: Create the error response helpers**

Create `internal/server/errors.go` — Docker-style error responses:

```go
package server

import (
	"encoding/json"
	"net/http"
)

type errorResponse struct {
	Message string `json:"message"`
}

func writeError(w http.ResponseWriter, statusCode int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(errorResponse{Message: message})
}

func writeJSON(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(data)
}
```

**Step 2: Create the server struct and listener**

Create `internal/server/server.go`:

```go
package server

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"sync"

	"github.com/mieubrisse/stacktrace"
)

// Server is the AgenC HTTP server that manages missions and background loops.
type Server struct {
	agencDirpath string
	socketPath   string
	logger       *log.Logger
	httpServer   *http.Server
	listener     net.Listener
}

// NewServer creates a new Server instance.
func NewServer(agencDirpath string, socketPath string, logger *log.Logger) *Server {
	return &Server{
		agencDirpath: agencDirpath,
		socketPath:   socketPath,
		logger:       logger,
	}
}

// Run starts the HTTP server on the unix socket and blocks until ctx is cancelled.
// It performs graceful shutdown when the context is cancelled.
func (s *Server) Run(ctx context.Context) error {
	// Clean up stale socket file from a previous run
	os.Remove(s.socketPath)

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to listen on unix socket '%s'", s.socketPath)
	}
	s.listener = listener

	// Restrict socket permissions
	if err := os.Chmod(s.socketPath, 0600); err != nil {
		listener.Close()
		return stacktrace.Propagate(err, "failed to set socket permissions")
	}

	mux := http.NewServeMux()
	s.registerRoutes(mux)

	s.httpServer = &http.Server{
		Handler: mux,
	}

	s.logger.Printf("Server listening on %s", s.socketPath)

	var wg sync.WaitGroup

	// Start HTTP server in a goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := s.httpServer.Serve(listener); err != http.ErrServerClosed {
			s.logger.Printf("HTTP server error: %v", err)
		}
	}()

	// Wait for context cancellation, then gracefully shut down
	<-ctx.Done()
	s.logger.Println("Server shutting down...")

	if err := s.httpServer.Shutdown(context.Background()); err != nil {
		s.logger.Printf("HTTP server shutdown error: %v", err)
	}

	wg.Wait()
	os.Remove(s.socketPath)
	s.logger.Println("Server stopped")

	return nil
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /health", s.handleHealth)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
```

**Step 3: Verify build**

Run: `make build`

**Step 4: Commit**

```
git add internal/server/
git commit -m "Add server package with HTTP listener and health endpoint"
```

---

### Task 0.3: Create server process management

**Files:**
- Create: `internal/server/process.go`

**Step 1: Create process management functions**

Create `internal/server/process.go` — follows the same pattern as `internal/daemon/process.go` but with `AGENC_SERVER_PROCESS` env var and `server start` subcommand:

```go
package server

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/mieubrisse/stacktrace"
)

const (
	serverEnvVar    = "AGENC_SERVER_PROCESS"
	stopPollTimeout = 3 * time.Second
	stopPollTick    = 100 * time.Millisecond
)

// IsServerProcess returns true if this process was launched as the server child.
func IsServerProcess() bool {
	return os.Getenv(serverEnvVar) == "1"
}

// ForkServer re-executes the current binary as a background server process.
func ForkServer(logFilepath string, pidFilepath string) error {
	executableFilepath, err := os.Executable()
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine executable path")
	}

	logFile, err := os.OpenFile(logFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open server log file")
	}

	cmd := exec.Command(executableFilepath, "server", "start")
	cmd.Env = append(os.Environ(), fmt.Sprintf("%s=1", serverEnvVar))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return stacktrace.Propagate(err, "failed to start server process")
	}

	pid := cmd.Process.Pid
	logFile.Close()

	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(pid)), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write PID file")
	}

	if err := cmd.Process.Release(); err != nil {
		return stacktrace.Propagate(err, "failed to release server process")
	}

	return nil
}

// ReadPID reads the server PID from the PID file. Returns 0 if the file
// does not exist or is empty.
func ReadPID(pidFilepath string) (int, error) {
	data, err := os.ReadFile(pidFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, stacktrace.Propagate(err, "failed to read PID file")
	}

	pidStr := strings.TrimSpace(string(data))
	if pidStr == "" {
		return 0, nil
	}

	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, stacktrace.Propagate(err, "invalid PID in file: '%s'", pidStr)
	}

	return pid, nil
}

// IsRunning checks if the server process is running.
func IsRunning(pidFilepath string) bool {
	pid, err := ReadPID(pidFilepath)
	if err != nil || pid == 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// StopServer sends SIGTERM to the server and waits for exit.
func StopServer(pidFilepath string) error {
	pid, err := ReadPID(pidFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read server PID")
	}

	if pid == 0 {
		os.Remove(pidFilepath)
		return stacktrace.NewError("server is not running")
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return stacktrace.Propagate(err, "failed to find server process")
	}

	if process.Signal(syscall.Signal(0)) != nil {
		os.Remove(pidFilepath)
		return stacktrace.NewError("server is not running")
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return stacktrace.Propagate(err, "failed to send SIGTERM to server (PID %d)", pid)
	}

	deadline := time.Now().Add(stopPollTimeout)
	for time.Now().Before(deadline) {
		if process.Signal(syscall.Signal(0)) != nil {
			os.Remove(pidFilepath)
			return nil
		}
		time.Sleep(stopPollTick)
	}

	_ = process.Signal(syscall.SIGKILL)
	os.Remove(pidFilepath)

	return nil
}
```

**Step 2: Verify build**

Run: `make build`

**Step 3: Commit**

```
git add internal/server/process.go
git commit -m "Add server process management (fork, PID, stop)"
```

---

### Task 0.4: Create CLI commands for server start/stop/status

**Files:**
- Create: `cmd/server.go`
- Create: `cmd/server_start.go`
- Create: `cmd/server_stop.go`
- Create: `cmd/server_status.go`

**Step 1: Create parent command**

Create `cmd/server.go`:

```go
package cmd

import "github.com/spf13/cobra"

const serverCmdStr = "server"

var serverCmd = &cobra.Command{
	Use:   serverCmdStr,
	Short: "Manage the AgenC server",
}

func init() {
	rootCmd.AddCommand(serverCmd)
}
```

**Step 2: Create server start command**

Create `cmd/server_start.go` — follows the same dual-path pattern as `cmd/daemon_start.go` (parent forks, child runs the loop):

```go
package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
)

var serverStartCmd = &cobra.Command{
	Use:   startCmdStr,
	Short: "Start the AgenC server",
	RunE:  runServerStart,
}

func init() {
	serverCmd.AddCommand(serverStartCmd)
}

func runServerStart(cmd *cobra.Command, args []string) error {
	if _, err := getAgencContext(); err != nil {
		return err
	}
	if server.IsServerProcess() {
		return runServerLoop()
	}
	return forkServer()
}

func runServerLoop() error {
	pidFilepath := config.GetServerPIDFilepath(agencDirpath)

	if err := os.WriteFile(pidFilepath, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write PID file")
	}

	logFilepath := config.GetServerLogFilepath(agencDirpath)
	logFile, err := os.OpenFile(logFilepath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open log file")
	}
	defer logFile.Close()

	logger := log.New(logFile, "", log.LstdFlags)

	socketFilepath := config.GetServerSocketFilepath(agencDirpath)
	srv := server.NewServer(agencDirpath, socketFilepath, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		sig := <-sigCh
		logger.Printf("Received signal: %v", sig)
		cancel()
	}()

	if err := srv.Run(ctx); err != nil {
		return err
	}

	os.Remove(pidFilepath)
	logger.Println("Server exited")

	return nil
}

func forkServer() error {
	pidFilepath := config.GetServerPIDFilepath(agencDirpath)
	logFilepath := config.GetServerLogFilepath(agencDirpath)

	if server.IsRunning(pidFilepath) {
		pid, _ := server.ReadPID(pidFilepath)
		fmt.Printf("Server is already running (PID %d).\n", pid)
		return nil
	}

	if err := server.ForkServer(logFilepath, pidFilepath); err != nil {
		return stacktrace.Propagate(err, "failed to fork server")
	}

	newPID, _ := server.ReadPID(pidFilepath)
	fmt.Printf("Server started (PID %d).\n", newPID)

	return nil
}

// ensureServerRunning idempotently starts the server if not already running.
func ensureServerRunning(agencDirpath string) {
	pidFilepath := config.GetServerPIDFilepath(agencDirpath)
	if server.IsRunning(pidFilepath) {
		return
	}
	logFilepath := config.GetServerLogFilepath(agencDirpath)
	_ = server.ForkServer(logFilepath, pidFilepath)
}
```

**Step 3: Create server stop command**

Create `cmd/server_stop.go`:

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
)

var serverStopCmd = &cobra.Command{
	Use:   stopCmdStr,
	Short: "Stop the AgenC server",
	RunE:  runServerStop,
}

func init() {
	serverCmd.AddCommand(serverStopCmd)
}

func runServerStop(cmd *cobra.Command, args []string) error {
	if _, err := getAgencContext(); err != nil {
		return err
	}

	pidFilepath := config.GetServerPIDFilepath(agencDirpath)
	if err := server.StopServer(pidFilepath); err != nil {
		return err
	}

	fmt.Println("Server stopped.")
	return nil
}
```

**Step 4: Create server status command**

Create `cmd/server_status.go`:

```go
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
)

var serverStatusCmd = &cobra.Command{
	Use:   statusCmdStr,
	Short: "Check AgenC server status",
	RunE:  runServerStatus,
}

func init() {
	serverCmd.AddCommand(serverStatusCmd)
}

func runServerStatus(cmd *cobra.Command, args []string) error {
	if _, err := getAgencContext(); err != nil {
		return err
	}

	pidFilepath := config.GetServerPIDFilepath(agencDirpath)
	pid, err := server.ReadPID(pidFilepath)
	if err != nil {
		return err
	}

	if pid > 0 && server.IsRunning(pidFilepath) {
		fmt.Printf("Server is running (PID %d).\n", pid)
	} else {
		fmt.Println("Server is not running.")
	}

	return nil
}
```

**Step 5: Verify build and test manually**

Run: `make build`
Run: `./agenc server start`
Run: `./agenc server status`
Run: `./agenc server stop`

**Step 6: Commit**

```
git add cmd/server.go cmd/server_start.go cmd/server_stop.go cmd/server_status.go
git commit -m "Add agenc server start/stop/status CLI commands"
```

---

### Task 0.5: Create HTTP client helper for CLI

**Files:**
- Create: `internal/server/client.go`

**Step 1: Create the client**

Create `internal/server/client.go` — HTTP client that talks to the server over the unix socket:

```go
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/mieubrisse/stacktrace"
)

// Client is an HTTP client that connects to the AgenC server via unix socket.
type Client struct {
	httpClient *http.Client
	baseURL    string
}

// NewClient creates a new client that connects to the server at the given socket path.
func NewClient(socketPath string) *Client {
	return &Client{
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.DialTimeout("unix", socketPath, 5*time.Second)
				},
			},
			Timeout: 30 * time.Second,
		},
		// The host doesn't matter for unix sockets, but HTTP requires one
		baseURL: "http://agenc",
	}
}

// Get sends a GET request and decodes the response into result.
func (c *Client) Get(path string, result any) error {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return stacktrace.Propagate(err, "failed to connect to server")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.decodeError(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return stacktrace.Propagate(err, "failed to decode server response")
		}
	}

	return nil
}

// Post sends a POST request with a JSON body and decodes the response into result.
func (c *Client) Post(path string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		pr, pw := io.Pipe()
		go func() {
			pw.CloseWithError(json.NewEncoder(pw).Encode(body))
		}()
		bodyReader = pr
	}

	resp, err := c.httpClient.Post(c.baseURL+path, "application/json", bodyReader)
	if err != nil {
		return stacktrace.Propagate(err, "failed to connect to server")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.decodeError(resp)
	}

	if result != nil {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return stacktrace.Propagate(err, "failed to decode server response")
		}
	}

	return nil
}

// Delete sends a DELETE request.
func (c *Client) Delete(path string) error {
	req, err := http.NewRequest(http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create request")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return stacktrace.Propagate(err, "failed to connect to server")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return c.decodeError(resp)
	}

	return nil
}

func (c *Client) decodeError(resp *http.Response) error {
	var errResp errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errResp); err != nil {
		return stacktrace.NewError("server returned status %d", resp.StatusCode)
	}
	return fmt.Errorf("%s", errResp.Message)
}
```

**Step 2: Verify build**

Run: `make build`

**Step 3: Commit**

```
git add internal/server/client.go
git commit -m "Add HTTP client for CLI-to-server communication"
```

---

### Task 0.6: Regenerate CLI docs and update system architecture

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Regenerate CLI docs**

Run: `make build` (this regenerates CLI docs via `gendocs`)

**Step 2: Update system architecture**

Add the server to the Process Overview section. Add `ServerDirname` to the runtime tree. Add `internal/server/` to the source tree. Note that the server is new and currently only serves `GET /health`.

**Step 3: Commit**

```
git add docs/
git commit -m "Add server to system architecture docs"
```

---

Phase 1: Move Mission Queries to Server
----------------------------------------

### Task 1.1: Add GET /missions endpoint to server

**Files:**
- Modify: `internal/server/server.go`
- Create: `internal/server/missions.go`

**Step 1: Open database in server startup**

Modify `Server` struct to hold a `*database.DB`. Open it in `Run()` before starting the HTTP listener. Close it in the shutdown path.

**Step 2: Create missions handler file**

Create `internal/server/missions.go` with `handleListMissions` that:
- Reads `?include_archived=true` and `?tmux_pane=<id>` query params
- Calls `db.ListMissions(params)` or `db.GetMissionByTmuxPane(paneID)`
- Serializes `[]*database.Mission` as JSON
- Returns 200 with the list

**Step 3: Register route**

Add `mux.HandleFunc("GET /missions", s.handleListMissions)` in `registerRoutes`.

**Step 4: Verify build and test manually**

Run: `make build`
Start server, then: `curl --unix-socket ~/.agenc/server/server.sock http://agenc/missions`

**Step 5: Commit**

---

### Task 1.2: Add GET /missions/{id} endpoint to server

**Files:**
- Modify: `internal/server/missions.go`

**Step 1: Add handler**

Add `handleGetMission` that:
- Extracts `{id}` from path via `r.PathValue("id")`
- Calls `db.ResolveMissionID(id)` then `db.GetMission(resolvedID)`
- Returns 404 if not found
- Returns 200 with the mission JSON

**Step 2: Register route**

Add `mux.HandleFunc("GET /missions/{id}", s.handleGetMission)` in `registerRoutes`.

**Step 3: Verify build and test manually**

**Step 4: Commit**

---

### Task 1.3: Convert mission ls to use HTTP client

**Files:**
- Modify: `cmd/mission_ls.go`

**Step 1: Add server client path**

When the server is running, `mission ls` should call `GET /missions` instead of opening the database directly. Add a check: if server is running, use client; otherwise fall back to direct DB (for backward compatibility during migration).

**Step 2: Ensure server is auto-started**

Call `ensureServerRunning(agencDirpath)` at the start of `runMissionLs`. The `ensureDaemonRunning` call stays for now (both server and daemon run during Phase 1).

**Step 3: Test**

Run: `./agenc mission ls` — should work via server. Stop server — should fall back to direct DB.

**Step 4: Commit**

---

### Task 1.4: Convert tmux resolve-mission to use HTTP client

**Files:**
- Modify: `cmd/tmux_resolve_mission.go`

**Step 1: Use GET /missions?tmux_pane= endpoint**

When the server is running, resolve pane-to-mission via the server instead of direct DB query.

**Step 2: Commit**

---

Phase 2: Move Mission Lifecycle to Server
------------------------------------------

### Task 2.1: Add POST /missions endpoint

**Files:**
- Modify: `internal/server/missions.go`

Accepts JSON body with: `repo`, `prompt`, `tmux_session` (caller's session), `headless`, `adjutant`, `cron_name`, `cron_id`.

Server performs:
1. Create database record
2. Create mission directory (call `mission.CreateMissionDir`)
3. Spawn wrapper in caller's tmux session (call `tmux new-window -t <session>`)
4. Return mission record as JSON

---

### Task 2.2: Add POST /missions/{id}/stop endpoint

**Files:**
- Modify: `internal/server/missions.go`

Server performs:
1. Resolve mission ID
2. Read wrapper PID file
3. Send SIGTERM, poll for exit, SIGKILL fallback
4. Return 200 immediately (or 404 if not found)

The key improvement: this returns immediately to the CLI. The CLI doesn't block.

---

### Task 2.3: Add DELETE /missions/{id} endpoint

**Files:**
- Modify: `internal/server/missions.go`

Server performs:
1. Verify mission is stopped/archived
2. Delete mission directory
3. Delete database record
4. Return 200

---

### Task 2.4: Add POST /missions/{id}/reload endpoint

**Files:**
- Modify: `internal/server/missions.go`

Server performs:
1. Rebuild per-mission config directory
2. Send hard restart command to wrapper socket
3. Return 200

---

### Task 2.5: Add POST /repos/{name}/push-event endpoint

**Files:**
- Create: `internal/server/repos.go`

Server performs:
1. Parse repo name from path
2. Call `mission.ForceUpdateRepo(repoLibraryDirpath)`
3. Return 200

---

### Task 2.6: Convert wrapper to call push-event instead of direct repo update

**Files:**
- Modify: `internal/wrapper/wrapper.go`

In `watchWorkspaceRemoteRefs`, replace the `mission.ForceUpdateRepo` call with an HTTP POST to `/repos/{name}/push-event` via the server client. Fall back to direct update if server is unreachable.

---

### Task 2.7: Convert CLI mission commands to HTTP clients

**Files:**
- Modify: `cmd/mission_new.go`
- Modify: `cmd/mission_stop.go`
- Modify: `cmd/mission_reload.go`
- Modify: `cmd/mission_nuke.go`

Each command becomes: parse args → build request → call server → print result.

Repo selection (fzf picker) stays client-side. Only the create/stop/delete/reload operations move to the server.

---

Phase 3: Absorb the Daemon
---------------------------

### Task 3.1: Move daemon loops into server

**Files:**
- Modify: `internal/server/server.go`

Add the five daemon goroutines to `Server.Run()`:
- Repo update loop
- Config auto-commit loop
- Config watcher loop
- Keybindings writer loop
- Mission summarizer loop

These are direct copies from `internal/daemon/*.go`, started as goroutines in the server's `Run()` method alongside the HTTP listener.

---

### Task 3.2: Replace ensureDaemonRunning with ensureServerRunning

**Files:**
- Modify: all CLI commands that call `ensureDaemonRunning`

Replace every `ensureDaemonRunning(agencDirpath)` call with `ensureServerRunning(agencDirpath)`.

---

### Task 3.3: Migrate daemon CLI commands to server

**Files:**
- Modify: `cmd/daemon.go` and sub-commands

Make `agenc daemon start/stop/status` aliases for `agenc server start/stop/status` (print deprecation warning). Or remove them outright and update documentation.

---

### Task 3.4: Remove daemon package

**Files:**
- Delete: `internal/daemon/` (after all loops moved to server)
- Modify: `internal/config/config.go` — keep `DaemonDirname` constants for cleanup but mark deprecated

---

### Task 3.5: Update system architecture doc

**Files:**
- Modify: `docs/system-architecture.md`

Replace daemon process section with server. Update process overview diagram. Remove `$AGENC_DIRPATH/daemon/` from runtime tree.

---

Phase 4: Tmux Pool + Attach/Detach
-----------------------------------

### Task 4.1: Create tmux pool session management

**Files:**
- Create: `internal/server/pool.go`

Server creates `agenc-pool` tmux session on startup. Manages window creation/destruction within the pool. Provides `linkWindow`/`unlinkWindow` helpers.

---

### Task 4.2: Add POST /missions/{id}/attach endpoint

**Files:**
- Modify: `internal/server/missions.go`

Accepts `tmux_session` in body. Ensures wrapper is running (lazy start). Links pool window into caller's session.

---

### Task 4.3: Add POST /missions/{id}/detach endpoint

**Files:**
- Modify: `internal/server/missions.go`

Unlinks the mission's window from the caller's session. Wrapper keeps running in pool.

---

### Task 4.4: Add idle timeout to server

**Files:**
- Modify: `internal/server/server.go`

New background goroutine that checks `last_active` timestamps. If a mission has been idle for N minutes, gracefully stops its wrapper.

---

### Task 4.5: Move wrapper spawning to pool

**Files:**
- Modify: `internal/server/missions.go`

Change `POST /missions` to create the wrapper window in `agenc-pool` instead of the caller's session, then link it.

---

### Task 4.6: Remove Side Claude/Side Adjutant/pane new

**Files:**
- Modify: `internal/config/agenc_config.go` — remove from `BuiltinPaletteCommands`
- Delete or modify: `cmd/tmux_pane_new.go`

---

Phase 5: Cleanup
----------------

### Task 5.1: Remove all direct database access from CLI

**Files:**
- Modify: `cmd/mission_helpers.go` — remove `openDB()`
- Modify: all remaining CLI commands that open the database directly

Every CLI command that touches the database should go through the server.

---

### Task 5.2: Remove wrapper database dependency

**Files:**
- Modify: `internal/wrapper/wrapper.go`

Remove `db *database.DB` from `Wrapper` struct. Heartbeat writes, `last_active` updates, `prompt_count` increments, pane registration — all either move to the server or are replaced by HTTP calls.

---

### Task 5.3: Final documentation pass

**Files:**
- Modify: `docs/system-architecture.md`
- Modify: `README.md`

Update all documentation to reflect the server/client architecture. Remove references to the daemon. Document the server lifecycle.

---

Testing Strategy
----------------

**Phase 0:** Manual testing — start/stop/status commands, curl health endpoint.

**Phase 1-2:** Each converted command should be tested manually to ensure identical output. Integration tests can use the server client to verify API responses.

**Phase 3:** Full regression — every CLI command should work with only the server running (no daemon).

**Phase 4-5:** Manual testing of attach/detach workflows in tmux.
