Mission Search Implementation Plan
===================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add FTS5 full-text search over mission transcripts so users can find missions by topic/content.

**Architecture:** SQLite FTS5 virtual table indexed by a dedicated server goroutine. Separate `last_indexed_offset` column (default 0) for natural backfill. New server endpoint + CLI command for programmatic search. Enhanced `mission attach` picker with live server-side search via fzf `--disabled` + `change:reload`.

**Tech Stack:** Go, SQLite FTS5 (via modernc.org/sqlite), Cobra CLI, fzf

---

Task 1: Database Migration — FTS5 Table + Column Changes
-----------------------------------------------------------

**Files:**
- Modify: `internal/database/migrations.go`
- Modify: `internal/database/database.go:25-46` (add to getMigrationSteps)

This migration does three things:
1. Creates the FTS5 virtual table `mission_search_index`
2. Renames `last_scanned_offset` → `last_title_update_offset` on the sessions table
3. Adds `last_indexed_offset INTEGER NOT NULL DEFAULT 0` to the sessions table

**Step 1: Add SQL constants**

In `internal/database/migrations.go`, add after the existing SQL constants (after line 53):

```go
const createMissionSearchIndexSQL = `CREATE VIRTUAL TABLE IF NOT EXISTS mission_search_index USING fts5(
	mission_id UNINDEXED,
	session_id UNINDEXED,
	content,
	tokenize='porter unicode61'
);`

const addLastIndexedOffsetColumnSQL = `ALTER TABLE sessions ADD COLUMN last_indexed_offset INTEGER NOT NULL DEFAULT 0;`
```

**Step 2: Add migration function**

In `internal/database/migrations.go`, add the migration function:

```go
// migrateSearchIndex idempotently creates the FTS5 virtual table for full-text
// mission search, renames last_scanned_offset to last_title_update_offset, and
// adds the last_indexed_offset column for the FTS indexer.
func migrateSearchIndex(conn *sql.DB) error {
	// Create FTS5 table (check sqlite_master since virtual tables don't
	// appear in PRAGMA table_info)
	var count int
	err := conn.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='mission_search_index'").Scan(&count)
	if err != nil {
		return stacktrace.Propagate(err, "failed to check for mission_search_index table")
	}
	if count == 0 {
		if _, err := conn.Exec(createMissionSearchIndexSQL); err != nil {
			return stacktrace.Propagate(err, "failed to create mission_search_index table")
		}
	}

	// Rename last_scanned_offset → last_title_update_offset
	columns, err := getSessionColumnNames(conn)
	if err != nil {
		return err
	}
	if columns["last_scanned_offset"] {
		if _, err := conn.Exec("ALTER TABLE sessions RENAME COLUMN last_scanned_offset TO last_title_update_offset"); err != nil {
			return stacktrace.Propagate(err, "failed to rename last_scanned_offset to last_title_update_offset")
		}
	}

	// Add last_indexed_offset column
	// Re-read columns after rename
	columns, err = getSessionColumnNames(conn)
	if err != nil {
		return err
	}
	if !columns["last_indexed_offset"] {
		if _, err := conn.Exec(addLastIndexedOffsetColumnSQL); err != nil {
			return stacktrace.Propagate(err, "failed to add last_indexed_offset column")
		}
	}

	return nil
}
```

**Step 3: Register migration**

In `internal/database/database.go`, add to `getMigrationSteps()` after the last entry:

```go
{migrateSearchIndex, "create FTS5 search index and add indexer offset"},
```

**Step 4: Update Session struct**

In `internal/database/sessions.go`, rename the `LastScannedOffset` field to `LastTitleUpdateOffset` and add `LastIndexedOffset`. Update all scan/query methods that reference the old column name.

**Step 5: Update session scanner**

In `internal/server/session_scanner.go`, rename all references from `LastScannedOffset` to `LastTitleUpdateOffset`. The scanner logic is otherwise unchanged — it continues to track its own offset independently.

**Step 6: Build and verify**

Run: `make build`
Expected: Clean build. Database auto-migrates on next server start.

**Step 7: Commit**

```
git add internal/database/migrations.go internal/database/database.go internal/database/sessions.go internal/server/session_scanner.go
git commit -m "Add FTS5 migration, rename last_scanned_offset, add last_indexed_offset"
```

---

Task 2: FTS5 Database Methods
-------------------------------

**Files:**
- Create: `internal/database/search.go`
- Create: `internal/database/search_test.go`

**Step 1: Write tests**

Create `internal/database/search_test.go` with tests for:
- `InsertSearchContent` — basic insert
- `SearchMissions` — finds matching mission, returns snippet
- `SearchMissions` with empty query — returns nil
- `SearchMissions` with no results — returns empty slice
- `SearchMissions` with special characters — doesn't crash
- `DeleteAllSearchContent` — clears the index

**Step 2: Run tests to verify they fail**

Run: `cd internal/database && go test -run TestSearch -v`
Expected: FAIL — methods not defined

**Step 3: Write implementation**

Create `internal/database/search.go` with:
- `InsertSearchContent(missionID, sessionID, content string) error`
- `SearchMissions(query string, limit int) ([]SearchResult, error)` — wraps query in double quotes for literal matching, deduplicates by mission_id keeping best rank
- `DeleteAllSearchContent() error`

The `SearchResult` struct has: `MissionID`, `SessionID`, `Snippet`, `Rank`.

**Step 4: Run tests to verify they pass**

Run: `cd internal/database && go test -run TestSearch -v`
Expected: All PASS

**Step 5: Commit**

```
git add internal/database/search.go internal/database/search_test.go
git commit -m "Add FTS5 search database methods"
```

---

Task 3: FTS Indexer Goroutine
-------------------------------

**Files:**
- Create: `internal/server/search_indexer.go`
- Modify: `internal/server/server.go` (start the goroutine)

The FTS indexer is a new server goroutine that runs every ~30 seconds. It iterates ALL sessions in the database, checks `last_indexed_offset` vs JSONL file size, extracts indexable text from new content, and inserts it into FTS5 — all within a single transaction per session.

**Step 1: Create the indexer**

Create `internal/server/search_indexer.go` with:
- `runSearchIndexerLoop(ctx context.Context)` — ticker loop at 30-second intervals
- `runSearchIndexerCycle()` — lists all sessions, finds their JSONL files, indexes unprocessed content
- `indexSessionContent(session, jsonlPath)` — reads from `LastIndexedOffset`, extracts user messages + assistant text blocks, inserts into FTS5 and updates offset in one transaction
- Text extraction helpers: `tryExtractIndexableText(line)`, `tryExtractAssistantText(line)` — reuse the existing string-matching-before-JSON-parsing pattern from the session scanner

**Key design points:**
- Uses `db.conn` directly for the transaction (INSERT into FTS5 + UPDATE sessions SET last_indexed_offset in one tx)
- Discovers JSONL files by iterating all sessions, looking up mission → agent dir → project dir → `{sessionID}.jsonl`
- Skips sessions where file size <= `LastIndexedOffset`
- Logs errors per-session but continues to next session (no abort on individual failure)

**Step 2: Start the goroutine**

In `internal/server/server.go`, in the server startup code where other goroutines are launched (near `runSessionScannerLoop`), add:

```go
go s.runSearchIndexerLoop(ctx)
```

**Step 3: Add a database method for atomic FTS insert + offset update**

In `internal/database/search.go`, add:

```go
func (db *DB) InsertSearchContentAndUpdateOffset(missionID, sessionID, content string, newOffset int64) error {
	tx, err := db.conn.Begin()
	// INSERT INTO mission_search_index ...
	// UPDATE sessions SET last_indexed_offset = ? WHERE id = ?
	// tx.Commit()
}
```

**Step 4: Build and verify**

Run: `make build`
Expected: Clean build. Server now starts the FTS indexer goroutine.

**Step 5: Commit**

```
git add internal/server/search_indexer.go internal/server/server.go internal/database/search.go
git commit -m "Add FTS indexer goroutine with separate offset tracking"
```

---

Task 4: Search Server Endpoint
--------------------------------

**Files:**
- Create: `internal/server/search.go`
- Modify: `internal/server/server.go` (add route)

**Step 1: Write the handler**

Create `internal/server/search.go` with `handleSearchMissions`. Accepts `?q=<query>&limit=<n>`. Calls `db.SearchMissions`, enriches results with mission metadata (short_id, git_repo, status, resolved session title, last_heartbeat). Returns JSON array.

**Step 2: Register route**

In `internal/server/server.go` `registerRoutes()`, add BEFORE `GET /missions/{id}`:

```go
mux.Handle("GET /missions/search", appHandler(s.requestLogger, s.handleSearchMissions))
```

**Step 3: Build and verify**

Run: `make build`

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

**Step 1: Add `SearchMissions` method**

```go
func (c *Client) SearchMissions(query string, limit int) ([]SearchMissionsResponse, error) {
	path := fmt.Sprintf("/missions/search?q=%s&limit=%d", url.QueryEscape(query), limit)
	// ...
}
```

**Step 2: Build and verify**

Run: `make build`

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

Add `searchCmdStr = "search"` to the subcommands block.

**Step 2: Write the command**

Create `cmd/mission_search.go`. Takes `<query>` as positional args. Supports `--json` and `--limit`. Displays ranked results with short ID, session name, repo, and match snippet.

**Step 3: Build and verify**

Run: `make build`
Verify: `agenc mission search --help`

**Step 4: Commit**

```
git add cmd/mission_search.go cmd/command_str_consts.go
git commit -m "Add mission search CLI command"
```

---

Task 7: Enhanced Mission Attach with Live Search
---------------------------------------------------

**Files:**
- Modify: `cmd/mission_attach.go`
- Modify: `cmd/fzf_picker.go` (add search-mode picker)
- Create: `cmd/mission_search_fzf.go` (hidden helper for fzf reload)

**Step 1: Add search-mode fzf function**

In `cmd/fzf_picker.go`, add `runFzfSearchPicker` that uses `--disabled` + `--bind 'change:reload(...)'`. Shows initial rows (recent missions), switches to search results as user types.

**Step 2: Create hidden fzf helper command**

Create `cmd/mission_search_fzf.go` — `agenc mission search-fzf <query>`. Hidden command that outputs tab-formatted search results for fzf consumption. Empty query returns recent missions sorted by recency.

**Step 3: Modify mission attach**

In `cmd/mission_attach.go`, change `runMissionAttach`:
- Args that look like mission IDs → resolve directly (existing behavior)
- No args → open search-mode fzf picker
- Args that don't look like IDs → pass as initial query to search picker

**Step 4: Build and manual smoke test**

Run: `make build`
Test: `agenc mission attach` opens search picker

**Step 5: Commit**

```
git add cmd/mission_attach.go cmd/fzf_picker.go cmd/mission_search_fzf.go
git commit -m "Add live search to mission attach picker"
```

---

Task 8: E2E Tests
--------------------

**Files:**
- Modify: `scripts/e2e-test.sh`

**Step 1: Add search E2E tests**

- `mission search` with no query fails (exit 1)
- `mission search "nonexistent"` returns "No results"
- `mission search --json "nonexistent"` returns valid JSON
- `mission search --help` mentions search

**Step 2: Run E2E tests**

Run: `make e2e`

**Step 3: Commit**

```
git add scripts/e2e-test.sh
git commit -m "Add E2E tests for mission search"
```

---

Task 9: Update Architecture Docs
------------------------------------

**Files:**
- Modify: `docs/system-architecture.md`

Add section documenting:
- FTS5 search index and its schema
- The search indexer goroutine (separate from session scanner, 30s cadence, all sessions)
- The two offset columns (`last_title_update_offset`, `last_indexed_offset`) and their independence
- The search server endpoint
- The `mission search` CLI command

**Commit:**

```
git add docs/system-architecture.md
git commit -m "Document FTS5 mission search in architecture docs"
```
