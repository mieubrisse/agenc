Mission Search Implementation Plan
===================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add FTS5 full-text search over mission transcripts so users can find missions by topic/content.

**Architecture:** Three-layer session processing pipeline: file watcher (tracks JSONL sizes) → consumers (title scanner + FTS indexer, each with independent offsets) → coordinated via sessions table. FTS5 virtual table for search. New server endpoint + CLI command. Enhanced `mission attach` with live search.

**Tech Stack:** Go, SQLite FTS5 (via modernc.org/sqlite), Cobra CLI, fzf

---

Task 1: Database Migration — Schema Changes
----------------------------------------------

**Files:**
- Modify: `internal/database/migrations.go`
- Modify: `internal/database/database.go:25-46` (add to getMigrationSteps)

This migration does four things:
1. Creates the FTS5 virtual table `mission_search_index`
2. Adds `known_file_size INTEGER NOT NULL DEFAULT 0` to the sessions table
3. Renames `last_scanned_offset` → `last_title_update_offset` on the sessions table
4. Adds `last_indexed_offset INTEGER NOT NULL DEFAULT 0` to the sessions table

**Step 1: Add SQL constants**

In `internal/database/migrations.go`, add after the existing SQL constants:

```go
const createMissionSearchIndexSQL = `CREATE VIRTUAL TABLE IF NOT EXISTS mission_search_index USING fts5(
	mission_id UNINDEXED,
	session_id UNINDEXED,
	content,
	tokenize='porter unicode61'
);`

const addKnownFileSizeColumnSQL = `ALTER TABLE sessions ADD COLUMN known_file_size INTEGER NOT NULL DEFAULT 0;`
const addLastIndexedOffsetColumnSQL = `ALTER TABLE sessions ADD COLUMN last_indexed_offset INTEGER NOT NULL DEFAULT 0;`
```

**Step 2: Add migration function**

Single migration function `migrateSearchIndex` that:
- Creates FTS5 table (check sqlite_master since virtual tables don't appear in PRAGMA table_info)
- Adds `known_file_size` column
- Renames `last_scanned_offset` → `last_title_update_offset`
- Adds `last_indexed_offset` column
- All idempotent via column existence checks

**Step 3: Register in getMigrationSteps()**

**Step 4: Update Session struct**

In `internal/database/sessions.go`:
- Rename `LastScannedOffset` field → `LastTitleUpdateOffset`
- Add `KnownFileSize int64`
- Add `LastIndexedOffset int64`
- Update all scan/query methods

**Step 5: Update session scanner references**

In `internal/server/session_scanner.go`, rename all `LastScannedOffset` → `LastTitleUpdateOffset`.

**Step 6: Build and verify**

Run: `make build`

**Step 7: Commit**

```
git commit -m "Add FTS5 migration, known_file_size, rename offsets, add last_indexed_offset"
```

---

Task 2: Refactor Session Scanner into File Watcher + Title Consumer
---------------------------------------------------------------------

**Files:**
- Modify: `internal/server/session_scanner.go` (becomes file watcher + title consumer)
- Modify: `internal/database/sessions.go` (add UpdateKnownFileSize, query methods)

This is the key refactor. Split the current `scanMissionJSONLFiles` into two concerns:

**File watcher** (`runFileWatcherLoop`, ~3s):
1. Lists running tmux panes → resolves to missions → walks project dirs for JSONL files
2. For each JSONL file: stats it, updates `known_file_size` in the sessions table
3. Does NOT read file content

**Title consumer** (`runTitleConsumerLoop`, ~3s):
1. Queries sessions where `known_file_size > last_title_update_offset` AND mission is running
2. For each: opens file, seeks to `last_title_update_offset`, reads to `known_file_size`
3. Extracts custom titles and first user messages (existing logic)
4. Updates `last_title_update_offset` in a transaction

The file watcher replaces the current `runSessionScannerLoop`. The title consumer runs as a separate goroutine at the same cadence.

**Step 1: Add DB methods**

- `UpdateKnownFileSize(sessionID string, size int64) error`
- `SessionsNeedingTitleUpdate() ([]*Session, error)` — `WHERE known_file_size > last_title_update_offset`
- `UpdateTitleAndOffset(sessionID, customTitle string, newOffset int64) error`

**Step 2: Split the scanner**

Extract file-watching logic (pane discovery, dir walking, stat) into `runFileWatcherLoop`. Extract content processing into `runTitleConsumerLoop`.

**Step 3: Start both goroutines** in `server.go`

**Step 4: Build and verify**

Run: `make build`
Verify existing title scanning still works (functional equivalence).

**Step 5: Commit**

```
git commit -m "Refactor session scanner into file watcher + title consumer"
```

---

Task 3: FTS5 Database Methods
-------------------------------

**Files:**
- Create: `internal/database/search.go`
- Create: `internal/database/search_test.go`

**Step 1: Write tests**

Tests for:
- `InsertSearchContentAndUpdateOffset` — insert + offset update in one transaction
- `SearchMissions` — finds matching mission, returns snippet, deduplicates by mission_id
- `SearchMissions` empty query — returns nil
- `SearchMissions` no results — returns empty slice
- `SearchMissions` special characters — doesn't crash
- `DeleteAllSearchContent` — clears the index
- `SessionsNeedingIndexing` — returns sessions where `known_file_size > last_indexed_offset`

**Step 2: Run tests to verify they fail**

**Step 3: Write implementation**

- `SearchResult` struct: `MissionID`, `SessionID`, `Snippet`, `Rank`
- `InsertSearchContentAndUpdateOffset(missionID, sessionID, content string, newOffset int64) error` — single transaction
- `SearchMissions(query string, limit int) ([]SearchResult, error)` — wraps query in double quotes, deduplicates by mission_id
- `DeleteAllSearchContent() error`
- `SessionsNeedingIndexing() ([]*Session, error)` — `WHERE known_file_size > last_indexed_offset`

**Step 4: Run tests, verify pass**

**Step 5: Commit**

```
git commit -m "Add FTS5 search database methods"
```

---

Task 4: FTS Indexer Consumer
------------------------------

**Files:**
- Create: `internal/server/search_indexer.go`
- Modify: `internal/server/server.go` (start goroutine)

The FTS indexer is a consumer goroutine. It wakes up every ~30 seconds, queries for sessions where `known_file_size > last_indexed_offset`, and processes them.

**Step 1: Create the indexer**

`internal/server/search_indexer.go`:
- `runSearchIndexerLoop(ctx context.Context)` — 30s ticker
- `runSearchIndexerCycle()` — queries `SessionsNeedingIndexing()`, processes each
- `indexSession(session)` — computes JSONL path, opens file, seeks to `LastIndexedOffset`, reads to `KnownFileSize`, extracts user/assistant text, calls `InsertSearchContentAndUpdateOffset`
- Text extraction helpers: `tryExtractIndexableText`, `tryExtractAssistantText` — reuse existing string-match-before-JSON-parse pattern

**Step 2: Start goroutine** in `server.go` alongside the file watcher and title consumer

**Step 3: Build and verify**

Run: `make build`

**Step 4: Commit**

```
git commit -m "Add FTS indexer consumer goroutine"
```

---

Task 5: Search Server Endpoint
--------------------------------

**Files:**
- Create: `internal/server/search.go`
- Modify: `internal/server/server.go` (add route)

**Step 1: Write handler**

`handleSearchMissions`: accepts `?q=<query>&limit=<n>`, calls `db.SearchMissions`, enriches results with mission metadata (short_id, git_repo, status, resolved session title, last_heartbeat). Returns JSON array.

**Step 2: Register route** BEFORE `GET /missions/{id}`:

```go
mux.Handle("GET /missions/search", appHandler(s.requestLogger, s.handleSearchMissions))
```

**Step 3: Build, commit**

```
git commit -m "Add search missions server endpoint"
```

---

Task 6: Search Client Method
-------------------------------

**Files:**
- Modify: `internal/server/client.go`

Add `SearchMissions(query string, limit int) ([]SearchMissionsResponse, error)`.

**Commit:**
```
git commit -m "Add SearchMissions client method"
```

---

Task 7: CLI `mission search` Command
---------------------------------------

**Files:**
- Create: `cmd/mission_search.go`
- Modify: `cmd/command_str_consts.go` (add `searchCmdStr`)

Takes `<query>` as positional args. Supports `--json` and `--limit`. Displays ranked results with short ID, session name, repo, and match snippet.

**Commit:**
```
git commit -m "Add mission search CLI command"
```

---

Task 8: Enhanced Mission Attach with Live Search
---------------------------------------------------

**Files:**
- Modify: `cmd/mission_attach.go`
- Modify: `cmd/fzf_picker.go` (add search-mode picker)
- Create: `cmd/mission_search_fzf.go` (hidden helper for fzf reload)

**Step 1: Add `runFzfSearchPicker`** in `fzf_picker.go` — uses `--disabled` + `--bind 'change:reload(...)'`

**Step 2: Create hidden `mission search-fzf` command** — outputs tab-formatted results for fzf. Empty query returns recent missions.

**Step 3: Modify `mission attach`:**
- Args that look like mission IDs → resolve directly
- No args → open search-mode fzf picker
- Args that don't look like IDs → pass as initial query

**Commit:**
```
git commit -m "Add live search to mission attach picker"
```

---

Task 9: E2E Tests
--------------------

**Files:**
- Modify: `scripts/e2e-test.sh`

Tests:
- `mission search` with no query fails (exit 1)
- `mission search "nonexistent"` returns "No results"
- `mission search --json "nonexistent"` returns valid JSON
- `mission search --help` shows help

**Commit:**
```
git commit -m "Add E2E tests for mission search"
```

---

Task 10: Update Architecture Docs
------------------------------------

**Files:**
- Modify: `docs/system-architecture.md`

Document:
- Three-layer session processing architecture (file watcher → consumers → sessions table)
- `known_file_size`, `last_title_update_offset`, `last_indexed_offset` columns
- FTS5 search index schema
- Search server endpoint
- `mission search` CLI command

**Commit:**
```
git commit -m "Document session processing pipeline and FTS5 search in architecture docs"
```
