Rename Session Implementation Plan
===================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow users to rename their mission's tmux window via the command palette, independent of the Claude process.

**Architecture:** Add `agenc_custom_title` column to the sessions table, expose sessions via REST endpoints (`GET /sessions`, `PATCH /sessions/{id}`), create `mission rename` and `session rename` CLI commands, add a palette command, and update title reconciliation priority.

**Tech Stack:** Go, Cobra CLI, SQLite, HTTP server (net/http)

**Design doc:** `docs/plans/2026-03-01-rename-session-design.md`

---

Task 1: Database — add agenc_custom_title column
-------------------------------------------------

**Files:**
- Modify: `internal/database/migrations.go`
- Modify: `internal/database/database.go`
- Modify: `internal/database/sessions.go`
- Modify: `internal/database/sessions_test.go`

**Step 1: Write a failing test for the new column**

Add to `internal/database/sessions_test.go`:

```go
func TestUpdateSessionAgencCustomTitle(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	_, err = db.CreateSession(mission.ID, "s-rename-1")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Set agenc_custom_title
	if err := db.UpdateSessionAgencCustomTitle("s-rename-1", "My Custom Name"); err != nil {
		t.Fatalf("UpdateSessionAgencCustomTitle failed: %v", err)
	}

	got, err := db.GetSession("s-rename-1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.AgencCustomTitle != "My Custom Name" {
		t.Errorf("expected agenc_custom_title %q, got %q", "My Custom Name", got.AgencCustomTitle)
	}

	// Clear agenc_custom_title with empty string
	if err := db.UpdateSessionAgencCustomTitle("s-rename-1", ""); err != nil {
		t.Fatalf("UpdateSessionAgencCustomTitle (clear) failed: %v", err)
	}

	got, err = db.GetSession("s-rename-1")
	if err != nil {
		t.Fatalf("GetSession (after clear) failed: %v", err)
	}
	if got.AgencCustomTitle != "" {
		t.Errorf("expected empty agenc_custom_title after clear, got %q", got.AgencCustomTitle)
	}
}
```

**Step 2: Run the test to verify it fails**

Run: `make check` (via Makefile with `dangerouslyDisableSandbox: true`)
Expected: Compile error — `db.UpdateSessionAgencCustomTitle` and `got.AgencCustomTitle` don't exist yet.

**Step 3: Add migration SQL and run it**

In `internal/database/migrations.go`, add after the `createSessionsMissionIDIndexSQL` constant (line 39):

```go
addAgencCustomTitleColumnSQL = `ALTER TABLE sessions ADD COLUMN agenc_custom_title TEXT NOT NULL DEFAULT '';`
```

Add migration function:

```go
// migrateAddAgencCustomTitle idempotently adds the agenc_custom_title column
// to the sessions table for user-set window titles via the CLI.
func migrateAddAgencCustomTitle(conn *sql.DB) error {
	// Check sessions table columns
	rows, err := conn.Query("PRAGMA table_info(sessions)")
	if err != nil {
		return stacktrace.Propagate(err, "failed to read sessions table info")
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull int
		var dfltValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return stacktrace.Propagate(err, "failed to scan table_info row")
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return stacktrace.Propagate(err, "error iterating table_info rows")
	}

	if columns["agenc_custom_title"] {
		return nil
	}

	_, err = conn.Exec(addAgencCustomTitleColumnSQL)
	return err
}
```

In `internal/database/database.go`, add the migration call after `migrateDropLastActive` (after line 100):

```go
if err := migrateAddAgencCustomTitle(conn); err != nil {
	conn.Close()
	return nil, stacktrace.Propagate(err, "failed to add agenc_custom_title column to sessions")
}
```

**Step 4: Update Session struct and scan functions**

In `internal/database/sessions.go`:

Add `AgencCustomTitle string` to the Session struct (after `CustomTitle` on line 14):

```go
type Session struct {
	ID                string
	MissionID         string
	CustomTitle       string
	AgencCustomTitle  string
	AutoSummary       string
	LastScannedOffset int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}
```

Update all SELECT queries in the file to include `agenc_custom_title`. The column list pattern is:
`id, mission_id, custom_title, agenc_custom_title, auto_summary, last_scanned_offset, created_at, updated_at`

Update `scanSession` (line 113) to scan the new column:

```go
func scanSession(row *sql.Row) (*Session, error) {
	var s Session
	var createdAt, updatedAt string
	if err := row.Scan(&s.ID, &s.MissionID, &s.CustomTitle, &s.AgencCustomTitle, &s.AutoSummary, &s.LastScannedOffset, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &s, nil
}
```

Update `scanSessions` (line 125) similarly:

```go
if err := rows.Scan(&s.ID, &s.MissionID, &s.CustomTitle, &s.AgencCustomTitle, &s.AutoSummary, &s.LastScannedOffset, &createdAt, &updatedAt); err != nil {
```

Add the `UpdateSessionAgencCustomTitle` function:

```go
// UpdateSessionAgencCustomTitle sets the agenc_custom_title for a session.
// An empty title clears the custom title.
func (db *DB) UpdateSessionAgencCustomTitle(sessionID string, title string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"UPDATE sessions SET agenc_custom_title = ?, updated_at = ? WHERE id = ?",
		title, now, sessionID,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update agenc_custom_title for session '%s'", sessionID)
	}
	return nil
}
```

**Step 5: Run tests to verify they pass**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: All tests pass including `TestUpdateSessionAgencCustomTitle`.

**Step 6: Commit**

```
git add internal/database/
git commit -m "Add agenc_custom_title column to sessions table"
```

---

Task 2: Title reconciliation — insert agenc_custom_title at priority 2
-----------------------------------------------------------------------

**Files:**
- Modify: `internal/server/tmux.go`

**Step 1: Update determineBestTitle**

In `internal/server/tmux.go`, update `determineBestTitle` (line 47) to insert `AgencCustomTitle` between `CustomTitle` and `AutoSummary`:

```go
// determineBestTitle picks the best available title using the priority chain.
//
// Title priority (highest to lowest):
//  1. custom_title from /rename (Claude-set)
//  2. agenc_custom_title (user-set via CLI)
//  3. auto_summary
//  4. repo short name
//  5. mission short ID
func determineBestTitle(activeSession *database.Session, mission *database.Mission) string {
	// Priority 1: custom_title from /rename
	if activeSession != nil && activeSession.CustomTitle != "" {
		return activeSession.CustomTitle
	}

	// Priority 2: agenc_custom_title from user rename
	if activeSession != nil && activeSession.AgencCustomTitle != "" {
		return activeSession.AgencCustomTitle
	}

	// Priority 3: auto_summary
	if activeSession != nil && activeSession.AutoSummary != "" {
		return activeSession.AutoSummary
	}

	// Priority 4: repo short name
	if mission.GitRepo != "" {
		repoName := extractRepoShortName(mission.GitRepo)
		if repoName != "" {
			return repoName
		}
	}

	// Priority 5: mission short ID
	return mission.ShortID
}
```

Also update the comment block on `reconcileTmuxWindowTitle` (line 16-24) to list 5 priorities:

```go
// Title priority (highest to lowest):
//  1. Active session's custom_title (from /rename)
//  2. Active session's agenc_custom_title (user-set via CLI)
//  3. Active session's auto_summary (from Claude or AgenC summarizer)
//  4. Repo short name (from git_repo)
//  5. Mission short ID (fallback)
```

**Step 2: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: All tests pass.

**Step 3: Commit**

```
git add internal/server/tmux.go
git commit -m "Add agenc_custom_title to title reconciliation priority chain"
```

---

Task 3: Server — session HTTP endpoints
----------------------------------------

**Files:**
- Create: `internal/server/sessions.go`
- Modify: `internal/server/server.go` (register routes, lines 199-216)
- Modify: `internal/server/client.go` (add client methods)

**Step 1: Create session handler file**

Create `internal/server/sessions.go`:

```go
package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/odyssey/agenc/internal/database"
)

// SessionResponse is the JSON representation of a session returned by the API.
type SessionResponse struct {
	ID               string    `json:"id"`
	MissionID        string    `json:"mission_id"`
	CustomTitle      string    `json:"custom_title"`
	AgencCustomTitle string    `json:"agenc_custom_title"`
	AutoSummary      string    `json:"auto_summary"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func toSessionResponse(s *database.Session) SessionResponse {
	return SessionResponse{
		ID:               s.ID,
		MissionID:        s.MissionID,
		CustomTitle:      s.CustomTitle,
		AgencCustomTitle: s.AgencCustomTitle,
		AutoSummary:      s.AutoSummary,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

func toSessionResponses(sessions []*database.Session) []SessionResponse {
	result := make([]SessionResponse, len(sessions))
	for i, s := range sessions {
		result[i] = toSessionResponse(s)
	}
	return result
}

// handleListSessions handles GET /sessions?mission_id={id}.
// Returns sessions for a mission, ordered by updated_at descending.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) error {
	missionID := r.URL.Query().Get("mission_id")
	if missionID == "" {
		return newHTTPError(http.StatusBadRequest, "mission_id query parameter is required")
	}

	resolvedID, err := s.db.ResolveMissionID(missionID)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+missionID)
	}

	sessions, err := s.db.ListSessionsByMission(resolvedID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}

	writeJSON(w, http.StatusOK, toSessionResponses(sessions))
	return nil
}

// UpdateSessionRequest is the JSON body for PATCH /sessions/{id}.
type UpdateSessionRequest struct {
	AgencCustomTitle *string `json:"agenc_custom_title,omitempty"`
}

// handleUpdateSession handles PATCH /sessions/{id}.
// Updates session fields and triggers tmux window title reconciliation.
func (s *Server) handleUpdateSession(w http.ResponseWriter, r *http.Request) error {
	sessionID := r.PathValue("id")

	var req UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	// Look up the session to verify it exists and get its mission_id
	session, err := s.db.GetSession(sessionID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}
	if session == nil {
		return newHTTPError(http.StatusNotFound, "session not found: "+sessionID)
	}

	if req.AgencCustomTitle != nil {
		if err := s.db.UpdateSessionAgencCustomTitle(sessionID, *req.AgencCustomTitle); err != nil {
			return newHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	// Trigger tmux window title reconciliation for the owning mission
	s.reconcileTmuxWindowTitle(session.MissionID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	return nil
}
```

**Step 2: Register routes**

In `internal/server/server.go`, add these two lines inside `registerRoutes` before the repos catch-all (before line 215):

```go
mux.Handle("GET /sessions", appHandler(s.requestLogger, s.handleListSessions))
mux.Handle("PATCH /sessions/{id}", appHandler(s.requestLogger, s.handleUpdateSession))
```

**Step 3: Add client methods**

In `internal/server/client.go`, add after the `CreateMission` method (after line 253):

```go
// ListMissionSessions fetches all sessions for a mission.
func (c *Client) ListMissionSessions(missionID string) ([]*database.Session, error) {
	var responses []SessionResponse
	if err := c.Get("/sessions?mission_id="+missionID, &responses); err != nil {
		return nil, err
	}

	sessions := make([]*database.Session, len(responses))
	for i, r := range responses {
		sessions[i] = &database.Session{
			ID:               r.ID,
			MissionID:        r.MissionID,
			CustomTitle:      r.CustomTitle,
			AgencCustomTitle: r.AgencCustomTitle,
			AutoSummary:      r.AutoSummary,
			CreatedAt:        r.CreatedAt,
			UpdatedAt:        r.UpdatedAt,
		}
	}
	return sessions, nil
}

// UpdateSession updates fields on a session via the server.
func (c *Client) UpdateSession(sessionID string, req UpdateSessionRequest) error {
	return c.Patch("/sessions/"+sessionID, req, nil)
}
```

**Step 4: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: All tests pass (compiles clean, existing tests unaffected).

**Step 5: Commit**

```
git add internal/server/
git commit -m "Add session list and update HTTP endpoints"
```

---

Task 4: CLI — session rename command
--------------------------------------

**Files:**
- Create: `cmd/session_rename.go`
- Modify: `cmd/command_str_consts.go` (add `renameCmdStr`)

**Step 1: Add command string constant**

In `cmd/command_str_consts.go`, add `renameCmdStr` in the mission subcommands block (after `sendCmdStr` around line 58):

```go
renameCmdStr = "rename"
```

**Step 2: Create the session rename command**

Create `cmd/session_rename.go`:

```go
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/server"
)

var sessionRenameCmd = &cobra.Command{
	Use:   renameCmdStr + " <session-id> [title]",
	Short: "Rename a session's window title",
	Long: `Rename a session's window title.

Sets the agenc_custom_title on the session, which controls the tmux window name.
If no title is provided, prompts for input interactively.
An empty title clears the custom title, falling back to the auto-resolved title.

Example:
  agenc session rename 18749fb5-02ba-4b19-b989-4e18fbf8ea92 "My Feature Work"
  agenc session rename 18749fb5-02ba-4b19-b989-4e18fbf8ea92    # prompts for title`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runSessionRename,
}

func init() {
	sessionCmd.AddCommand(sessionRenameCmd)
}

func runSessionRename(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	sessionID := args[0]

	var title string
	if len(args) >= 2 {
		title = args[1]
	} else {
		title, err = promptForTitle()
		if err != nil {
			return err
		}
	}

	req := server.UpdateSessionRequest{
		AgencCustomTitle: &title,
	}
	if err := client.UpdateSession(sessionID, req); err != nil {
		return stacktrace.Propagate(err, "failed to rename session")
	}

	if title == "" {
		fmt.Println("Session title cleared.")
	} else {
		fmt.Printf("Session renamed to %q.\n", title)
	}
	return nil
}

// promptForTitle reads a title from stdin. Returns the trimmed input.
func promptForTitle() (string, error) {
	fmt.Print("New title (empty to clear): ")
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read input")
	}
	return strings.TrimSpace(line), nil
}
```

**Step 3: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: All tests pass.

**Step 4: Commit**

```
git add cmd/session_rename.go cmd/command_str_consts.go
git commit -m "Add session rename CLI command"
```

---

Task 5: CLI — mission rename command
--------------------------------------

**Files:**
- Create: `cmd/mission_rename.go`

**Step 1: Create the mission rename command**

Create `cmd/mission_rename.go`:

```go
package cmd

import (
	"fmt"
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/server"
)

var missionRenameCmd = &cobra.Command{
	Use:   renameCmdStr + " [mission-id] [title]",
	Short: "Rename the active session's window title for a mission",
	Long: `Rename the active session's window title for a mission.

This is a convenience command that resolves the mission's active session and
renames it. If no mission-id is provided, uses $AGENC_CALLING_MISSION_UUID.
If no title is provided, prompts for input interactively.

Example:
  agenc mission rename                          # uses env var, prompts for title
  agenc mission rename abc12345 "My Feature"    # explicit mission and title`,
	Args: cobra.RangeArgs(0, 2),
	RunE: runMissionRename,
}

func init() {
	missionCmd.AddCommand(missionRenameCmd)
}

func runMissionRename(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	// Resolve mission ID from args or env var
	var missionID string
	if len(args) >= 1 {
		missionID = args[0]
	} else {
		missionID = os.Getenv("AGENC_CALLING_MISSION_UUID")
	}
	if missionID == "" {
		return stacktrace.NewError("no mission ID provided; pass a mission ID or set $AGENC_CALLING_MISSION_UUID")
	}

	// Resolve the active session
	sessions, err := client.ListMissionSessions(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to list sessions for mission %s", missionID)
	}
	if len(sessions) == 0 {
		return stacktrace.NewError("no sessions found for mission %s", missionID)
	}
	activeSession := sessions[0] // already sorted by updated_at DESC

	// Get title from args or prompt
	var title string
	if len(args) >= 2 {
		title = args[1]
	} else {
		title, err = promptForTitle()
		if err != nil {
			return err
		}
	}

	req := server.UpdateSessionRequest{
		AgencCustomTitle: &title,
	}
	if err := client.UpdateSession(activeSession.ID, req); err != nil {
		return stacktrace.Propagate(err, "failed to rename session")
	}

	if title == "" {
		fmt.Println("Session title cleared.")
	} else {
		fmt.Printf("Session renamed to %q.\n", title)
	}
	return nil
}
```

**Step 2: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: All tests pass.

**Step 3: Commit**

```
git add cmd/mission_rename.go
git commit -m "Add mission rename CLI command"
```

---

Task 6: Palette command — add Rename Session
----------------------------------------------

**Files:**
- Modify: `internal/config/agenc_config.go`

**Step 1: Add builtin palette command**

In `internal/config/agenc_config.go`, add to `BuiltinPaletteCommands` map (after `copyMissionUuid` entry, around line 138):

```go
"renameSession": {
	Title:       "✨  Rename Session",
	Description: "Rename the focused mission's window",
	Command:     "agenc mission rename $AGENC_CALLING_MISSION_UUID",
},
```

Add `"renameSession"` to `builtinPaletteCommandOrder` slice (after `"copyMissionUuid"`, around line 191):

```go
"copyMissionUuid",
"renameSession",
"stopMission",
```

**Step 2: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: All tests pass. The palette uniqueness validation in `validatePaletteUniqueness` will automatically verify the new title and command don't conflict.

**Step 3: Commit**

```
git add internal/config/agenc_config.go
git commit -m "Add Rename Session builtin palette command"
```

---

Task 7: Update architecture docs
----------------------------------

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Update the architecture doc**

Add the two new routes (`GET /sessions`, `PATCH /sessions/{id}`) to the HTTP routes table in the architecture doc. Also note the `agenc_custom_title` column in the sessions table section if one exists.

Search the architecture doc for where routes are listed and add the session routes. Search for any sessions table documentation and add the new column.

**Step 2: Commit**

```
git add docs/system-architecture.md
git commit -m "Update architecture docs with session endpoints"
```

---

Task 8: Manual smoke test
--------------------------

**Step 1: Build**

Run: `make build` (with `dangerouslyDisableSandbox: true`)

**Step 2: Verify CLI commands exist**

```
./agenc session rename --help
./agenc mission rename --help
```

Expected: Help text displays for both commands.

**Step 3: Verify palette command appears**

```
./agenc config paletteCommand ls
```

Expected: "✨  Rename Session" appears in the list.

**Step 4: End-to-end test (if a mission is running)**

If a running mission is available, test the full flow:
1. `./agenc mission rename <mission-short-id> "Test Title"`
2. Verify the tmux window name changes
3. `./agenc mission rename <mission-short-id> ""` to clear
4. Verify it falls back to the previous title

**Step 5: Final commit and push**

Push all changes to remote.
