Server Logs Implementation Plan
================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `GET /server/logs` endpoint and `agenc server logs print` CLI command so agents and humans can fetch server log content.

**Architecture:** The server handler reads the requested log file (`server.log` or `requests.log`) from disk, returns raw text. The CLI command calls the endpoint via a new `GetRaw()` client method and prints to stdout. Default is tail (last 200 lines); `--all` returns the full file.

**Tech Stack:** Go stdlib `net/http`, Cobra CLI, existing `internal/server` and `internal/config` packages.

---

### Task 1: Add `GetRaw` client method

**Files:**
- Modify: `internal/server/client.go`

**Step 1: Write the `GetRaw` method**

Add after the existing `Patch` method (around line 145), before the high-level API section:

```go
// GetRaw sends a GET request and returns the raw response body as bytes.
// Unlike Get, it does not JSON-decode the response.
func (c *Client) GetRaw(path string) ([]byte, error) {
	resp, err := c.httpClient.Get(c.baseURL + path)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to connect to server")
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return nil, c.decodeError(resp)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read response body")
	}

	return body, nil
}
```

Note: `io` is already imported in `client.go`.

**Step 2: Add the `GetServerLogs` convenience method**

Add at the end of the file, in a new section after the repo API methods:

```go
// ============================================================================
// High-level server API methods
// ============================================================================

// GetServerLogs fetches server log content from the server.
// source is "server" or "requests"; if all is true, returns the entire file.
func (c *Client) GetServerLogs(source string, all bool) ([]byte, error) {
	path := "/server/logs?source=" + source
	if all {
		path += "&mode=all"
	}
	return c.GetRaw(path)
}
```

**Step 3: Commit**

```bash
git add internal/server/client.go
git commit -m "Add GetRaw and GetServerLogs client methods"
```

---

### Task 2: Add the server handler

**Files:**
- Create: `internal/server/handle_server_logs.go`
- Modify: `internal/server/server.go` (add route registration)

**Step 1: Create the handler file**

Create `internal/server/handle_server_logs.go`:

```go
package server

import (
	"bufio"
	"net/http"
	"os"

	"github.com/odyssey/agenc/internal/config"
)

const defaultTailLines = 200

func (s *Server) handleServerLogs(w http.ResponseWriter, r *http.Request) error {
	source := r.URL.Query().Get("source")
	if source == "" {
		source = "server"
	}

	var logFilepath string
	switch source {
	case "server":
		logFilepath = config.GetServerLogFilepath(s.agencDirpath)
	case "requests":
		logFilepath = config.GetServerRequestsLogFilepath(s.agencDirpath)
	default:
		return newHTTPErrorf(http.StatusBadRequest, "invalid source %q: must be \"server\" or \"requests\"", source)
	}

	if _, err := os.Stat(logFilepath); os.IsNotExist(err) {
		return newHTTPError(http.StatusNotFound, "log file does not exist yet")
	}

	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "tail"
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")

	switch mode {
	case "all":
		file, err := os.Open(logFilepath)
		if err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to open log file: %v", err)
		}
		defer file.Close()
		buf := make([]byte, 32*1024)
		for {
			n, readErr := file.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
			}
			if readErr != nil {
				break
			}
		}
	case "tail":
		lines, err := readTailLines(logFilepath, defaultTailLines)
		if err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to read log file: %v", err)
		}
		for i, line := range lines {
			if i > 0 {
				w.Write([]byte("\n"))
			}
			w.Write([]byte(line))
		}
		if len(lines) > 0 {
			w.Write([]byte("\n"))
		}
	default:
		return newHTTPErrorf(http.StatusBadRequest, "invalid mode %q: must be \"tail\" or \"all\"", mode)
	}

	return nil
}

// readTailLines returns the last n lines of a file.
func readTailLines(filepath string, n int) ([]string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) > n {
			lines = lines[1:]
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}
```

**Step 2: Register the route**

In `internal/server/server.go`, in `registerRoutes`, add after the `GET /health` line:

```go
mux.Handle("GET /server/logs", appHandler(s.requestLogger, s.handleServerLogs))
```

**Step 3: Commit**

```bash
git add internal/server/handle_server_logs.go internal/server/server.go
git commit -m "Add GET /server/logs endpoint"
```

---

### Task 3: Add the `server logs print` CLI command

**Files:**
- Create: `cmd/server_logs.go`
- Create: `cmd/server_logs_print.go`

**Step 1: Create the `server logs` group command**

Create `cmd/server_logs.go`:

```go
package cmd

import "github.com/spf13/cobra"

var serverLogsCmd = &cobra.Command{
	Use:   logsCmdStr,
	Short: "View server logs",
}

func init() {
	serverCmd.AddCommand(serverLogsCmd)
}
```

**Step 2: Create the `server logs print` subcommand**

Create `cmd/server_logs_print.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var serverLogsPrintRequestsFlag bool
var serverLogsPrintAllFlag bool

var serverLogsPrintCmd = &cobra.Command{
	Use:   printCmdStr,
	Short: "Print server log content",
	Long: `Print the server operational log (default) or HTTP request log.

By default, prints the last 200 lines. Use --all for the full file.

Examples:
  agenc server logs print
  agenc server logs print --requests
  agenc server logs print --all`,
	RunE: runServerLogsPrint,
}

func init() {
	serverLogsPrintCmd.Flags().BoolVar(&serverLogsPrintRequestsFlag, "requests", false, "show HTTP request log instead of operational log")
	serverLogsPrintCmd.Flags().BoolVar(&serverLogsPrintAllFlag, allFlagName, false, "print entire log file instead of last 200 lines")
	serverLogsCmd.AddCommand(serverLogsPrintCmd)
}

func runServerLogsPrint(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	source := "server"
	if serverLogsPrintRequestsFlag {
		source = "requests"
	}

	body, err := client.GetServerLogs(source, serverLogsPrintAllFlag)
	if err != nil {
		return fmt.Errorf("failed to fetch server logs: %w", err)
	}

	os.Stdout.Write(body)
	return nil
}
```

**Step 3: Run build to verify compilation**

```bash
make build
```

**Step 4: Commit**

```bash
git add cmd/server_logs.go cmd/server_logs_print.go
git commit -m "Add server logs print CLI command"
```

---

### Task 4: Add handler tests

**Files:**
- Create: `internal/server/handle_server_logs_test.go`

**Step 1: Write the test file**

Create `internal/server/handle_server_logs_test.go`:

```go
package server

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestHandleServerLogs_DefaultTailsServerLog(t *testing.T) {
	tmpDir := t.TempDir()
	setupServerLogDir(t, tmpDir)

	// Write more than 200 lines
	var lines []string
	for i := 0; i < 250; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	writeLogFile(t, config.GetServerLogFilepath(tmpDir), strings.Join(lines, "\n")+"\n")

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/server/logs", nil)
	w := httptest.NewRecorder()

	err := srv.handleServerLogs(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body := w.Body.String()
	bodyLines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	if len(bodyLines) != 200 {
		t.Errorf("expected 200 lines, got %d", len(bodyLines))
	}
	if bodyLines[0] != "line 50" {
		t.Errorf("expected first line 'line 50', got %q", bodyLines[0])
	}
}

func TestHandleServerLogs_AllMode(t *testing.T) {
	tmpDir := t.TempDir()
	setupServerLogDir(t, tmpDir)

	content := "line 1\nline 2\nline 3\n"
	writeLogFile(t, config.GetServerLogFilepath(tmpDir), content)

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/server/logs?mode=all", nil)
	w := httptest.NewRecorder()

	err := srv.handleServerLogs(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w.Body.String() != content {
		t.Errorf("expected %q, got %q", content, w.Body.String())
	}
}

func TestHandleServerLogs_RequestsSource(t *testing.T) {
	tmpDir := t.TempDir()
	setupServerLogDir(t, tmpDir)

	content := `{"time":"2026-03-01","level":"INFO","msg":"request"}` + "\n"
	writeLogFile(t, config.GetServerRequestsLogFilepath(tmpDir), content)

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/server/logs?source=requests&mode=all", nil)
	w := httptest.NewRecorder()

	err := srv.handleServerLogs(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w.Body.String() != content {
		t.Errorf("expected %q, got %q", content, w.Body.String())
	}
}

func TestHandleServerLogs_InvalidSource(t *testing.T) {
	tmpDir := t.TempDir()
	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/server/logs?source=invalid", nil)
	w := httptest.NewRecorder()

	err := srv.handleServerLogs(w, req)
	if err == nil {
		t.Fatal("expected error for invalid source")
	}

	httpErr, ok := err.(*httpError)
	if !ok {
		t.Fatalf("expected *httpError, got %T", err)
	}
	if httpErr.status != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", httpErr.status)
	}
}

func TestHandleServerLogs_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/server/logs", nil)
	w := httptest.NewRecorder()

	err := srv.handleServerLogs(w, req)
	if err == nil {
		t.Fatal("expected error for missing log file")
	}

	httpErr, ok := err.(*httpError)
	if !ok {
		t.Fatalf("expected *httpError, got %T", err)
	}
	if httpErr.status != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", httpErr.status)
	}
}

func setupServerLogDir(t *testing.T, agencDirpath string) {
	t.Helper()
	serverDirpath := filepath.Join(agencDirpath, config.ServerDirname)
	if err := os.MkdirAll(serverDirpath, 0755); err != nil {
		t.Fatalf("failed to create server dir: %v", err)
	}
}

func writeLogFile(t *testing.T, filepath string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write log file: %v", err)
	}
}
```

Note: add `"fmt"` to the import block.

**Step 2: Run the tests**

```bash
go test ./internal/server/ -run TestHandleServerLogs -v
```

**Step 3: Commit**

```bash
git add internal/server/handle_server_logs_test.go
git commit -m "Add tests for server logs handler"
```

---

### Task 5: Update architecture doc

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Add endpoint to route list**

In `docs/system-architecture.md`, find the line `- \`GET /health\`` and add after it:

```markdown
- `GET /server/logs` — returns server log content as plain text (supports `source` and `mode` query params)
```

**Step 2: Commit**

```bash
git add docs/system-architecture.md
git commit -m "Document GET /server/logs endpoint in architecture doc"
```

---

### Task 6: Manual smoke test

**Step 1: Build and test**

```bash
make build
./agenc server logs print
./agenc server logs print --requests
./agenc server logs print --all
```

Verify each command prints the expected log content.
