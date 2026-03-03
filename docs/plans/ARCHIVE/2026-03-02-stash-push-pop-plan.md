Stash Push/Pop Implementation Plan
====================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `agenc stash push`, `agenc stash pop`, and `agenc stash ls` commands that snapshot and restore the set of running missions and their tmux session links.

**Architecture:** Three server-side endpoints (`GET /stash`, `POST /stash/push`, `POST /stash/pop`) handle all orchestration. The CLI commands are thin wrappers that call these endpoints, with client-side fzf selection for pop and interactive confirmation for non-idle warnings during push. Stash files are stored as JSON at `$AGENC_DIRPATH/stash/`.

**Tech Stack:** Go stdlib `net/http`, Cobra CLI, existing `internal/server` and `internal/config` packages, existing `Resolve` fzf pattern.

---

### Task 1: Add config path helper and command string constants

**Files:**
- Modify: `internal/config/config.go`
- Modify: `cmd/command_str_consts.go`

**Step 1: Add `GetStashDirpath` to config.go**

In `internal/config/config.go`, add after the `GetServerRequestsLogFilepath` function:

```go
// GetStashDirpath returns the path to the stash directory.
func GetStashDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, StashDirname)
}
```

Add the constant `StashDirname = "stash"` alongside the existing directory name constants (near `ServerDirname`, `MissionsDirname`, etc.).

**Step 2: Add command string constants**

In `cmd/command_str_consts.go`, add to the top-level commands section:

```go
stashCmdStr = "stash"
```

Add to the subcommands section:

```go
pushCmdStr = "push"
popCmdStr  = "pop"
```

**Step 3: Commit**

```bash
git add internal/config/config.go cmd/command_str_consts.go
git commit -m "Add stash directory path helper and command constants"
```

---

### Task 2: Add `getLinkedPaneSessions()` to pool.go

**Files:**
- Modify: `internal/server/pool.go`

**Step 1: Add the function**

Add after the existing `getLinkedPaneIDs()` function:

```go
// getLinkedPaneSessions returns a map of pane IDs to the list of tmux session
// names they are linked into (excluding the agenc-pool session). Pane IDs are
// returned without the "%" prefix to match the database convention.
//
// If the tmux command fails, returns an empty map.
func getLinkedPaneSessions() map[string][]string {
	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{session_name} #{pane_id}")
	output, err := cmd.Output()
	if err != nil {
		return map[string][]string{}
	}

	// Collect which sessions each pane appears in (excluding pool)
	paneSessions := make(map[string]map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		sessionName := parts[0]
		if sessionName == poolSessionName {
			continue
		}
		paneID := strings.TrimPrefix(parts[1], "%")
		if paneSessions[paneID] == nil {
			paneSessions[paneID] = make(map[string]bool)
		}
		paneSessions[paneID][sessionName] = true
	}

	// Convert to sorted slices
	result := make(map[string][]string, len(paneSessions))
	for paneID, sessions := range paneSessions {
		names := make([]string, 0, len(sessions))
		for name := range sessions {
			names = append(names, name)
		}
		sort.Strings(names)
		result[paneID] = names
	}
	return result
}
```

Add `"sort"` to the import block.

**Step 2: Run tests**

Run: `make check` (via `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```bash
git add internal/server/pool.go
git commit -m "Add getLinkedPaneSessions for per-pane tmux session tracking"
```

---

### Task 3: Add server-side stash handlers

**Files:**
- Create: `internal/server/handle_stash.go`
- Modify: `internal/server/server.go`

**Step 1: Create the handler file**

Create `internal/server/handle_stash.go` with all three handlers and supporting types:

```go
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

// ============================================================================
// Stash file data structures
// ============================================================================

// StashFile is the JSON structure persisted to disk.
type StashFile struct {
	CreatedAt time.Time      `json:"created_at"`
	Missions  []StashMission `json:"missions"`
}

// StashMission records a single mission's state at time of stash.
type StashMission struct {
	MissionID      string   `json:"mission_id"`
	LinkedSessions []string `json:"linked_sessions"`
}

// ============================================================================
// API request/response types
// ============================================================================

// StashPushRequest is the JSON body for POST /stash/push.
type StashPushRequest struct {
	Force bool `json:"force"`
}

// StashPushResponse is the JSON response for a successful push.
type StashPushResponse struct {
	StashID          string `json:"stash_id"`
	MissionsStashed  int    `json:"missions_stashed"`
}

// NonIdleMissionInfo describes a non-idle mission for the 409 response.
type NonIdleMissionInfo struct {
	MissionID   string `json:"mission_id"`
	ShortID     string `json:"short_id"`
	ClaudeState string `json:"claude_state"`
	SessionName string `json:"session_name"`
}

// StashPushConflictResponse is the 409 response when non-idle missions exist.
type StashPushConflictResponse struct {
	NonIdleMissions []NonIdleMissionInfo `json:"non_idle_missions"`
}

// StashPopRequest is the JSON body for POST /stash/pop.
type StashPopRequest struct {
	StashID string `json:"stash_id"`
}

// StashPopResponse is the JSON response for a successful pop.
type StashPopResponse struct {
	MissionsRestored int `json:"missions_restored"`
}

// StashListEntry is a single entry in the GET /stash response.
type StashListEntry struct {
	StashID      string    `json:"stash_id"`
	CreatedAt    time.Time `json:"created_at"`
	MissionCount int       `json:"mission_count"`
}

// ============================================================================
// Handlers
// ============================================================================

// handleListStashes handles GET /stash.
func (s *Server) handleListStashes(w http.ResponseWriter, r *http.Request) error {
	stashDirpath := config.GetStashDirpath(s.agencDirpath)

	entries, err := os.ReadDir(stashDirpath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, []StashListEntry{})
			return nil
		}
		return newHTTPErrorf(http.StatusInternalServerError, "failed to read stash directory: %s", err.Error())
	}

	var result []StashListEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		stashFilepath := filepath.Join(stashDirpath, entry.Name())
		stashFile, err := readStashFile(stashFilepath)
		if err != nil {
			s.logger.Printf("Warning: skipping unreadable stash file %s: %v", entry.Name(), err)
			continue
		}

		stashID := strings.TrimSuffix(entry.Name(), ".json")
		result = append(result, StashListEntry{
			StashID:      stashID,
			CreatedAt:    stashFile.CreatedAt,
			MissionCount: len(stashFile.Missions),
		})
	}

	// Sort by creation time, most recent first
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	writeJSON(w, http.StatusOK, result)
	return nil
}

// handlePushStash handles POST /stash/push.
func (s *Server) handlePushStash(w http.ResponseWriter, r *http.Request) error {
	var req StashPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	// List all active missions and enrich with ClaudeState
	missions, err := s.db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to list missions: %s", err.Error())
	}

	// Filter to running missions only (wrapper PID alive)
	var runningMissions []*database.Mission
	for _, m := range missions {
		pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, m.ID)
		pid, pidErr := ReadPID(pidFilepath)
		if pidErr == nil && pid != 0 && IsProcessRunning(pid) {
			runningMissions = append(runningMissions, m)
		}
	}

	if len(runningMissions) == 0 {
		writeJSON(w, http.StatusOK, StashPushResponse{MissionsStashed: 0})
		return nil
	}

	// Enrich with ClaudeState concurrently
	responses := toMissionResponses(runningMissions)
	var wg sync.WaitGroup
	for i := range responses {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.enrichMissionResponse(&responses[idx])
		}(i)
	}
	wg.Wait()

	// Check for non-idle missions
	if !req.Force {
		var nonIdle []NonIdleMissionInfo
		for _, resp := range responses {
			if resp.ClaudeState != nil && *resp.ClaudeState != "idle" {
				s.enrichMissionWithSessionTitle(resp.ToMission())
				nonIdle = append(nonIdle, NonIdleMissionInfo{
					MissionID:   resp.ID,
					ShortID:     resp.ShortID,
					ClaudeState: *resp.ClaudeState,
					SessionName: resolveSessionTitle(func() *database.Session {
						session, _ := s.db.GetActiveSession(resp.ID)
						return session
					}()),
				})
			}
		}
		if len(nonIdle) > 0 {
			writeJSON(w, http.StatusConflict, StashPushConflictResponse{NonIdleMissions: nonIdle})
			return nil
		}
	}

	// Build per-pane session map
	paneSessions := getLinkedPaneSessions()

	// Build stash data
	now := time.Now().UTC()
	stashID := now.Format("2006-01-02T15-04-05")

	var stashMissions []StashMission
	for _, resp := range responses {
		var linkedSessions []string
		if resp.TmuxPane != nil && *resp.TmuxPane != "" {
			linkedSessions = paneSessions[*resp.TmuxPane]
		}
		stashMissions = append(stashMissions, StashMission{
			MissionID:      resp.ID,
			LinkedSessions: linkedSessions,
		})
	}

	stashFile := StashFile{
		CreatedAt: now,
		Missions:  stashMissions,
	}

	// Write stash file before stopping (so partial failures still have a record)
	if err := writeStashFile(s.agencDirpath, stashID, &stashFile); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to write stash file: %s", err.Error())
	}

	// Stop all running missions
	for _, resp := range responses {
		if err := s.stopWrapper(resp.ID); err != nil {
			s.logger.Printf("Warning: failed to stop wrapper for mission %s: %v", resp.ShortID, err)
		}
		s.destroyPoolWindow(resp.ID)
	}

	s.logger.Printf("Stashed %d missions as %s", len(stashMissions), stashID)
	writeJSON(w, http.StatusOK, StashPushResponse{
		StashID:         stashID,
		MissionsStashed: len(stashMissions),
	})
	return nil
}

// handlePopStash handles POST /stash/pop.
func (s *Server) handlePopStash(w http.ResponseWriter, r *http.Request) error {
	var req StashPopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	stashDirpath := config.GetStashDirpath(s.agencDirpath)

	// Resolve stash ID (use most recent if not specified)
	stashID := req.StashID
	if stashID == "" {
		mostRecent, err := findMostRecentStash(stashDirpath)
		if err != nil {
			return newHTTPErrorf(http.StatusNotFound, "no stash files found")
		}
		stashID = mostRecent
	}

	stashFilepath := filepath.Join(stashDirpath, stashID+".json")
	stashFile, err := readStashFile(stashFilepath)
	if err != nil {
		return newHTTPErrorf(http.StatusNotFound, "stash not found: %s", stashID)
	}

	// Restore each mission
	restored := 0
	for _, sm := range stashFile.Missions {
		// Verify mission still exists
		mission, err := s.db.GetMission(sm.MissionID)
		if err != nil || mission == nil {
			s.logger.Printf("Warning: stashed mission %s no longer exists, skipping", database.ShortID(sm.MissionID))
			continue
		}

		if mission.Status == "archived" {
			s.logger.Printf("Warning: stashed mission %s is archived, skipping", database.ShortID(sm.MissionID))
			continue
		}

		// Spawn wrapper in pool
		if err := s.ensureWrapperInPool(mission); err != nil {
			s.logger.Printf("Warning: failed to start wrapper for mission %s: %v", database.ShortID(sm.MissionID), err)
			continue
		}

		// Re-link into saved tmux sessions
		shortID := database.ShortID(sm.MissionID)
		poolWindowTarget := fmt.Sprintf("%s:%s", poolSessionName, shortID)
		for _, sessionName := range sm.LinkedSessions {
			// Create tmux session if it doesn't exist
			if !tmuxSessionExists(sessionName) {
				createCmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName)
				if output, err := createCmd.CombinedOutput(); err != nil {
					s.logger.Printf("Warning: failed to create tmux session %s: %v (output: %s)", sessionName, err, string(output))
					continue
				}
				s.logger.Printf("Created tmux session %s for stash restore", sessionName)
			}

			if err := linkPoolWindow(poolWindowTarget, sessionName); err != nil {
				s.logger.Printf("Warning: failed to link mission %s to session %s: %v", shortID, sessionName, err)
			}
		}

		s.reconcileTmuxWindowTitle(sm.MissionID)
		restored++
	}

	// Delete stash file on success
	if err := os.Remove(stashFilepath); err != nil {
		s.logger.Printf("Warning: failed to delete stash file %s: %v", stashFilepath, err)
	}

	s.logger.Printf("Restored %d missions from stash %s", restored, stashID)
	writeJSON(w, http.StatusOK, StashPopResponse{MissionsRestored: restored})
	return nil
}

// ============================================================================
// Helpers
// ============================================================================

// tmuxSessionExists checks whether a named tmux session exists.
func tmuxSessionExists(sessionName string) bool {
	return exec.Command("tmux", "has-session", "-t", sessionName).Run() == nil
}

// readStashFile reads and parses a stash JSON file.
func readStashFile(filepath string) (*StashFile, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}
	var stash StashFile
	if err := json.Unmarshal(data, &stash); err != nil {
		return nil, err
	}
	return &stash, nil
}

// writeStashFile creates the stash directory if needed and writes the stash file.
func writeStashFile(agencDirpath string, stashID string, stash *StashFile) error {
	stashDirpath := config.GetStashDirpath(agencDirpath)
	if err := os.MkdirAll(stashDirpath, 0755); err != nil {
		return fmt.Errorf("failed to create stash directory: %w", err)
	}

	data, err := json.MarshalIndent(stash, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal stash: %w", err)
	}

	stashFilepath := filepath.Join(stashDirpath, stashID+".json")
	if err := os.WriteFile(stashFilepath, data, 0644); err != nil {
		return fmt.Errorf("failed to write stash file: %w", err)
	}
	return nil
}

// findMostRecentStash returns the stash ID of the most recently created stash file.
func findMostRecentStash(stashDirpath string) (string, error) {
	entries, err := os.ReadDir(stashDirpath)
	if err != nil {
		return "", err
	}

	var newest string
	var newestTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if newest == "" || info.ModTime().After(newestTime) {
			newest = strings.TrimSuffix(entry.Name(), ".json")
			newestTime = info.ModTime()
		}
	}

	if newest == "" {
		return "", fmt.Errorf("no stash files found")
	}
	return newest, nil
}
```

Note: `tmuxSessionExists` in this file is a package-level function in `internal/server`.
There is already a `tmuxSessionExists` in `cmd/tmux_helpers.go` (different package), so
no conflict.

**Step 2: Register routes in server.go**

In `internal/server/server.go`, in `registerRoutes`, add after the existing route
registrations:

```go
mux.Handle("GET /stash", appHandler(s.requestLogger, s.handleListStashes))
mux.Handle("POST /stash/push", appHandler(s.requestLogger, s.handlePushStash))
mux.Handle("POST /stash/pop", appHandler(s.requestLogger, s.handlePopStash))
```

**Step 3: Run tests**

Run: `make check` (via `dangerouslyDisableSandbox: true`)
Expected: PASS (no new tests yet, just compilation)

**Step 4: Commit**

```bash
git add internal/server/handle_stash.go internal/server/server.go
git commit -m "Add server-side stash push/pop/list handlers"
```

---

### Task 4: Add client methods

**Files:**
- Modify: `internal/server/client.go`

**Step 1: Add stash client methods**

Add at the end of the file, in a new section:

```go
// ============================================================================
// High-level stash API methods
// ============================================================================

// ListStashes fetches available stash files from the server.
func (c *Client) ListStashes() ([]StashListEntry, error) {
	var entries []StashListEntry
	if err := c.Get("/stash", &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// PushStash snapshots and stops all running missions.
// Returns (response, nonIdleMissions, error).
// If non-idle missions exist and force is false, response is nil and
// nonIdleMissions contains the conflicting missions.
func (c *Client) PushStash(force bool) (*StashPushResponse, *StashPushConflictResponse, error) {
	body := StashPushRequest{Force: force}

	pr, pw := io.Pipe()
	go func() {
		pw.CloseWithError(json.NewEncoder(pw).Encode(body))
	}()

	resp, err := c.httpClient.Post(c.baseURL+"/stash/push", "application/json", pr)
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "failed to connect to server")
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		var conflict StashPushConflictResponse
		if err := json.NewDecoder(resp.Body).Decode(&conflict); err != nil {
			return nil, nil, stacktrace.Propagate(err, "failed to decode conflict response")
		}
		return nil, &conflict, nil
	}

	if resp.StatusCode >= 400 {
		return nil, nil, c.decodeError(resp)
	}

	var pushResp StashPushResponse
	if err := json.NewDecoder(resp.Body).Decode(&pushResp); err != nil {
		return nil, nil, stacktrace.Propagate(err, "failed to decode push response")
	}
	return &pushResp, nil, nil
}

// PopStash restores missions from a stash. If stashID is empty, pops the
// most recent stash.
func (c *Client) PopStash(stashID string) (*StashPopResponse, error) {
	body := StashPopRequest{StashID: stashID}
	var resp StashPopResponse
	if err := c.Post("/stash/pop", body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
```

Add `"net/http"` to the import block (it may already be imported via the existing `decodeError` method — check).

**Step 2: Commit**

```bash
git add internal/server/client.go
git commit -m "Add stash push/pop/list client methods"
```

---

### Task 5: Add CLI commands

**Files:**
- Create: `cmd/stash.go`
- Create: `cmd/stash_push.go`
- Create: `cmd/stash_pop.go`
- Create: `cmd/stash_ls.go`

**Step 1: Create the parent `stash` command**

Create `cmd/stash.go`:

```go
package cmd

import "github.com/spf13/cobra"

var stashCmd = &cobra.Command{
	Use:   stashCmdStr,
	Short: "Snapshot and restore running missions",
}

func init() {
	rootCmd.AddCommand(stashCmd)
}
```

**Step 2: Create `stash push`**

Create `cmd/stash_push.go`:

```go
package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/rodaine/table"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/tableprinter"
)

var stashPushForceFlag bool

var stashPushCmd = &cobra.Command{
	Use:   pushCmdStr,
	Short: "Snapshot running missions and stop them",
	Long: `Snapshot all running missions — recording which tmux sessions they are
linked into — then stop them. The snapshot is saved to a stash file that
can be restored later with 'agenc stash pop'.

If any missions are actively busy (not idle), you will be warned before
proceeding. Use --force to skip the warning.`,
	RunE: runStashPush,
}

func init() {
	stashPushCmd.Flags().BoolVar(&stashPushForceFlag, forceFlagName, false, "skip warning for non-idle missions")
	stashCmd.AddCommand(stashPushCmd)
}

func runStashPush(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	// First attempt without force
	pushResp, conflict, err := client.PushStash(stashPushForceFlag)
	if err != nil {
		return stacktrace.Propagate(err, "failed to push stash")
	}

	// Handle non-idle warning
	if conflict != nil {
		fmt.Println("The following missions are not idle:")
		fmt.Println()

		tbl := tableprinter.NewTable("ID", "STATE", "SESSION")
		for _, m := range conflict.NonIdleMissions {
			tbl.AddRow(m.ShortID, m.ClaudeState, truncatePrompt(m.SessionName, 60))
		}
		tbl.Print()

		fmt.Println()
		fmt.Print("Stashing will stop these missions. Continue? [y/N] ")

		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}

		// Retry with force
		pushResp, _, err = client.PushStash(true)
		if err != nil {
			return stacktrace.Propagate(err, "failed to push stash")
		}
	}

	if pushResp.MissionsStashed == 0 {
		fmt.Println("No running missions to stash.")
	} else {
		fmt.Printf("Stashed %d mission(s). Restore with '%s %s %s'.\n",
			pushResp.MissionsStashed, agencCmdStr, stashCmdStr, popCmdStr)
	}
	return nil
}
```

**Step 3: Create `stash pop`**

Create `cmd/stash_pop.go`:

```go
package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/server"
)

var stashPopCmd = &cobra.Command{
	Use:   popCmdStr,
	Short: "Restore missions from a stash",
	Long: `Restore all missions from a previously saved stash. Missions are
re-started and their windows are linked back into the tmux sessions
they were in at the time of the stash.

If there is only one stash, it is restored automatically. If multiple
stashes exist, an interactive picker is shown.`,
	RunE: runStashPop,
}

func init() {
	stashCmd.AddCommand(stashPopCmd)
}

type stashPickerEntry struct {
	StashID      string
	CreatedAt    string
	MissionCount string
}

func runStashPop(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	stashes, err := client.ListStashes()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list stashes")
	}

	if len(stashes) == 0 {
		fmt.Println("No stashed workspaces.")
		return nil
	}

	// If only one stash, use it directly
	var selectedStashID string
	if len(stashes) == 1 {
		selectedStashID = stashes[0].StashID
	} else {
		// Build picker entries
		entries := make([]stashPickerEntry, len(stashes))
		for i, s := range stashes {
			entries[i] = stashPickerEntry{
				StashID:      s.StashID,
				CreatedAt:    s.CreatedAt.Local().Format("2006-01-02 15:04:05"),
				MissionCount: fmt.Sprintf("%d missions", s.MissionCount),
			}
		}

		result, err := Resolve("", Resolver[stashPickerEntry]{
			TryCanonical: func(input string) (stashPickerEntry, bool, error) {
				return stashPickerEntry{}, false, nil
			},
			GetItems: func() ([]stashPickerEntry, error) { return entries, nil },
			FormatRow: func(e stashPickerEntry) []string {
				return []string{e.CreatedAt, e.StashID, e.MissionCount}
			},
			FzfPrompt:         "Select stash to restore: ",
			FzfHeaders:        []string{"CREATED", "ID", "MISSIONS"},
			MultiSelect:       false,
			NotCanonicalError: "not a valid stash ID",
		})
		if err != nil {
			return err
		}

		if result.WasCancelled || len(result.Items) == 0 {
			return nil
		}
		selectedStashID = result.Items[0].StashID
	}

	popResp, err := client.PopStash(selectedStashID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to pop stash")
	}

	fmt.Printf("Restored %d mission(s).\n", popResp.MissionsRestored)
	return nil
}
```

**Step 4: Create `stash ls`**

Create `cmd/stash_ls.go`:

```go
package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/tableprinter"
)

var stashLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List saved stashes",
	RunE:  runStashLs,
}

func init() {
	stashCmd.AddCommand(stashLsCmd)
}

func runStashLs(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	stashes, err := client.ListStashes()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list stashes")
	}

	if len(stashes) == 0 {
		fmt.Println("No stashed workspaces.")
		return nil
	}

	tbl := tableprinter.NewTable("CREATED", "ID", "MISSIONS")
	for _, s := range stashes {
		tbl.AddRow(
			s.CreatedAt.Local().Format("2006-01-02 15:04:05"),
			s.StashID,
			fmt.Sprintf("%d", s.MissionCount),
		)
	}
	tbl.Print()
	return nil
}
```

**Step 5: Run build**

Run: `make build` (via `dangerouslyDisableSandbox: true`)
Expected: Clean compilation.

**Step 6: Commit**

```bash
git add cmd/stash.go cmd/stash_push.go cmd/stash_pop.go cmd/stash_ls.go
git commit -m "Add stash push/pop/ls CLI commands"
```

---

### Task 6: Add handler tests

**Files:**
- Create: `internal/server/handle_stash_test.go`

**Step 1: Write tests**

Create `internal/server/handle_stash_test.go` with tests covering:

- `TestHandleListStashes_EmptyDir` — no stash directory exists, returns `[]`
- `TestHandleListStashes_WithFiles` — multiple stash files, returned sorted by recency
- `TestHandlePushStash_NoRunningMissions` — returns `missions_stashed: 0`
- `TestHandlePopStash_NotFound` — returns 404 for nonexistent stash ID
- `TestReadWriteStashFile` — round-trip write+read of stash file
- `TestFindMostRecentStash` — correctly identifies newest file

Use `t.TempDir()` for isolation. For handler tests that need the database, follow the
existing test setup patterns (check if there are existing server handler tests, or test
the exported helper functions directly).

**Step 2: Run tests**

Run: `make check` (via `dangerouslyDisableSandbox: true`)
Expected: ALL PASS

**Step 3: Commit**

```bash
git add internal/server/handle_stash_test.go
git commit -m "Add stash handler tests"
```

---

### Task 7: Update architecture doc

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Add stash directory to the directory layout**

In the directory layout section, add `stash/` as a peer to `server/`, `missions/`, etc.:

```
stash/                    Timestamped workspace snapshots (stash push/pop)
```

**Step 2: Add endpoints to the route list**

Add to the HTTP routes section:

```
GET  /stash               List saved stash files
POST /stash/push          Snapshot running missions and stop them
POST /stash/pop           Restore missions from a stash file
```

**Step 3: Commit**

```bash
git add docs/system-architecture.md
git commit -m "Document stash directory and endpoints in architecture doc"
```

---

### Task 8: Manual smoke test

**Step 1: Build**

Run: `make build`

**Step 2: Test push with no running missions**

```bash
./agenc stash push
```

Expected: "No running missions to stash."

**Step 3: Test push with running missions**

1. Start some missions: `./agenc mission new <repo>`
2. Run `./agenc stash push`
3. Verify missions are stopped: `./agenc mission ls`
4. Verify stash file exists: `./agenc stash ls`

**Step 4: Test pop**

```bash
./agenc stash pop
```

Verify missions are restored and linked back into tmux sessions.

**Step 5: Test ls**

```bash
./agenc stash ls
```

Verify table output with stash metadata.
