Send Keys Implementation Plan
=============================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `agenc mission send-keys` CLI command that forwards keystrokes to a running mission's tmux pane via the server.

**Architecture:** CLI -> Server HTTP -> tmux send-keys. Same pattern as attach/detach. No database changes.

**Tech Stack:** Go, Cobra CLI, tmux, go-isatty (already a dependency)

**Design doc:** `docs/plans/2026-03-05-send-keys-design.md`

---

Task 1: Add pool helper `sendKeysToPane()`
-------------------------------------------

**Files:**
- Modify: `internal/server/pool.go` (append to end)
- Test: `internal/server/pool_test.go` (create if absent, or append)

**Step 1: Write the failing test**

Create `internal/server/pool_test.go` (or append if it exists). The function
calls tmux which we can't easily unit test without a running tmux server, so we
test argument construction instead. Actually — `sendKeysToPane` is a thin
wrapper over `exec.Command`. Skip unit testing this function; it will be
covered by the server handler test and manual integration tests.

**Step 2: Write the implementation**

Append to `internal/server/pool.go`:

```go
// sendKeysToPane sends keystrokes to the given tmux pane by invoking
// tmux send-keys. Keys are passed as separate arguments — no shell
// interpolation. Uses tmux's native key name syntax (Enter, C-c, Escape, etc.).
func sendKeysToPane(paneID string, keys []string) error {
	paneTarget := "%" + paneID
	args := append([]string{"send-keys", "-t", paneTarget}, keys...)
	cmd := exec.Command("tmux", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.NewError("tmux send-keys failed: %v (output: %s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}
```

**Step 3: Verify it compiles**

Run: `go build ./internal/server/...`
Expected: success (no callers yet, but the function must compile)

**Step 4: Commit**

```
git add internal/server/pool.go
git commit -m "Add sendKeysToPane pool helper for tmux send-keys passthrough"
```

---

Task 2: Add server endpoint `POST /missions/{id}/send-keys`
------------------------------------------------------------

**Files:**
- Modify: `internal/server/missions.go` (append handler + request type after detach handler, ~line 705)
- Modify: `internal/server/server.go` (add route in `registerRoutes`, ~line 238)
- Modify: `internal/server/client.go` (add `SendKeys` client method after `DetachMission`)

**Step 1: Write the failing test**

Add to `internal/server/missions_test.go` (or create). The server tests use
the real HTTP test server. Look at existing test patterns in that file first —
if there are none, skip this step and rely on manual integration testing.

**Step 2: Add the request type and handler**

Append to `internal/server/missions.go` after `handleDetachMission` (~line 705):

```go
// SendKeysRequest is the JSON body for POST /missions/{id}/send-keys.
type SendKeysRequest struct {
	Keys []string `json:"keys"`
}

// handleSendKeys handles POST /missions/{id}/send-keys.
// Sends keystrokes to a running mission's tmux pane via tmux send-keys.
func (s *Server) handleSendKeys(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	var req SendKeysRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}
	if len(req.Keys) == 0 {
		return newHTTPError(http.StatusBadRequest, "keys is required and must not be empty")
	}

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	missionRecord, err := s.db.GetMission(resolvedID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}
	if missionRecord == nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	shortID := database.ShortID(resolvedID)

	if missionRecord.Status == "archived" {
		return newHTTPErrorf(http.StatusBadRequest,
			"cannot send keys to archived mission %s — unarchive it with: agenc mission unarchive %s",
			shortID, shortID)
	}
	if missionRecord.TmuxPane == nil || *missionRecord.TmuxPane == "" {
		return newHTTPErrorf(http.StatusBadRequest,
			"mission %s is not running — start it with: agenc mission attach %s",
			shortID, shortID)
	}

	paneID := *missionRecord.TmuxPane
	if !poolWindowExistsByPane(paneID) {
		return newHTTPErrorf(http.StatusInternalServerError,
			"mission %s has a stale pane reference — try: agenc mission reload %s",
			shortID, shortID)
	}

	if err := sendKeysToPane(paneID, req.Keys); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "%s", err.Error())
	}

	s.logger.Printf("Sent %d key(s) to mission %s (pane %s)", len(req.Keys), shortID, paneID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	return nil
}
```

**Step 3: Register the route**

In `internal/server/server.go` `registerRoutes()`, add after the detach route (~line 238):

```go
mux.Handle("POST /missions/{id}/send-keys", appHandler(s.requestLogger, s.handleSendKeys))
```

Note: NO `stashGuard` wrapper — send-keys is not a mission lifecycle mutation.

**Step 4: Add the client method**

In `internal/server/client.go`, add after `DetachMission`:

```go
// SendKeys sends keystrokes to a running mission's tmux pane.
func (c *Client) SendKeys(id string, keys []string) error {
	body := SendKeysRequest{Keys: keys}
	return c.Post("/missions/"+id+"/send-keys", body, nil)
}
```

**Step 5: Verify it compiles**

Run: `go build ./internal/server/...`
Expected: success

**Step 6: Commit**

```
git add internal/server/missions.go internal/server/server.go internal/server/client.go
git commit -m "Add POST /missions/{id}/send-keys server endpoint and client method"
```

---

Task 3: Add CLI command `agenc mission send-keys`
--------------------------------------------------

**Files:**
- Modify: `cmd/command_str_consts.go` (add `sendKeysCmdStr`)
- Create: `cmd/mission_send_keys.go`

**Step 1: Add the command constant**

In `cmd/command_str_consts.go`, add to the "Mission subcommands" section (~line 62):

```go
sendKeysCmdStr = "send-keys"
```

**Step 2: Create the CLI command file**

Create `cmd/mission_send_keys.go`:

```go
package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var missionSendKeysCmd = &cobra.Command{
	Use:   sendKeysCmdStr + " <mission-id> [keys...]",
	Short: "Send keystrokes to a running mission's tmux pane",
	Long: `Send keystrokes to a running mission's tmux pane via tmux send-keys.

Keys are passed through to tmux verbatim — use tmux key names for special keys:
  Enter, Escape, C-c, C-d, Space, Tab, Up, Down, Left, Right, etc.

Examples:
  agenc mission send-keys abc123 "hello world" Enter
  agenc mission send-keys abc123 C-c
  echo "fix the bug" | agenc mission send-keys abc123
  echo "fix the bug" | agenc mission send-keys abc123 Enter`,
	Args: cobra.MinimumNArgs(1),
	RunE: runMissionSendKeys,
}

func init() {
	missionCmd.AddCommand(missionSendKeysCmd)
}

func runMissionSendKeys(cmd *cobra.Command, args []string) error {
	missionIDInput := args[0]
	positionalKeys := args[1:]

	// Build the keys list: stdin content first (if piped), then positional args
	var keys []string

	if !isatty.IsTerminal(os.Stdin.Fd()) && !isatty.IsCygwinTerminal(os.Stdin.Fd()) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read stdin")
		}
		stdinContent := strings.TrimRight(string(data), "\n")
		if stdinContent != "" {
			keys = append(keys, stdinContent)
		}
	}

	keys = append(keys, positionalKeys...)

	if len(keys) == 0 {
		return stacktrace.NewError(
			"no keys provided — pass keys as arguments or pipe via stdin\n\n" +
				"Examples:\n" +
				"  %s %s %s abc123 \"hello world\" Enter\n" +
				"  echo \"hello\" | %s %s %s abc123",
			agencCmdStr, missionCmdStr, sendKeysCmdStr,
			agencCmdStr, missionCmdStr, sendKeysCmdStr,
		)
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	// Resolve mission ID (supports short IDs)
	missionID, err := client.ResolveMissionID(missionIDInput)
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve mission ID")
	}

	if err := client.SendKeys(missionID, keys); err != nil {
		return stacktrace.Propagate(err, "failed to send keys to mission %s", database.ShortID(missionID))
	}

	fmt.Printf("Sent %d key(s) to mission %s\n", len(keys), database.ShortID(missionID))
	return nil
}
```

**Step 3: Verify it compiles**

Run: `go build ./...`
Expected: success

**Step 4: Run existing tests**

Run: `go test ./cmd/... ./internal/server/...`
Expected: all existing tests pass (no regressions)

**Step 5: Commit**

```
git add cmd/command_str_consts.go cmd/mission_send_keys.go
git commit -m "Add agenc mission send-keys CLI command with stdin support"
```

---

Task 4: Build and manual integration test
------------------------------------------

**Files:**
- None modified — this is verification only

**Step 1: Build the binary**

Run: `make bin`
Expected: `./agenc` binary built successfully

**Step 2: Verify help text**

Run: `./agenc mission send-keys --help`
Expected: shows usage, long description, and examples

**Step 3: Test error cases**

Run: `./agenc mission send-keys`
Expected: error about minimum args

Run: `./agenc mission send-keys nonexistent "hello"`
Expected: error about mission not found (or server not running)

**Step 4: Test with a real mission (if server is running)**

These tests require a running AgenC server and a live mission. Skip if not
available — they'll be verified manually post-merge.

Run: `./agenc mission send-keys <real-mission-id> "hello" Enter`
Expected: 200 OK, text appears in mission's tmux pane

Run: `echo "test message" | ./agenc mission send-keys <real-mission-id> Enter`
Expected: 200 OK, piped text + Enter sent to pane

Run: `./agenc mission send-keys <real-mission-id> C-c`
Expected: 200 OK, Ctrl-C sent to pane

**Step 5: Test newline vs Enter**

Run: `printf "line1\nline2" | ./agenc mission send-keys <real-mission-id>`
Observe: how the pane receives newlines vs explicit Enter

Run: `./agenc mission send-keys <real-mission-id> "line1" Enter "line2" Enter`
Observe: compare behavior to above

Document findings in a comment on the design doc or in the commit message.

**Step 6: Final commit (docs update if needed)**

If the newline/Enter test reveals anything worth documenting, update the design
doc and commit.
