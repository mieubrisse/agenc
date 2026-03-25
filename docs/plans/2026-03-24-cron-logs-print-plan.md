Cron Logs Print Implementation Plan
====================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Let users view cron job logs via `agenc cron logs print <name-or-id>` backed by server endpoints.

**Architecture:** Two new server endpoints (`GET /crons` and `GET /crons/{id}/logs`) serve cron metadata and log file content. The CLI resolves cron names to IDs via the list endpoint, then fetches logs. This keeps all filesystem access on the server side.

**Tech Stack:** Go, net/http (Go 1.22+ routing), Cobra CLI

---

### Task 1: GET /crons server handler + test

**Files:**
- Create: `internal/server/handle_crons.go`
- Create: `internal/server/handle_crons_test.go`

**Step 1: Write the test**

In `internal/server/handle_crons_test.go`:

```go
package server

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestHandleListCrons_ReturnsCronInfo(t *testing.T) {
	enabled := true
	cfg := &config.AgencConfig{
		Crons: map[string]config.CronConfig{
			"daily-report": {
				ID:       "abc-123",
				Schedule: "0 9 * * *",
				Repo:     "mieubrisse/my-repo",
				Enabled:  &enabled,
			},
			"weekly-cleanup": {
				ID:       "def-456",
				Schedule: "0 0 * * SUN",
			},
		},
	}

	srv := &Server{logger: log.New(os.Stderr, "", 0)}
	srv.cachedConfig.Store(cfg)

	req := httptest.NewRequest("GET", "/crons", nil)
	w := httptest.NewRecorder()

	err := srv.handleListCrons(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []CronInfo
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 crons, got %d", len(result))
	}

	// Build a map for order-independent checks
	byName := make(map[string]CronInfo)
	for _, c := range result {
		byName[c.Name] = c
	}

	daily, ok := byName["daily-report"]
	if !ok {
		t.Fatal("missing daily-report")
	}
	if daily.ID != "abc-123" {
		t.Errorf("expected ID abc-123, got %s", daily.ID)
	}
	if daily.Schedule != "0 9 * * *" {
		t.Errorf("expected schedule '0 9 * * *', got %s", daily.Schedule)
	}
	if daily.Repo != "mieubrisse/my-repo" {
		t.Errorf("expected repo mieubrisse/my-repo, got %s", daily.Repo)
	}
}

func TestHandleListCrons_EmptyConfig(t *testing.T) {
	cfg := &config.AgencConfig{}
	srv := &Server{logger: log.New(os.Stderr, "", 0)}
	srv.cachedConfig.Store(cfg)

	req := httptest.NewRequest("GET", "/crons", nil)
	w := httptest.NewRecorder()

	err := srv.handleListCrons(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result []CronInfo
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(result) != 0 {
		t.Errorf("expected 0 crons, got %d", len(result))
	}
}
```

**Step 2: Run test to verify it fails**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: FAIL — `CronInfo` type and `handleListCrons` method undefined.

**Step 3: Write the handler**

In `internal/server/handle_crons.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"sort"
)

// CronInfo represents a cron job in API responses.
type CronInfo struct {
	Name     string `json:"name"`
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
	Repo     string `json:"repo,omitempty"`
}

func (s *Server) handleListCrons(w http.ResponseWriter, r *http.Request) error {
	cfg := s.getConfig()

	crons := make([]CronInfo, 0, len(cfg.Crons))
	for name, cronCfg := range cfg.Crons {
		crons = append(crons, CronInfo{
			Name:     name,
			ID:       cronCfg.ID,
			Schedule: cronCfg.Schedule,
			Repo:     cronCfg.Repo,
		})
	}

	sort.Slice(crons, func(i, j int) bool {
		return crons[i].Name < crons[j].Name
	})

	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(crons)
}
```

**Step 4: Run test to verify it passes**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 5: Commit**

```
git add internal/server/handle_crons.go internal/server/handle_crons_test.go
git commit -m "Add GET /crons handler to serve cron name-to-ID mapping"
```

---

### Task 2: GET /crons/{id}/logs server handler + test

**Files:**
- Create: `internal/server/handle_cron_logs.go`
- Create: `internal/server/handle_cron_logs_test.go`

**Step 1: Write the tests**

In `internal/server/handle_cron_logs_test.go`:

```go
package server

import (
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestHandleCronLogs_DefaultTails(t *testing.T) {
	tmpDir := t.TempDir()
	setupCronLogDir(t, tmpDir)

	cronID := "abc-123"
	var lines []string
	for i := 0; i < 250; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	writeLogFile(t, config.GetCronLogFilepath(tmpDir, cronID), strings.Join(lines, "\n")+"\n")

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/crons/"+cronID+"/logs", nil)
	req.SetPathValue("id", cronID)
	w := httptest.NewRecorder()

	err := srv.handleCronLogs(w, req)
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

func TestHandleCronLogs_AllMode(t *testing.T) {
	tmpDir := t.TempDir()
	setupCronLogDir(t, tmpDir)

	cronID := "abc-123"
	content := "line 1\nline 2\nline 3\n"
	writeLogFile(t, config.GetCronLogFilepath(tmpDir, cronID), content)

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/crons/"+cronID+"/logs?mode=all", nil)
	req.SetPathValue("id", cronID)
	w := httptest.NewRecorder()

	err := srv.handleCronLogs(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w.Body.String() != content {
		t.Errorf("expected %q, got %q", content, w.Body.String())
	}
}

func TestHandleCronLogs_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/crons/nonexistent/logs", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	err := srv.handleCronLogs(w, req)
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

func TestHandleCronLogs_InvalidMode(t *testing.T) {
	tmpDir := t.TempDir()
	setupCronLogDir(t, tmpDir)

	cronID := "abc-123"
	writeLogFile(t, config.GetCronLogFilepath(tmpDir, cronID), "content\n")

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/crons/"+cronID+"/logs?mode=invalid", nil)
	req.SetPathValue("id", cronID)
	w := httptest.NewRecorder()

	err := srv.handleCronLogs(w, req)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}

	httpErr, ok := err.(*httpError)
	if !ok {
		t.Fatalf("expected *httpError, got %T", err)
	}
	if httpErr.status != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", httpErr.status)
	}
}

func setupCronLogDir(t *testing.T, agencDirpath string) {
	t.Helper()
	cronLogDirpath := filepath.Join(agencDirpath, "logs", "crons")
	if err := os.MkdirAll(cronLogDirpath, 0755); err != nil {
		t.Fatalf("failed to create cron log dir: %v", err)
	}
}
```

Note: `writeLogFile` is already defined in `handle_server_logs_test.go` and available within the `server` package.

**Step 2: Run test to verify it fails**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: FAIL — `handleCronLogs` undefined.

**Step 3: Write the handler**

In `internal/server/handle_cron_logs.go`:

```go
package server

import (
	"net/http"
	"os"

	"github.com/odyssey/agenc/internal/config"
)

func (s *Server) handleCronLogs(w http.ResponseWriter, r *http.Request) error {
	cronID := r.PathValue("id")

	logFilepath := config.GetCronLogFilepath(s.agencDirpath, cronID)
	if _, err := os.Stat(logFilepath); os.IsNotExist(err) {
		return newHTTPError(http.StatusNotFound, "no logs found — cron may not have run yet")
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
```

**Step 4: Run test to verify it passes**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 5: Commit**

```
git add internal/server/handle_cron_logs.go internal/server/handle_cron_logs_test.go
git commit -m "Add GET /crons/{id}/logs handler to serve cron log content"
```

---

### Task 3: Register routes in server.go

**Files:**
- Modify: `internal/server/server.go:229-267` (inside `registerRoutes`)

**Step 1: Add the two new routes**

In `internal/server/server.go`, inside `registerRoutes`, add after the stash block (after line 260) and before the Claude-modifications block:

```go
	// Cron endpoints
	mux.Handle("GET /crons", appHandler(s.requestLogger, s.handleListCrons))
	mux.Handle("GET /crons/{id}/logs", appHandler(s.requestLogger, s.handleCronLogs))
```

**Step 2: Run tests to verify nothing broke**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```
git add internal/server/server.go
git commit -m "Register GET /crons and GET /crons/{id}/logs routes"
```

---

### Task 4: Client methods in client.go

**Files:**
- Modify: `internal/server/client.go` — add `ListCrons()` and `GetCronLogs()` methods

**Step 1: Add client methods**

Add after the `GetServerLogs` method (after line 471 in `client.go`):

```go
// ============================================================================
// High-level cron API methods
// ============================================================================

// ListCrons fetches the list of configured cron jobs from the server.
func (c *Client) ListCrons() ([]CronInfo, error) {
	var crons []CronInfo
	if err := c.Get("/crons", &crons); err != nil {
		return nil, err
	}
	return crons, nil
}

// GetCronLogs fetches log content for a cron job by ID.
// If all is true, returns the entire log file; otherwise returns the last 200 lines.
func (c *Client) GetCronLogs(cronID string, all bool) ([]byte, error) {
	path := "/crons/" + cronID + "/logs"
	if all {
		path += "?mode=all"
	}
	return c.GetRaw(path)
}
```

Note: `CronInfo` is defined in `handle_crons.go` in the same package, so it's directly available.

**Step 2: Run tests to verify nothing broke**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```
git add internal/server/client.go
git commit -m "Add ListCrons and GetCronLogs client methods"
```

---

### Task 5: CLI helper for cron name-or-ID resolution

**Files:**
- Modify: `cmd/mission_helpers.go` — add `resolveCronID` helper

**Step 1: Write the helper**

Add to `cmd/mission_helpers.go`:

```go
// resolveCronID resolves a cron name or UUID to a cron ID using the server API.
// It first tries to match the argument as a cron name. If no match is found,
// it treats the argument as a UUID directly.
func resolveCronID(client *server.Client, nameOrID string) (string, error) {
	crons, err := client.ListCrons()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to list crons")
	}

	for _, c := range crons {
		if c.Name == nameOrID {
			if c.ID == "" {
				return "", stacktrace.NewError("cron job '%s' has no ID — re-create it or add an 'id' field to config.yml", nameOrID)
			}
			return c.ID, nil
		}
	}

	// No name match — treat as UUID. Check if it matches any known cron ID.
	for _, c := range crons {
		if c.ID == nameOrID {
			return nameOrID, nil
		}
	}

	return "", stacktrace.NewError("cron job '%s' not found", nameOrID)
}
```

**Step 2: Run tests to verify nothing broke**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```
git add cmd/mission_helpers.go
git commit -m "Add resolveCronID helper for name-or-UUID resolution via server API"
```

---

### Task 6: CLI cron logs print command

**Files:**
- Create: `cmd/cron_logs.go`
- Create: `cmd/cron_logs_print.go`

**Step 1: Create the parent cron logs subcommand**

In `cmd/cron_logs.go`:

```go
package cmd

import "github.com/spf13/cobra"

var cronLogsCmd = &cobra.Command{
	Use:   logsCmdStr,
	Short: "View cron job logs",
}

func init() {
	cronCmd.AddCommand(cronLogsCmd)
}
```

**Step 2: Create the cron logs print command**

In `cmd/cron_logs_print.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var cronLogsPrintAllFlag bool

var cronLogsPrintCmd = &cobra.Command{
	Use:   printCmdStr + " <name-or-id>",
	Short: "Print cron job log content",
	Long: `Print the log output for a cron job.

The argument can be a cron name (as defined in config.yml) or a cron UUID.

By default, prints the last 200 lines. Use --all for the full file.

Examples:
  agenc cron logs print daily-report
  agenc cron logs print daily-report --all
  agenc cron logs print abc-123`,
	Args: cobra.ExactArgs(1),
	RunE: runCronLogsPrint,
}

func init() {
	cronLogsPrintCmd.Flags().BoolVar(&cronLogsPrintAllFlag, allFlagName, false, "print entire log file instead of last 200 lines")
	cronLogsCmd.AddCommand(cronLogsPrintCmd)
}

func runCronLogsPrint(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	cronID, err := resolveCronID(client, args[0])
	if err != nil {
		return err
	}

	body, err := client.GetCronLogs(cronID, cronLogsPrintAllFlag)
	if err != nil {
		return fmt.Errorf("failed to fetch cron logs: %w", err)
	}

	os.Stdout.Write(body)
	return nil
}
```

**Step 3: Run tests to verify nothing broke**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 4: Commit**

```
git add cmd/cron_logs.go cmd/cron_logs_print.go
git commit -m "Add 'agenc cron logs print' CLI command"
```

---

### Task 7: Update cron history to use server API

**Files:**
- Modify: `cmd/cron_history.go:35-62`

**Step 1: Update runCronHistory to use resolveCronID**

Replace the body of `runCronHistory` in `cmd/cron_history.go`:

```go
func runCronHistory(cmd *cobra.Command, args []string) error {
	nameOrID := args[0]

	client, err := serverClient()
	if err != nil {
		return err
	}

	cronID, err := resolveCronID(client, nameOrID)
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(true, "cron", cronID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Printf("No runs found for cron job '%s'\n", nameOrID)
		return nil
	}

	// Limit the number of entries
	displayMissions := missions
	if len(missions) > cronHistoryLimitFlag {
		displayMissions = missions[:cronHistoryLimitFlag]
	}

	fmt.Printf("Run history for cron job '%s':\n\n", nameOrID)

	tbl := tableprinter.NewTable("STARTED", "ID", "STATUS", "DURATION")

	for _, m := range displayMissions {
		status := getMissionStatus(m.ID, m.Status, m.ClaudeState)
		coloredStatus := colorizeStatus(status)

		started := m.CreatedAt.Local().Format("2006-01-02 15:04:05")

		var duration string
		if m.LastHeartbeat != nil {
			d := m.LastHeartbeat.Sub(m.CreatedAt)
			duration = formatMissionDuration(d)
		} else {
			duration = "--"
		}

		tbl.AddRow(started, m.ShortID, coloredStatus, duration)
	}

	tbl.Print()

	if len(missions) > cronHistoryLimitFlag {
		fmt.Printf("\n...showing %d of %d runs. Use --limit to see more.\n", cronHistoryLimitFlag, len(missions))
	}

	return nil
}
```

This removes the `readConfig()` call and `cfg.Crons[name]` lookup, replacing it with `resolveCronID`. The `config` import can be removed and the `tableprinter` import remains.

**Step 2: Remove the unused config import**

Remove `"github.com/odyssey/agenc/internal/config"` if it was imported (check — the current file doesn't import it, so this may be a no-op).

**Step 3: Run tests to verify nothing broke**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 4: Commit**

```
git add cmd/cron_history.go
git commit -m "Switch cron history from readConfig to server API for name resolution"
```

---

### Task 8: Final push

```
git pull --rebase
git push
```
