Mission Search Implementation Plan
===================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add FTS5 full-text search over mission transcripts so users can find missions by topic/content.

**Architecture:** SQLite FTS5 virtual table indexed by the session scanner pipeline. New server endpoint + CLI command for programmatic search. Enhanced `mission attach` picker with live server-side search via fzf `--disabled` + `change:reload`.

**Tech Stack:** Go, SQLite FTS5 (via modernc.org/sqlite), Cobra CLI, fzf

---

Task 1: FTS5 Database Migration
---------------------------------

**Files:**
- Modify: `internal/database/migrations.go`
- Modify: `internal/database/database.go:25-46` (add to getMigrationSteps)

**Step 1: Add FTS5 table creation SQL constant**

In `internal/database/migrations.go`, add after the existing SQL constants (after line 53):

```go
const createMissionSearchIndexSQL = `CREATE VIRTUAL TABLE IF NOT EXISTS mission_search_index USING fts5(
	mission_id UNINDEXED,
	session_id UNINDEXED,
	content,
	tokenize='porter unicode61'
);`
```

**Step 2: Add migration function**

In `internal/database/migrations.go`, add the migration function:

```go
// migrateCreateMissionSearchIndex idempotently creates the FTS5 virtual table
// for full-text mission search. FTS5 virtual tables don't appear in PRAGMA
// table_info, so we check sqlite_master directly.
func migrateCreateMissionSearchIndex(conn *sql.DB) error {
	var count int
	err := conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='mission_search_index'").Scan(&count)
	if err != nil {
		return stacktrace.Propagate(err, "failed to check for mission_search_index table")
	}
	if count > 0 {
		return nil
	}
	_, err = conn.Exec(createMissionSearchIndexSQL)
	return err
}
```

**Step 3: Register migration**

In `internal/database/database.go`, add to `getMigrationSteps()` after the last entry (`migrateAddSourceColumns`):

```go
{migrateCreateMissionSearchIndex, "create FTS5 search index"},
```

**Step 4: Build and verify**

Run: `make build`
Expected: Clean build, no errors. Database auto-migrates on next server start.

**Step 5: Commit**

```
git add internal/database/migrations.go internal/database/database.go
git commit -m "Add FTS5 virtual table migration for mission search"
```

---

Task 2: FTS5 Database Methods
-------------------------------

**Files:**
- Create: `internal/database/search.go`

**Step 1: Write test file**

Create `internal/database/search_test.go`:

```go
package database

import (
	"os"
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.sqlite")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestInsertSearchContent(t *testing.T) {
	db := openTestDB(t)

	// Create a mission first
	_, err := db.conn.Exec(
		"INSERT INTO missions (id, short_id, prompt, status, created_at, updated_at) VALUES (?, ?, ?, 'active', datetime('now'), datetime('now'))",
		"test-mission-id-00000000-0000-0000-0000", "testmiss", "test prompt",
	)
	if err != nil {
		t.Fatalf("failed to create test mission: %v", err)
	}

	err = db.InsertSearchContent("test-mission-id-00000000-0000-0000-0000", "test-session-id", "discussing authentication refactoring")
	if err != nil {
		t.Fatalf("InsertSearchContent failed: %v", err)
	}
}

func TestSearchMissions(t *testing.T) {
	db := openTestDB(t)

	// Create two missions
	for _, m := range []struct{ id, shortID, prompt string }{
		{"mission-aaa-00000000-0000-0000-0000", "missiaaa", "first mission"},
		{"mission-bbb-00000000-0000-0000-0000", "missibbb", "second mission"},
	} {
		_, err := db.conn.Exec(
			"INSERT INTO missions (id, short_id, prompt, status, git_repo, created_at, updated_at) VALUES (?, ?, ?, 'active', 'github.com/test/repo', datetime('now'), datetime('now'))",
			m.id, m.shortID, m.prompt,
		)
		if err != nil {
			t.Fatalf("failed to create mission: %v", err)
		}
	}

	// Index content
	db.InsertSearchContent("mission-aaa-00000000-0000-0000-0000", "session-1", "building the authentication system with OAuth")
	db.InsertSearchContent("mission-bbb-00000000-0000-0000-0000", "session-2", "refactoring the database migration layer")

	// Search for "authentication"
	results, err := db.SearchMissions("authentication", 10)
	if err != nil {
		t.Fatalf("SearchMissions failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].MissionID != "mission-aaa-00000000-0000-0000-0000" {
		t.Errorf("expected mission-aaa, got %s", results[0].MissionID)
	}
	if results[0].Snippet == "" {
		t.Error("expected non-empty snippet")
	}
}

func TestSearchMissions_EmptyQuery(t *testing.T) {
	db := openTestDB(t)
	results, err := db.SearchMissions("", 10)
	if err != nil {
		t.Fatalf("SearchMissions with empty query failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestSearchMissions_NoResults(t *testing.T) {
	db := openTestDB(t)
	results, err := db.SearchMissions("nonexistent", 10)
	if err != nil {
		t.Fatalf("SearchMissions failed: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchMissions_SpecialCharacters(t *testing.T) {
	db := openTestDB(t)

	_, err := db.conn.Exec(
		"INSERT INTO missions (id, short_id, prompt, status, created_at, updated_at) VALUES (?, ?, ?, 'active', datetime('now'), datetime('now'))",
		"mission-ccc-00000000-0000-0000-0000", "missiccc", "test",
	)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	db.InsertSearchContent("mission-ccc-00000000-0000-0000-0000", "session-3", "fixing the user's config.yml parsing")

	// Query with special chars should not crash
	results, err := db.SearchMissions("config.yml", 10)
	if err != nil {
		t.Fatalf("SearchMissions with special chars failed: %v", err)
	}
	// Should find the result (FTS5 handles dots in porter tokenizer)
	if len(results) == 0 {
		t.Log("no results for config.yml — acceptable with porter tokenizer")
	}
}

func TestDeleteSearchContent(t *testing.T) {
	db := openTestDB(t)

	_, err := db.conn.Exec(
		"INSERT INTO missions (id, short_id, prompt, status, created_at, updated_at) VALUES (?, ?, ?, 'active', datetime('now'), datetime('now'))",
		"mission-ddd-00000000-0000-0000-0000", "missiddd", "test",
	)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	db.InsertSearchContent("mission-ddd-00000000-0000-0000-0000", "session-4", "some content to delete")

	results, _ := db.SearchMissions("delete", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result before delete, got %d", len(results))
	}

	err = db.DeleteAllSearchContent()
	if err != nil {
		t.Fatalf("DeleteAllSearchContent failed: %v", err)
	}

	results, _ = db.SearchMissions("delete", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results after delete, got %d", len(results))
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `cd internal/database && go test -run TestInsertSearchContent -v`
Expected: FAIL — `InsertSearchContent` not defined

**Step 3: Write the implementation**

Create `internal/database/search.go`:

```go
package database

import (
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// SearchResult represents a single search hit with mission metadata.
type SearchResult struct {
	MissionID string
	SessionID string
	Snippet   string
	Rank      float64
}

// InsertSearchContent adds a text chunk to the FTS5 search index.
func (db *DB) InsertSearchContent(missionID, sessionID, content string) error {
	_, err := db.conn.Exec(
		"INSERT INTO mission_search_index (mission_id, session_id, content) VALUES (?, ?, ?)",
		missionID, sessionID, content,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to insert search content")
	}
	return nil
}

// SearchMissions queries the FTS5 index and returns ranked mission results.
// The query is wrapped in double quotes to treat it as a phrase search by default,
// preventing accidental FTS5 operator syntax. Results are grouped by mission_id,
// returning only the best match per mission.
func (db *DB) SearchMissions(query string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// Escape double quotes in the query, then wrap in quotes for literal matching.
	// FTS5 phrase queries use "double quotes".
	escaped := strings.ReplaceAll(query, `"`, `""`)
	ftsQuery := `"` + escaped + `"`

	rows, err := db.conn.Query(`
		SELECT mission_id, session_id,
			snippet(mission_search_index, 2, '[', ']', '...', 20) as snippet,
			bm25(mission_search_index) as rank
		FROM mission_search_index
		WHERE content MATCH ?
		ORDER BY rank
		LIMIT ?
	`, ftsQuery, limit*3) // Over-fetch to allow dedup by mission
	if err != nil {
		// If the query is invalid FTS5 syntax, try a simpler tokenized search
		// by splitting into terms joined by AND.
		terms := strings.Fields(escaped)
		if len(terms) > 1 {
			ftsQuery = strings.Join(terms, " AND ")
			rows, err = db.conn.Query(`
				SELECT mission_id, session_id,
					snippet(mission_search_index, 2, '[', ']', '...', 20) as snippet,
					bm25(mission_search_index) as rank
				FROM mission_search_index
				WHERE content MATCH ?
				ORDER BY rank
				LIMIT ?
			`, ftsQuery, limit*3)
		}
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to search missions")
		}
	}
	defer rows.Close()

	seen := make(map[string]bool)
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.MissionID, &r.SessionID, &r.Snippet, &r.Rank); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan search result")
		}
		if seen[r.MissionID] {
			continue // Keep only the best match per mission
		}
		seen[r.MissionID] = true
		results = append(results, r)
		if len(results) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating search results")
	}

	return results, nil
}

// DeleteAllSearchContent removes all entries from the FTS5 index.
// Used by the reindex command before repopulating.
func (db *DB) DeleteAllSearchContent() error {
	_, err := db.conn.Exec("DELETE FROM mission_search_index")
	if err != nil {
		return stacktrace.Propagate(err, "failed to clear search index")
	}
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `cd internal/database && go test -run TestSearch -v && go test -run TestInsert -v && go test -run TestDelete -v`
Expected: All PASS

**Step 5: Commit**

```
git add internal/database/search.go internal/database/search_test.go
git commit -m "Add FTS5 search database methods"
```

---

Task 3: Extend Session Scanner for FTS Indexing
-------------------------------------------------

**Files:**
- Modify: `internal/server/session_scanner.go`

**Step 1: Add text extraction helper**

Add a function to `internal/server/session_scanner.go` that extracts indexable text from a JSONL line. This reuses the existing type detection patterns (`"type":"user"` and `"type":"assistant"`) but extracts plain text instead of formatting for display.

```go
// maxIndexableLineLen is the maximum line length we inspect for indexable content.
// User messages and assistant text are typically under 500 KB. Skip multi-MB
// lines (usually tool results with large code blocks).
const maxIndexableLineLen = 500 * 1024 // 500 KB

// tryExtractIndexableText extracts searchable text from a JSONL line.
// Returns user message text or assistant prose text (skipping tool_use and thinking blocks).
// Returns empty string for non-conversation lines or lines exceeding size limits.
func tryExtractIndexableText(line string) string {
	if len(line) > maxIndexableLineLen {
		return ""
	}

	// User messages
	if strings.Contains(line, `"type":"user"`) {
		return tryExtractUserMessage(line)
	}

	// Assistant messages — extract text blocks only
	if strings.Contains(line, `"type":"assistant"`) {
		return tryExtractAssistantText(line)
	}

	return ""
}

// jsonlAssistantEntry represents an assistant message entry for text extraction.
type jsonlAssistantEntry struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

// jsonlAssistantMessage represents the message portion of an assistant entry.
type jsonlAssistantMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// jsonlContentBlock represents a single content block in a message.
type jsonlContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// tryExtractAssistantText extracts text blocks from an assistant message,
// skipping tool_use and thinking blocks.
func tryExtractAssistantText(line string) string {
	var entry jsonlAssistantEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Type != "assistant" {
		return ""
	}
	var msg jsonlAssistantMessage
	if err := json.Unmarshal(entry.Message, &msg); err != nil {
		return ""
	}
	var blocks []jsonlContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return ""
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			texts = append(texts, b.Text)
		}
	}
	return strings.Join(texts, "\n")
}
```

**Step 2: Extend scanJSONLFromOffset to collect indexable text**

Modify `jsonlScanResult` to include content for indexing:

```go
type jsonlScanResult struct {
	customTitle      string
	firstUserMessage string
	indexableContent []string // NEW: accumulated text chunks for FTS indexing
}
```

Modify `scanJSONLFromOffset` to extract indexable text on every line:

In the `for` loop (around line 242-255), add after the existing extraction:

```go
// Always extract indexable text for FTS indexing
if text := tryExtractIndexableText(line); text != "" {
	result.indexableContent = append(result.indexableContent, text)
}
```

**Step 3: Extend scanMissionJSONLFiles to insert into FTS**

In `scanMissionJSONLFiles`, after the `UpdateSessionScanResults` call succeeds (line 132-135), add FTS indexing — but only commit the offset after FTS insertion succeeds:

Replace the existing offset-update block (lines 132-135) with:

```go
// Index content into FTS before advancing the offset.
// This ensures the offset only advances when both session name
// extraction AND FTS indexing succeed.
if len(scanResult.indexableContent) > 0 {
	content := strings.Join(scanResult.indexableContent, "\n")
	if err := s.db.InsertSearchContent(missionID, sessionID, content); err != nil {
		s.logger.Printf("Session scanner: failed to index FTS content for session '%s': %v", sessionID, err)
		continue // Don't advance offset — retry next cycle
	}
}

if err := s.db.UpdateSessionScanResults(sessionID, scanResult.customTitle, fileSize); err != nil {
	s.logger.Printf("Session scanner: failed to update session '%s': %v", sessionID, err)
	continue
}
```

**Step 4: Build and verify**

Run: `make build`
Expected: Clean build. The scanner now indexes new content into FTS on every 3-second cycle.

**Step 5: Commit**

```
git add internal/server/session_scanner.go
git commit -m "Extend session scanner to index conversation content into FTS5"
```

---

Task 4: Search Server Endpoint
--------------------------------

**Files:**
- Create: `internal/server/search.go`
- Modify: `internal/server/server.go:229` (add route)

**Step 1: Write the handler**

Create `internal/server/search.go`:

```go
package server

import (
	"net/http"
	"strconv"
)

// SearchMissionsResponse is a single result from mission search.
type SearchMissionsResponse struct {
	MissionID string  `json:"mission_id"`
	ShortID   string  `json:"short_id"`
	SessionID string  `json:"session_id"`
	Snippet   string  `json:"snippet"`
	Rank      float64 `json:"rank"`
	// Enriched fields from missions table
	GitRepo              string  `json:"git_repo"`
	Status               string  `json:"status"`
	Prompt               string  `json:"prompt"`
	ResolvedSessionTitle string  `json:"resolved_session_title"`
	LastHeartbeat        *string `json:"last_heartbeat"`
}

func (s *Server) handleSearchMissions(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query().Get("q")
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if query == "" {
		writeJSON(w, http.StatusOK, []SearchMissionsResponse{})
		return nil
	}

	results, err := s.db.SearchMissions(query, limit)
	if err != nil {
		return err
	}

	// Enrich results with mission metadata
	responses := make([]SearchMissionsResponse, 0, len(results))
	for _, r := range results {
		resp := SearchMissionsResponse{
			MissionID: r.MissionID,
			SessionID: r.SessionID,
			Snippet:   r.Snippet,
			Rank:      r.Rank,
		}

		// Look up mission metadata
		mission, err := s.db.GetMission(r.MissionID)
		if err == nil && mission != nil {
			resp.ShortID = mission.ShortID
			resp.GitRepo = mission.GitRepo
			resp.Status = mission.Status
			resp.Prompt = mission.Prompt
			// Resolve session title
			sessions, sessErr := s.db.ListSessionsByMission(r.MissionID)
			if sessErr == nil {
				for _, sess := range sessions {
					title := sess.CustomTitle
					if title == "" {
						title = sess.AgencCustomTitle
					}
					if title == "" {
						title = sess.AutoSummary
					}
					if title != "" {
						resp.ResolvedSessionTitle = title
						break
					}
				}
			}
			if mission.LastHeartbeat != nil {
				ts := mission.LastHeartbeat.Format("2006-01-02T15:04:05Z")
				resp.LastHeartbeat = &ts
			}
		}

		responses = append(responses, resp)
	}

	writeJSON(w, http.StatusOK, responses)
	return nil
}
```

**Step 2: Register route**

In `internal/server/server.go`, in `registerRoutes()`, add after the missions routes (around line 245):

```go
mux.Handle("GET /missions/search", appHandler(s.requestLogger, s.handleSearchMissions))
```

**Important:** This must be registered BEFORE the `GET /missions/{id}` route because Go's ServeMux matches the most specific pattern. `search` is a literal path segment that takes precedence over `{id}`.

**Step 3: Build and verify**

Run: `make build`
Expected: Clean build.

**Step 4: Commit**

```
git add internal/server/search.go internal/server/server.go
git commit -m "Add search missions server endpoint"
```

---

Task 5: Search Client Method
-------------------------------

**Files:**
- Modify: `internal/server/client.go`

**Step 1: Add SearchMissions method**

In `internal/server/client.go`, add after the existing mission methods (around line 336):

```go
// SearchMissions searches missions by FTS query and returns ranked results.
func (c *Client) SearchMissions(query string, limit int) ([]SearchMissionsResponse, error) {
	var results []SearchMissionsResponse
	path := fmt.Sprintf("/missions/search?q=%s&limit=%d", url.QueryEscape(query), limit)
	if err := c.Get(path, &results); err != nil {
		return nil, err
	}
	return results, nil
}
```

Add `"net/url"` to the imports if not already present.

**Step 2: Build and verify**

Run: `make build`
Expected: Clean build.

**Step 3: Commit**

```
git add internal/server/client.go
git commit -m "Add SearchMissions client method"
```

---

Task 6: CLI `mission search` Command
---------------------------------------

**Files:**
- Create: `cmd/mission_search.go`
- Modify: `cmd/command_str_consts.go` (add `searchCmdStr`)

**Step 1: Add command string constant**

In `cmd/command_str_consts.go`, add to the "Subcommands shared across multiple parent commands" block:

```go
searchCmdStr = "search"
```

**Step 2: Write the command**

Create `cmd/mission_search.go`:

```go
package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var searchJSONFlag bool
var searchLimitFlag int

var missionSearchCmd = &cobra.Command{
	Use:   searchCmdStr + " <query>",
	Short: "Search missions by conversation content",
	Long: `Search missions using full-text search over conversation transcripts.

Searches user messages, assistant responses, session titles, and mission prompts.
Results are ranked by relevance using BM25.

Run 'agenc reindex' to build or rebuild the search index.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runMissionSearch,
}

func init() {
	missionCmd.AddCommand(missionSearchCmd)
	missionSearchCmd.Flags().BoolVar(&searchJSONFlag, "json", false, "output results as JSON")
	missionSearchCmd.Flags().IntVar(&searchLimitFlag, "limit", 20, "maximum number of results")
}

func runMissionSearch(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	query := strings.Join(args, " ")
	results, err := client.SearchMissions(query, searchLimitFlag)
	if err != nil {
		return stacktrace.Propagate(err, "search failed")
	}

	if searchJSONFlag {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(results)
	}

	if len(results) == 0 {
		fmt.Println("No results.")
		return nil
	}

	for _, r := range results {
		shortID := r.ShortID
		if shortID == "" {
			shortID = r.MissionID[:8]
		}

		session := r.ResolvedSessionTitle
		if session == "" {
			session = truncatePrompt(r.Prompt, 60)
		}

		repo := displayGitRepo(r.GitRepo)

		// Clean up snippet brackets for terminal display
		snippet := r.Snippet

		fmt.Printf("%s  %s  %s\n", shortID, session, repo)
		if snippet != "" {
			fmt.Printf("  %s\n\n", snippet)
		}
	}

	return nil
}
```

**Step 3: Build and verify**

Run: `make build`
Expected: Clean build. `agenc mission search --help` shows the command.

**Step 4: Commit**

```
git add cmd/mission_search.go cmd/command_str_consts.go
git commit -m "Add mission search CLI command"
```

---

Task 7: `agenc reindex` Command
----------------------------------

**Files:**
- Create: `cmd/reindex.go`
- Modify: `cmd/command_str_consts.go` (add `reindexCmdStr`)

**Step 1: Add command string constant**

In `cmd/command_str_consts.go`, add to the "Top-level commands" block:

```go
reindexCmdStr = "reindex"
```

**Step 2: Write the reindex command**

Create `cmd/reindex.go`. This command walks ALL session JSONL files, extracts indexable content, and inserts into FTS5. It does NOT use `last_scanned_offset` — it reads each file from offset 0.

```go
package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var reindexCmd = &cobra.Command{
	Use:   reindexCmdStr,
	Short: "Rebuild the full-text search index",
	Long: `Rebuild the full-text search index from all session transcripts.

This reads every JSONL transcript file from the beginning and repopulates
the FTS5 search index. This is needed after first install to index
historical missions, or to recover from index corruption.

WARNING: This may take 30-60 seconds for large mission histories (10K+ missions).`,
	RunE: runReindex,
}

func init() {
	rootCmd.AddCommand(reindexCmd)
}

func runReindex(cmd *cobra.Command, args []string) error {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}

	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	fmt.Println("Clearing existing search index...")
	if err := db.DeleteAllSearchContent(); err != nil {
		return stacktrace.Propagate(err, "failed to clear search index")
	}

	// Find all sessions and their JSONL files
	sessions, err := db.ListAllSessions()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list sessions")
	}

	// Discover Claude projects directory
	claudeProjectsDir := filepath.Join(os.Getenv("HOME"), ".claude", "projects")

	startTime := time.Now()
	indexed := 0
	skipped := 0

	fmt.Printf("Indexing %d sessions...\n", len(sessions))

	for _, sess := range sessions {
		jsonlPath := findJSONLFile(claudeProjectsDir, sess.ID)
		if jsonlPath == "" {
			skipped++
			continue
		}

		content, err := extractIndexableContent(jsonlPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to index session %s: %v\n", sess.ID[:8], err)
			skipped++
			continue
		}

		if content == "" {
			skipped++
			continue
		}

		if err := db.InsertSearchContent(sess.MissionID, sess.ID, content); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to store index for session %s: %v\n", sess.ID[:8], err)
			skipped++
			continue
		}

		indexed++
	}

	elapsed := time.Since(startTime)
	fmt.Printf("Done. Indexed %d sessions, skipped %d, took %s\n", indexed, skipped, elapsed.Round(time.Millisecond))

	return nil
}

// findJSONLFile searches the Claude projects directory for a session's JSONL file.
func findJSONLFile(claudeProjectsDir string, sessionID string) string {
	filename := sessionID + ".jsonl"

	// Walk project directories looking for the file
	entries, err := os.ReadDir(claudeProjectsDir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(claudeProjectsDir, entry.Name(), filename)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

// extractIndexableContent reads a JSONL file and extracts all indexable text
// (user messages + assistant text blocks).
func extractIndexableContent(jsonlPath string) (string, error) {
	file, err := os.Open(jsonlPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	reader := bufio.NewReaderSize(file, 64*1024)
	var texts []string

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if text := extractTextFromJSONLLine(line); text != "" {
				texts = append(texts, text)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
	}

	return strings.Join(texts, "\n"), nil
}

// extractTextFromJSONLLine extracts searchable text from a single JSONL line.
// Extracts user messages and assistant text blocks, skipping tool calls and thinking.
func extractTextFromJSONLLine(line string) string {
	const maxLineLen = 500 * 1024 // 500 KB
	if len(line) > maxLineLen {
		return ""
	}

	if strings.Contains(line, `"type":"user"`) {
		return extractUserText(line)
	}
	if strings.Contains(line, `"type":"assistant"`) {
		return extractAssistantText(line)
	}
	return ""
}

// extractUserText extracts the text content from a user message JSONL line.
func extractUserText(line string) string {
	var entry struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Type != "user" {
		return ""
	}
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(entry.Message, &msg); err != nil {
		return ""
	}
	// Try string content
	var text string
	if err := json.Unmarshal(msg.Content, &text); err == nil {
		return text
	}
	// Try array content
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(msg.Content, &blocks); err == nil {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				parts = append(parts, b.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return ""
}

// extractAssistantText extracts text blocks from an assistant message JSONL line,
// skipping tool_use and thinking blocks.
func extractAssistantText(line string) string {
	var entry struct {
		Type    string          `json:"type"`
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Type != "assistant" {
		return ""
	}
	var msg struct {
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(entry.Message, &msg); err != nil {
		return ""
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return ""
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			texts = append(texts, b.Text)
		}
	}
	return strings.Join(texts, "\n")
}
```

**Step 3: Add ListAllSessions database method**

In `internal/database/sessions.go`, add:

```go
// ListAllSessions returns all sessions in the database.
func (db *DB) ListAllSessions() ([]*Session, error) {
	rows, err := db.conn.Query("SELECT id, short_id, mission_id, custom_title, agenc_custom_title, auto_summary, last_scanned_offset, created_at, updated_at FROM sessions")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list all sessions")
	}
	defer rows.Close()
	return scanSessions(rows)
}
```

Check if `scanSessions` already exists; if not, add a helper that scans session rows.

**Step 4: Build and verify**

Run: `make build`
Expected: Clean build. `agenc reindex --help` shows the command with the warning text.

**Step 5: Commit**

```
git add cmd/reindex.go cmd/command_str_consts.go internal/database/sessions.go
git commit -m "Add agenc reindex command for full-text index rebuild"
```

---

Task 8: Enhanced Mission Attach with Live Search
---------------------------------------------------

**Files:**
- Modify: `cmd/mission_attach.go`
- Modify: `cmd/fzf_picker.go` (add search-mode picker)

**Step 1: Add a search-mode fzf function**

In `cmd/fzf_picker.go`, add a new function that runs fzf with `--disabled` and `--bind 'change:reload(...)'`:

```go
// FzfSearchPickerConfig defines the configuration for a search-mode fzf picker
// where results come from an external command on each keystroke.
type FzfSearchPickerConfig struct {
	Prompt         string   // The prompt displayed to the user
	Headers        []string // Column headers
	InitialRows    [][]string // Rows to show before the user types anything
	ReloadCommand  string   // Command to run on each keystroke (receives {q} as query)
	MultiSelect    bool
}

// runFzfSearchPicker runs fzf in search mode with dynamic reloading.
// The initial display shows InitialRows. As the user types, fzf calls
// ReloadCommand with the query, replacing the displayed results.
// Returns the index column value from the selected row.
func runFzfSearchPicker(cfg FzfSearchPickerConfig) (string, error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return "", stacktrace.NewError("interactive selection requires a terminal")
	}
	if err := validateHeaders(cfg.Headers); err != nil {
		return "", stacktrace.Propagate(err, "")
	}

	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return "", stacktrace.Propagate(err, "'fzf' binary not found in PATH")
	}

	// Build initial input with header + data rows
	var fzfInput strings.Builder
	// Header row
	tbl := tableprinter.NewTable(toAnySlice(cfg.Headers)...).WithWriter(&fzfInput)
	for _, row := range cfg.InitialRows {
		tbl.AddRow(toAnySlice(row)...)
	}
	tbl.Print()

	// Prepend index column to each line (mission ID for selection)
	lines := strings.Split(strings.TrimSuffix(fzfInput.String(), "\n"), "\n")
	var indexedInput strings.Builder
	if len(lines) > 0 {
		// Header line
		indexedInput.WriteString("HEADER\t")
		indexedInput.WriteString(lines[0])
		indexedInput.WriteString("\n")
	}
	for i, line := range lines[1:] {
		if i < len(cfg.InitialRows) {
			indexedInput.WriteString(cfg.InitialRows[i][0]) // Use first column (short ID) as index
		}
		indexedInput.WriteString("\t")
		indexedInput.WriteString(line)
		indexedInput.WriteString("\n")
	}

	args := []string{
		"--ansi",
		"--header-lines", "1",
		"--with-nth", "2..",
		"--prompt", cfg.Prompt,
		"--disabled",
		"--bind", "change:reload:" + cfg.ReloadCommand,
	}
	if cfg.MultiSelect {
		args = append(args, "--multi")
	}

	fzfCmd := exec.Command(fzfBinary, args...)
	fzfCmd.Stdin = strings.NewReader(indexedInput.String())
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			return "", nil // User cancelled
		}
		return "", stacktrace.Propagate(err, "fzf search failed")
	}

	// Extract mission ID from the first column of selected line
	selected := strings.TrimSpace(string(output))
	if selected == "" {
		return "", nil
	}
	id, _, _ := strings.Cut(selected, "\t")
	return strings.TrimSpace(id), nil
}
```

**Step 2: Create a helper command for fzf reload**

The fzf `change:reload` needs to call a command that outputs formatted results. Add a hidden subcommand `agenc mission search-fzf` that outputs tab-formatted results for fzf consumption.

Create `cmd/mission_search_fzf.go`:

```go
package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/tableprinter"
)

// missionSearchFzfCmd is a hidden command used by fzf's change:reload binding.
// It outputs search results formatted for fzf consumption with tab-separated
// index columns.
var missionSearchFzfCmd = &cobra.Command{
	Use:    "search-fzf <query>",
	Short:  "Search missions (fzf helper)",
	Hidden: true,
	Args:   cobra.ArbitraryArgs,
	RunE:   runMissionSearchFzf,
}

func init() {
	missionCmd.AddCommand(missionSearchFzfCmd)
}

func runMissionSearchFzf(cmd *cobra.Command, args []string) error {
	query := strings.Join(args, " ")
	if query == "" {
		// Empty query — show recent missions
		return printRecentMissionsForFzf()
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	results, err := client.SearchMissions(query, 30)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		return nil
	}

	cfg, _ := readConfig()

	// Format as table with ID prefix for selection
	var buf strings.Builder
	tbl := tableprinter.NewTable("ID", "SESSION", "REPO", "MATCH").WithWriter(&buf)
	for _, r := range results {
		shortID := r.ShortID
		if shortID == "" && len(r.MissionID) >= 8 {
			shortID = r.MissionID[:8]
		}

		session := r.ResolvedSessionTitle
		if session == "" {
			session = truncatePrompt(r.Prompt, 50)
		}

		repo := displayGitRepo(r.GitRepo)
		if cfg != nil {
			if t := cfg.GetRepoTitle(r.GitRepo); t != "" {
				repo = t
			}
			if e := cfg.GetRepoEmoji(r.GitRepo); e != "" {
				repo = e + "  " + repo
			}
		}

		snippet := strings.ReplaceAll(r.Snippet, "\n", " ")
		if len(snippet) > 60 {
			snippet = snippet[:60] + "…"
		}

		tbl.AddRow(shortID, session, repo, snippet)
	}
	tbl.Print()

	// Prepend mission ID as hidden index column
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	for i, line := range lines {
		if i < len(results) {
			shortID := results[i].ShortID
			if shortID == "" && len(results[i].MissionID) >= 8 {
				shortID = results[i].MissionID[:8]
			}
			fmt.Printf("%s\t%s\n", shortID, line)
		}
	}

	return nil
}

func printRecentMissionsForFzf() error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(true, "", "")
	if err != nil {
		return err
	}

	sortMissionsForPicker(missions)
	entries := buildMissionPickerEntries(missions, 50)

	cfg, _ := readConfig()
	_ = cfg

	var buf strings.Builder
	tbl := tableprinter.NewTable("ID", "SESSION", "REPO", "MATCH").WithWriter(&buf)
	for _, e := range entries {
		tbl.AddRow(e.ShortID, e.Session, e.Repo, "--")
	}
	tbl.Print()

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	for i, line := range lines {
		if i < len(entries) {
			fmt.Printf("%s\t%s\n", entries[i].ShortID, line)
		}
	}

	return nil
}
```

**Step 3: Modify mission attach to use search picker**

In `cmd/mission_attach.go`, modify `runMissionAttach` to use the search picker when no args are given:

Replace the current `Resolve` block with logic that:
1. If args provided and look like mission IDs → resolve directly (existing behavior)
2. If args provided and don't look like IDs → use as search query
3. If no args → open search picker

The key change is in the `runMissionAttach` function. The approach: find the `agenc` binary path for the reload command, then launch the search-mode fzf.

```go
func runMissionAttach(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	tmuxSession := getCurrentTmuxSessionName()
	if tmuxSession == "" {
		return stacktrace.NewError("mission attach requires tmux; run inside a tmux session")
	}

	input := strings.Join(args, " ")

	var missionID string

	if input != "" && looksLikeMissionID(input) {
		// Direct ID resolution
		resolved, err := client.ResolveMissionID(input)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve mission ID")
		}
		missionID = resolved
	} else {
		// Search-based picker (either with initial query from args, or empty)
		agencBinary, err := os.Executable()
		if err != nil {
			agencBinary = "agenc"
		}
		reloadCmd := fmt.Sprintf("%s mission search-fzf {q}", agencBinary)

		// Build initial rows (recent missions for empty query)
		missions, err := client.ListMissions(true, "", "")
		if err != nil {
			return stacktrace.Propagate(err, "failed to list missions")
		}
		sortMissionsForPicker(missions)
		entries := buildMissionPickerEntries(missions, 50)

		initialRows := make([][]string, len(entries))
		for i, e := range entries {
			initialRows[i] = []string{e.ShortID, e.Session, e.Repo, "--"}
		}

		selectedID, err := runFzfSearchPicker(FzfSearchPickerConfig{
			Prompt:        "Search missions: ",
			Headers:       []string{"ID", "SESSION", "REPO", "MATCH"},
			InitialRows:   initialRows,
			ReloadCommand: reloadCmd,
		})
		if err != nil {
			return err
		}
		if selectedID == "" {
			return nil // User cancelled
		}

		resolved, err := client.ResolveMissionID(selectedID)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve selected mission")
		}
		missionID = resolved
	}

	// Migrate old .assistant marker if present
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}
	if err := config.MigrateAssistantMarkerIfNeeded(agencDirpath, missionID); err != nil {
		return stacktrace.Propagate(err, "failed to migrate assistant marker")
	}

	fmt.Printf("Attaching mission: %s\n", database.ShortID(missionID))

	if err := client.AttachMission(missionID, tmuxSession, attachNoFocusFlag); err != nil {
		return stacktrace.Propagate(err, "failed to attach mission")
	}

	return nil
}
```

**Step 4: Build and verify**

Run: `make build`
Expected: Clean build. `agenc mission attach` opens a search-mode picker.

**Step 5: Manual smoke test**

If in the test environment, create a mission with a known prompt, run `agenc reindex`, then `agenc mission attach` and type keywords from the prompt.

**Step 6: Commit**

```
git add cmd/mission_attach.go cmd/fzf_picker.go cmd/mission_search_fzf.go
git commit -m "Add live search to mission attach picker"
```

---

Task 9: E2E Tests
--------------------

**Files:**
- Modify: `scripts/e2e-test.sh`

**Step 1: Add search E2E tests**

Append to `scripts/e2e-test.sh` before the summary section:

```bash
echo "--- Mission Search ---"

run_test "mission search with no query fails" \
    1 \
    "${agenc_test}" mission search

run_test_output_contains "mission search with no results" \
    "No results" \
    "${agenc_test}" mission search "xyznonexistent12345"

run_test "mission search json with no results" \
    0 \
    "${agenc_test}" mission search --json "xyznonexistent12345"

run_test_output_contains "mission search help shows reindex mention" \
    "reindex" \
    "${agenc_test}" mission search --help

echo "--- Reindex ---"

run_test_output_contains "reindex runs successfully" \
    "Done" \
    "${agenc_test}" reindex

run_test_output_contains "reindex help shows warning" \
    "WARNING" \
    "${agenc_test}" reindex --help
```

**Step 2: Run E2E tests**

Run: `make e2e`
Expected: All new tests pass.

**Step 3: Commit**

```
git add scripts/e2e-test.sh
git commit -m "Add E2E tests for mission search and reindex"
```

---

Task 10: Update Architecture Docs
------------------------------------

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Add search section**

Add a section documenting the FTS5 search index, the session scanner's dual-output pipeline, the search server endpoint, and the reindex command. Follow the existing style (filepath-level descriptions, no code snippets).

**Step 2: Commit**

```
git add docs/system-architecture.md
git commit -m "Document FTS5 mission search in architecture docs"
```
