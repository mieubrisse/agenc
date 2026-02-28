Session Scanner Pipeline Implementation Plan
=============================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a server-side session scanner that polls JSONL files, tracks session metadata in a new `sessions` table, and updates tmux window titles when `/rename` custom titles or auto-summaries are detected.

**Architecture:** A new `sessions` table stores per-session metadata (custom_title, auto_summary, last_scanned_offset). A 3-second polling loop in the server globs for JSONL files, incrementally scans from byte offsets, and updates the DB. An idempotent `reconcileTmuxWindowTitle` function converges tmux window names using a priority chain (custom_title > auto_summary > repo name > mission short ID).

**Tech Stack:** Go, SQLite (modernc.org/sqlite), tmux CLI

**Design doc:** `docs/plans/2026-02-27-session-scanner-pipeline-design.md`

---

### Task 1: Create the `sessions` table migration

**Files:**
- Modify: `internal/database/migrations.go` — add SQL constants and migration function
- Modify: `internal/database/database.go` — call the new migration from `Open()`

**Step 1: Add SQL constants to migrations.go**

Add these constants to the `const` block at the top of `internal/database/migrations.go`:

```go
createSessionsTableSQL = `CREATE TABLE IF NOT EXISTS sessions (
	id TEXT PRIMARY KEY,
	mission_id TEXT NOT NULL,
	custom_title TEXT NOT NULL DEFAULT '',
	auto_summary TEXT NOT NULL DEFAULT '',
	last_scanned_offset INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	FOREIGN KEY (mission_id) REFERENCES missions(id) ON DELETE CASCADE
);`
createSessionsMissionIDIndexSQL = `CREATE INDEX IF NOT EXISTS idx_sessions_mission_id ON sessions(mission_id);`
```

**Step 2: Add migration function to migrations.go**

Add at the bottom of `internal/database/migrations.go`:

```go
// migrateCreateSessionsTable idempotently creates the sessions table and its
// mission_id index for tracking per-session metadata from JSONL files.
func migrateCreateSessionsTable(conn *sql.DB) error {
	if _, err := conn.Exec(createSessionsTableSQL); err != nil {
		return stacktrace.Propagate(err, "failed to create sessions table")
	}
	if _, err := conn.Exec(createSessionsMissionIDIndexSQL); err != nil {
		return stacktrace.Propagate(err, "failed to create sessions mission_id index")
	}
	return nil
}
```

**Step 3: Call migration from Open()**

In `internal/database/database.go`, add after the `migrateAddQueryIndices` block (after line 95):

```go
if err := migrateCreateSessionsTable(conn); err != nil {
	conn.Close()
	return nil, stacktrace.Propagate(err, "failed to create sessions table")
}
```

**Step 4: Build and verify**

Run: `make check`
Expected: PASS — migration runs on a fresh DB (the test helper calls `Open()`)

**Step 5: Commit**

```
git add internal/database/migrations.go internal/database/database.go
git commit -m "Add sessions table migration"
```

---

### Task 2: Add Session struct and CRUD functions

**Files:**
- Create: `internal/database/sessions.go` — Session struct and CRUD functions

**Step 1: Write tests first**

Create `internal/database/sessions_test.go`:

```go
package database

import (
	"testing"
)

func TestCreateAndGetSession(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	session, err := db.CreateSession(mission.ID, "session-uuid-123")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if session.ID != "session-uuid-123" {
		t.Errorf("expected ID 'session-uuid-123', got %q", session.ID)
	}
	if session.MissionID != mission.ID {
		t.Errorf("expected MissionID %q, got %q", mission.ID, session.MissionID)
	}
	if session.CustomTitle != "" {
		t.Errorf("expected empty custom_title, got %q", session.CustomTitle)
	}

	got, err := db.GetSession("session-uuid-123")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected session, got nil")
	}
	if got.MissionID != mission.ID {
		t.Errorf("expected MissionID %q, got %q", mission.ID, got.MissionID)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	db := openTestDB(t)

	got, err := db.GetSession("nonexistent")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for unknown session, got %v", got)
	}
}

func TestUpdateSessionScanResults(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	_, err = db.CreateSession(mission.ID, "session-uuid-456")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	err = db.UpdateSessionScanResults("session-uuid-456", "My Custom Title", "Auto summary text", 4096)
	if err != nil {
		t.Fatalf("UpdateSessionScanResults failed: %v", err)
	}

	got, err := db.GetSession("session-uuid-456")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got.CustomTitle != "My Custom Title" {
		t.Errorf("expected custom_title %q, got %q", "My Custom Title", got.CustomTitle)
	}
	if got.AutoSummary != "Auto summary text" {
		t.Errorf("expected auto_summary %q, got %q", "Auto summary text", got.AutoSummary)
	}
	if got.LastScannedOffset != 4096 {
		t.Errorf("expected last_scanned_offset 4096, got %d", got.LastScannedOffset)
	}
}

func TestListSessionsByMission(t *testing.T) {
	db := openTestDB(t)

	m1, err := db.CreateMission("github.com/owner/repo1", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	m2, err := db.CreateMission("github.com/owner/repo2", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	if _, err := db.CreateSession(m1.ID, "s1"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := db.CreateSession(m1.ID, "s2"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := db.CreateSession(m2.ID, "s3"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	sessions, err := db.ListSessionsByMission(m1.ID)
	if err != nil {
		t.Fatalf("ListSessionsByMission failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions for m1, got %d", len(sessions))
	}
}

func TestGetActiveSession(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	if _, err := db.CreateSession(mission.ID, "older-session"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := db.CreateSession(mission.ID, "newer-session"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Update the older session to make it "most recently modified"
	if err := db.UpdateSessionScanResults("older-session", "Updated", "", 100); err != nil {
		t.Fatalf("UpdateSessionScanResults failed: %v", err)
	}

	active, err := db.GetActiveSession(mission.ID)
	if err != nil {
		t.Fatalf("GetActiveSession failed: %v", err)
	}
	if active == nil {
		t.Fatal("expected active session, got nil")
	}
	if active.ID != "older-session" {
		t.Errorf("expected active session 'older-session', got %q", active.ID)
	}
}

func TestGetActiveSession_NoSessions(t *testing.T) {
	db := openTestDB(t)

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	active, err := db.GetActiveSession(mission.ID)
	if err != nil {
		t.Fatalf("GetActiveSession failed: %v", err)
	}
	if active != nil {
		t.Errorf("expected nil for mission with no sessions, got %v", active)
	}
}

func TestSessionsCascadeDeleteWithMission(t *testing.T) {
	db := openTestDB(t)

	// Enable foreign keys (SQLite has them off by default)
	if _, err := db.conn.Exec("PRAGMA foreign_keys = ON"); err != nil {
		t.Fatalf("failed to enable foreign keys: %v", err)
	}

	mission, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}

	if _, err := db.CreateSession(mission.ID, "s1"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if err := db.DeleteMission(mission.ID); err != nil {
		t.Fatalf("DeleteMission failed: %v", err)
	}

	got, err := db.GetSession("s1")
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected session to be cascade-deleted, but it still exists")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `make check`
Expected: FAIL — `CreateSession`, `GetSession`, etc. do not exist yet

**Step 3: Write the Session struct and CRUD functions**

Create `internal/database/sessions.go`:

```go
package database

import (
	"database/sql"
	"time"

	"github.com/mieubrisse/stacktrace"
)

// Session represents a row in the sessions table.
type Session struct {
	ID                string
	MissionID         string
	CustomTitle       string
	AutoSummary       string
	LastScannedOffset int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CreateSession inserts a new session row with the given ID and mission_id.
// Returns the created Session.
func (db *DB) CreateSession(missionID string, sessionID string) (*Session, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"INSERT INTO sessions (id, mission_id, created_at, updated_at) VALUES (?, ?, ?, ?)",
		sessionID, missionID, now, now,
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to insert session '%s'", sessionID)
	}

	return &Session{
		ID:        sessionID,
		MissionID: missionID,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}, nil
}

// GetSession returns a single session by ID, or (nil, nil) if not found.
func (db *DB) GetSession(sessionID string) (*Session, error) {
	row := db.conn.QueryRow(
		"SELECT id, mission_id, custom_title, auto_summary, last_scanned_offset, created_at, updated_at FROM sessions WHERE id = ?",
		sessionID,
	)

	s, err := scanSession(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get session '%s'", sessionID)
	}
	return s, nil
}

// UpdateSessionScanResults updates the custom_title, auto_summary, and
// last_scanned_offset for a session after an incremental JSONL scan.
// Only updates non-empty title/summary values (preserves existing values
// when the new scan found nothing new for that field).
func (db *DB) UpdateSessionScanResults(sessionID string, customTitle string, autoSummary string, lastScannedOffset int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		`UPDATE sessions SET
			custom_title = CASE WHEN ? != '' THEN ? ELSE custom_title END,
			auto_summary = CASE WHEN ? != '' THEN ? ELSE auto_summary END,
			last_scanned_offset = ?,
			updated_at = ?
		WHERE id = ?`,
		customTitle, customTitle, autoSummary, autoSummary, lastScannedOffset, now, sessionID,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update scan results for session '%s'", sessionID)
	}
	return nil
}

// ListSessionsByMission returns all sessions for a given mission,
// ordered by updated_at descending (most recently modified first).
func (db *DB) ListSessionsByMission(missionID string) ([]*Session, error) {
	rows, err := db.conn.Query(
		"SELECT id, mission_id, custom_title, auto_summary, last_scanned_offset, created_at, updated_at FROM sessions WHERE mission_id = ? ORDER BY updated_at DESC",
		missionID,
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list sessions for mission '%s'", missionID)
	}
	defer rows.Close()

	return scanSessions(rows)
}

// GetActiveSession returns the most recently modified session for a mission,
// or (nil, nil) if the mission has no sessions.
func (db *DB) GetActiveSession(missionID string) (*Session, error) {
	row := db.conn.QueryRow(
		"SELECT id, mission_id, custom_title, auto_summary, last_scanned_offset, created_at, updated_at FROM sessions WHERE mission_id = ? ORDER BY updated_at DESC LIMIT 1",
		missionID,
	)

	s, err := scanSession(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get active session for mission '%s'", missionID)
	}
	return s, nil
}

// scanSession scans a single session row.
func scanSession(row *sql.Row) (*Session, error) {
	var s Session
	var createdAt, updatedAt string
	if err := row.Scan(&s.ID, &s.MissionID, &s.CustomTitle, &s.AutoSummary, &s.LastScannedOffset, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &s, nil
}

// scanSessions scans multiple session rows from a query result.
func scanSessions(rows *sql.Rows) ([]*Session, error) {
	var sessions []*Session
	for rows.Next() {
		var s Session
		var createdAt, updatedAt string
		if err := rows.Scan(&s.ID, &s.MissionID, &s.CustomTitle, &s.AutoSummary, &s.LastScannedOffset, &createdAt, &updatedAt); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan session row")
		}
		s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		sessions = append(sessions, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating session rows")
	}
	return sessions, nil
}
```

**Step 4: Run tests to verify they pass**

Run: `make check`
Expected: PASS

**Step 5: Commit**

```
git add internal/database/sessions.go internal/database/sessions_test.go
git commit -m "Add Session struct and CRUD functions for sessions table"
```

---

### Task 3: Build the session scanner loop

**Files:**
- Create: `internal/server/session_scanner.go` — scanner loop and scan logic
- Modify: `internal/server/server.go` — launch the scanner goroutine

**Step 1: Write the session scanner**

Create `internal/server/session_scanner.go`:

```go
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
)

const (
	// sessionScannerInterval is how often the scanner checks for JSONL changes.
	sessionScannerInterval = 3 * time.Second
)

// jsonlGlobPattern returns the glob pattern for discovering all JSONL session
// files across all missions.
func jsonlGlobPattern(agencDirpath string) string {
	return filepath.Join(
		config.GetMissionsDirpath(agencDirpath),
		"*",
		claudeconfig.MissionClaudeConfigDirname,
		"projects",
		"*",
		"*.jsonl",
	)
}

// runSessionScannerLoop polls for JSONL file changes every 3 seconds and
// updates the sessions table with any newly discovered custom titles or
// auto-summaries.
func (s *Server) runSessionScannerLoop(ctx context.Context) {
	// Initial delay to avoid racing with startup I/O
	select {
	case <-ctx.Done():
		return
	case <-time.After(sessionScannerInterval):
		s.runSessionScannerCycle()
	}

	ticker := time.NewTicker(sessionScannerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runSessionScannerCycle()
		}
	}
}

// runSessionScannerCycle performs a single scan pass over all JSONL files.
func (s *Server) runSessionScannerCycle() {
	matches, err := filepath.Glob(jsonlGlobPattern(s.agencDirpath))
	if err != nil {
		s.logger.Printf("Session scanner: glob failed: %v", err)
		return
	}

	for _, jsonlFilepath := range matches {
		sessionID, missionID, ok := extractSessionAndMissionID(s.agencDirpath, jsonlFilepath)
		if !ok {
			continue
		}

		fileInfo, err := os.Stat(jsonlFilepath)
		if err != nil {
			continue
		}
		fileSize := fileInfo.Size()

		// Look up or create the session row
		sess, err := s.db.GetSession(sessionID)
		if err != nil {
			s.logger.Printf("Session scanner: failed to get session '%s': %v", sessionID, err)
			continue
		}
		if sess == nil {
			sess, err = s.db.CreateSession(missionID, sessionID)
			if err != nil {
				s.logger.Printf("Session scanner: failed to create session '%s': %v", sessionID, err)
				continue
			}
		}

		// Skip if no new data since last scan
		if fileSize <= sess.LastScannedOffset {
			continue
		}

		// Incremental scan from the last offset
		customTitle, autoSummary, err := scanJSONLFromOffset(jsonlFilepath, sess.LastScannedOffset)
		if err != nil {
			s.logger.Printf("Session scanner: failed to scan '%s': %v", jsonlFilepath, err)
			continue
		}

		// Track whether custom_title changed (for tmux reconciliation)
		customTitleChanged := customTitle != "" && customTitle != sess.CustomTitle
		autoSummaryChanged := autoSummary != "" && autoSummary != sess.AutoSummary

		if err := s.db.UpdateSessionScanResults(sessionID, customTitle, autoSummary, fileSize); err != nil {
			s.logger.Printf("Session scanner: failed to update session '%s': %v", sessionID, err)
			continue
		}

		// Trigger tmux reconciliation if something display-relevant changed
		if customTitleChanged || autoSummaryChanged {
			s.reconcileTmuxWindowTitle(missionID)
		}
	}
}

// extractSessionAndMissionID extracts the session UUID and mission UUID from a
// JSONL filepath. The expected path structure is:
//
//	<agencDirpath>/missions/<missionID>/claude-config/projects/<encoded-path>/<sessionID>.jsonl
//
// Returns (sessionID, missionID, true) on success, or ("", "", false) if the
// path doesn't match the expected structure.
func extractSessionAndMissionID(agencDirpath string, jsonlFilepath string) (sessionID string, missionID string, ok bool) {
	missionsDirpath := config.GetMissionsDirpath(agencDirpath)

	// Strip the missions directory prefix to get: <missionID>/claude-config/projects/<encoded-path>/<sessionID>.jsonl
	relPath, err := filepath.Rel(missionsDirpath, jsonlFilepath)
	if err != nil {
		return "", "", false
	}

	parts := strings.Split(relPath, string(filepath.Separator))
	// Expected: [missionID, "claude-config", "projects", encodedPath, "sessionID.jsonl"]
	// Minimum 5 parts
	if len(parts) < 5 {
		return "", "", false
	}

	missionID = parts[0]
	filename := parts[len(parts)-1]
	sessionID = strings.TrimSuffix(filename, ".jsonl")

	return sessionID, missionID, true
}

// jsonlMetadataEntry represents a metadata line in a session JSONL file.
// Covers both {"type":"summary"} and {"type":"custom-title"} entries.
type jsonlMetadataEntry struct {
	Type        string `json:"type"`
	Summary     string `json:"summary"`
	CustomTitle string `json:"customTitle"`
}

// scanJSONLFromOffset reads a JSONL file starting at the given byte offset and
// returns any custom-title and summary values found in the new data. Uses quick
// string matching before JSON parsing to avoid parsing every line.
func scanJSONLFromOffset(jsonlFilepath string, offset int64) (customTitle string, autoSummary string, err error) {
	file, err := os.Open(jsonlFilepath)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	if offset > 0 {
		if _, err := file.Seek(offset, 0); err != nil {
			return "", "", err
		}
	}

	scanner := bufio.NewScanner(file)
	// JSONL lines can be large (full conversation messages)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Quick string check: skip lines that can't contain metadata
		hasCustomTitle := strings.Contains(line, `"custom-title"`)
		hasSummary := strings.Contains(line, `"type":"summary"`)
		if !hasCustomTitle && !hasSummary {
			continue
		}

		var entry jsonlMetadataEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		switch entry.Type {
		case "custom-title":
			if entry.CustomTitle != "" {
				customTitle = entry.CustomTitle
			}
		case "summary":
			if entry.Summary != "" {
				autoSummary = entry.Summary
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return customTitle, autoSummary, err
	}

	return customTitle, autoSummary, nil
}
```

**Step 2: Launch the scanner goroutine in server.go**

In `internal/server/server.go`, add after the idle timeout goroutine block (after line 149):

```go
wg.Add(1)
go func() {
	defer wg.Done()
	s.runSessionScannerLoop(ctx)
}()
```

**Step 3: Build and verify**

Run: `make check`
Expected: PASS

**Step 4: Commit**

```
git add internal/server/session_scanner.go internal/server/server.go
git commit -m "Add session scanner loop for JSONL polling"
```

---

### Task 4: Add the tmux reconciliation function

**Files:**
- Create: `internal/server/tmux.go` — `reconcileTmuxWindowTitle` and tmux helpers

**Step 1: Write the tmux reconciliation function**

Create `internal/server/tmux.go`:

```go
package server

import (
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/odyssey/agenc/internal/database"
)

const (
	// maxWindowTitleLen is the maximum character length for tmux window titles.
	maxWindowTitleLen = 30
)

// reconcileTmuxWindowTitle examines all available data for a mission and
// converges the tmux window to the correct title. This function is idempotent
// and can be called from any context (scanner, summarizer, mission switch).
//
// Title priority (highest to lowest):
//  1. Active session's custom_title (from /rename)
//  2. Active session's auto_summary (from Claude or AgenC summarizer)
//  3. Repo short name (from git_repo)
//  4. Mission short ID (fallback)
func (s *Server) reconcileTmuxWindowTitle(missionID string) {
	// Step 1: Get the active session's metadata
	activeSession, err := s.db.GetActiveSession(missionID)
	if err != nil {
		s.logger.Printf("Tmux reconcile: failed to get active session for %s: %v", missionID, err)
		return
	}

	// Step 2: Get mission data for tmux_pane, tmux_window_title, git_repo
	mission, err := s.db.GetMission(missionID)
	if err != nil || mission == nil {
		return
	}

	// Step 3: Determine the best title
	bestTitle := determineBestTitle(activeSession, mission)

	// Step 4: Apply the title to tmux
	applyTmuxTitle(s, mission, bestTitle)
}

// determineBestTitle picks the best available title using the priority chain.
func determineBestTitle(activeSession *database.Session, mission *database.Mission) string {
	// Priority 1: custom_title from /rename
	if activeSession != nil && activeSession.CustomTitle != "" {
		return activeSession.CustomTitle
	}

	// Priority 2: auto_summary
	if activeSession != nil && activeSession.AutoSummary != "" {
		return activeSession.AutoSummary
	}

	// Priority 3: repo short name
	if mission.GitRepo != "" {
		repoName := extractRepoShortName(mission.GitRepo)
		if repoName != "" {
			return repoName
		}
	}

	// Priority 4: mission short ID
	return mission.ShortID
}

// applyTmuxTitle applies a title to the tmux window for a mission, subject to
// guards (sole pane, user override detection).
func applyTmuxTitle(s *Server, mission *database.Mission, title string) {
	// No tmux pane registered — mission is not running in tmux
	if mission.TmuxPane == nil || *mission.TmuxPane == "" {
		return
	}

	paneID := *mission.TmuxPane

	// Guard: only rename if this pane is the sole pane in its window
	if !isSolePaneInWindow(paneID) {
		return
	}

	// Guard: if we previously set a title and the current window name differs,
	// the user has manually renamed the window — respect that
	if mission.TmuxWindowTitle != "" {
		currentName := queryCurrentWindowName(paneID)
		if currentName != mission.TmuxWindowTitle {
			return
		}
	}

	truncatedTitle := truncateTitle(title, maxWindowTitleLen)

	// Skip if the title hasn't actually changed
	if truncatedTitle == mission.TmuxWindowTitle {
		return
	}

	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "rename-window", "-t", paneID, truncatedTitle).Run()

	if err := s.db.SetMissionTmuxWindowTitle(mission.ID, truncatedTitle); err != nil {
		s.logger.Printf("Tmux reconcile: failed to save window title for %s: %v", mission.ShortID, err)
	}
}

// isSolePaneInWindow returns true if the given pane is the only pane in its
// tmux window. Returns false if the window has multiple panes or if detection fails.
func isSolePaneInWindow(paneID string) bool {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{window_panes}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "1"
}

// queryCurrentWindowName returns the current name of the tmux window containing
// paneID. Returns "" if the query fails.
func queryCurrentWindowName(paneID string) string {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{window_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// truncateTitle truncates a string to maxLen runes, appending an ellipsis if
// truncation occurs. Collapses internal whitespace first.
func truncateTitle(title string, maxLen int) string {
	collapsed := strings.Join(strings.Fields(title), " ")
	if utf8.RuneCountInString(collapsed) <= maxLen {
		return collapsed
	}
	runes := []rune(collapsed)
	return string(runes[:maxLen-1]) + "…"
}

// extractRepoShortName extracts just the repository name from a canonical repo
// reference like "owner/repo" or "host/owner/repo". Returns just "repo".
func extractRepoShortName(gitRepo string) string {
	parts := strings.Split(gitRepo, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}
```

**Step 2: Build and verify**

Run: `make check`
Expected: PASS

**Step 3: Commit**

```
git add internal/server/tmux.go
git commit -m "Add idempotent tmux window title reconciliation"
```

---

### Task 5: Add tests for the scanner and reconciliation logic

**Files:**
- Create: `internal/server/session_scanner_test.go` — tests for path extraction and JSONL scanning

**Step 1: Write tests for extractSessionAndMissionID**

Create `internal/server/session_scanner_test.go`:

```go
package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractSessionAndMissionID(t *testing.T) {
	agencDirpath := "/home/user/.agenc"

	tests := []struct {
		name          string
		jsonlFilepath string
		wantSession   string
		wantMission   string
		wantOK        bool
	}{
		{
			name:          "standard path",
			jsonlFilepath: "/home/user/.agenc/missions/abc-123/claude-config/projects/encoded-path/session-uuid.jsonl",
			wantSession:   "session-uuid",
			wantMission:   "abc-123",
			wantOK:        true,
		},
		{
			name:          "too few path components",
			jsonlFilepath: "/home/user/.agenc/missions/abc-123/some.jsonl",
			wantSession:   "",
			wantMission:   "",
			wantOK:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sessionID, missionID, ok := extractSessionAndMissionID(agencDirpath, tt.jsonlFilepath)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v", ok, tt.wantOK)
			}
			if sessionID != tt.wantSession {
				t.Errorf("sessionID = %q, want %q", sessionID, tt.wantSession)
			}
			if missionID != tt.wantMission {
				t.Errorf("missionID = %q, want %q", missionID, tt.wantMission)
			}
		})
	}
}

func TestScanJSONLFromOffset(t *testing.T) {
	// Create a temp JSONL file with test data
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	content := `{"type":"message","role":"user","content":"hello"}
{"type":"summary","summary":"Working on auth system"}
{"type":"message","role":"assistant","content":"I'll help with that"}
{"type":"custom-title","customTitle":"Auth Feature"}
`
	if err := os.WriteFile(jsonlFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	// Scan from offset 0
	customTitle, autoSummary, err := scanJSONLFromOffset(jsonlFilepath, 0)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if customTitle != "Auth Feature" {
		t.Errorf("customTitle = %q, want %q", customTitle, "Auth Feature")
	}
	if autoSummary != "Working on auth system" {
		t.Errorf("autoSummary = %q, want %q", autoSummary, "Working on auth system")
	}
}

func TestScanJSONLFromOffset_IncrementalScan(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	// Write initial content
	initialContent := `{"type":"summary","summary":"Initial summary"}
{"type":"message","role":"user","content":"hello"}
`
	if err := os.WriteFile(jsonlFilepath, []byte(initialContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	initialSize := int64(len(initialContent))

	// Append new content
	appendedContent := `{"type":"custom-title","customTitle":"New Title"}
`
	f, err := os.OpenFile(jsonlFilepath, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatalf("failed to open file for append: %v", err)
	}
	if _, err := f.WriteString(appendedContent); err != nil {
		f.Close()
		t.Fatalf("failed to append: %v", err)
	}
	f.Close()

	// Scan from the initial size offset — should only see the appended data
	customTitle, autoSummary, err := scanJSONLFromOffset(jsonlFilepath, initialSize)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if customTitle != "New Title" {
		t.Errorf("customTitle = %q, want %q", customTitle, "New Title")
	}
	if autoSummary != "" {
		t.Errorf("autoSummary = %q, want empty (initial summary is before offset)", autoSummary)
	}
}

func TestScanJSONLFromOffset_NoMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	jsonlFilepath := filepath.Join(tmpDir, "test-session.jsonl")

	content := `{"type":"message","role":"user","content":"hello"}
{"type":"message","role":"assistant","content":"hi there"}
`
	if err := os.WriteFile(jsonlFilepath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	customTitle, autoSummary, err := scanJSONLFromOffset(jsonlFilepath, 0)
	if err != nil {
		t.Fatalf("scanJSONLFromOffset failed: %v", err)
	}
	if customTitle != "" {
		t.Errorf("customTitle = %q, want empty", customTitle)
	}
	if autoSummary != "" {
		t.Errorf("autoSummary = %q, want empty", autoSummary)
	}
}

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		title  string
		maxLen int
		want   string
	}{
		{"short", 30, "short"},
		{"this is a very long title that exceeds the maximum length", 30, "this is a very long title tha…"},
		{"  lots   of    whitespace  ", 30, "lots of whitespace"},
	}

	for _, tt := range tests {
		got := truncateTitle(tt.title, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncateTitle(%q, %d) = %q, want %q", tt.title, tt.maxLen, got, tt.want)
		}
	}
}

func TestExtractRepoShortName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"owner/repo", "repo"},
		{"github.com/owner/repo", "repo"},
		{"repo", "repo"},
		{"", ""},
	}

	for _, tt := range tests {
		got := extractRepoShortName(tt.input)
		if got != tt.want {
			t.Errorf("extractRepoShortName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestDetermineBestTitle(t *testing.T) {
	mission := &database.Mission{
		ShortID: "abc12345",
		GitRepo: "github.com/owner/my-project",
	}

	// With custom title — highest priority
	sessionWithCustom := &database.Session{
		CustomTitle: "My Custom Name",
		AutoSummary: "Auto generated summary",
	}
	if got := determineBestTitle(sessionWithCustom, mission); got != "My Custom Name" {
		t.Errorf("with custom title: got %q, want %q", got, "My Custom Name")
	}

	// Without custom title, with auto summary
	sessionWithSummary := &database.Session{
		AutoSummary: "Auto generated summary",
	}
	if got := determineBestTitle(sessionWithSummary, mission); got != "Auto generated summary" {
		t.Errorf("with auto summary: got %q, want %q", got, "Auto generated summary")
	}

	// No session — falls back to repo name
	if got := determineBestTitle(nil, mission); got != "my-project" {
		t.Errorf("no session: got %q, want %q", got, "my-project")
	}

	// No session, no repo — falls back to short ID
	missionNoRepo := &database.Mission{ShortID: "abc12345"}
	if got := determineBestTitle(nil, missionNoRepo); got != "abc12345" {
		t.Errorf("no session, no repo: got %q, want %q", got, "abc12345")
	}
}
```

Note: The `TestDetermineBestTitle` test imports `database` from the same module. Add the import at the top:

```go
import (
	"github.com/odyssey/agenc/internal/database"
)
```

**Step 2: Run tests to verify they pass**

Run: `make check`
Expected: PASS

**Step 3: Commit**

```
git add internal/server/session_scanner_test.go
git commit -m "Add tests for session scanner and tmux reconciliation"
```

---

### Task 6: Update architecture doc

**Files:**
- Modify: `docs/system-architecture.md` — document sessions table, scanner loop, tmux reconciliation

**Step 1: Read the current architecture doc**

Read `docs/system-architecture.md` to find the relevant sections for:
- Database schema (add sessions table)
- Server goroutines (add session scanner loop)
- Cross-cutting patterns (add tmux title reconciliation)

**Step 2: Add sessions table to the database schema section**

Add the sessions table description near the existing missions table documentation.

**Step 3: Add session scanner to the server goroutines section**

Document the new `runSessionScannerLoop` goroutine alongside the existing loops (repo update, config auto-commit, config watcher, keybindings writer, mission summarizer, idle timeout).

**Step 4: Add tmux title reconciliation to cross-cutting patterns**

Document the `reconcileTmuxWindowTitle` function, its priority chain, and its idempotent nature.

**Step 5: Build and verify**

Run: `make check`
Expected: PASS (no code changes, just docs)

**Step 6: Commit**

```
git add docs/system-architecture.md
git commit -m "Document sessions table, scanner loop, and tmux reconciliation in architecture doc"
```

---

### Task 7: Final integration verification

**Step 1: Run full test suite**

Run: `make check`
Expected: PASS — all tests including new session CRUD and scanner tests

**Step 2: Build the binary**

Run: `make build`
Expected: Binary compiles successfully with no errors

**Step 3: Verify migration runs on existing DB**

This can be verified by running the binary against an existing AgenC installation — the `Open()` function will run the new migration idempotently.

**Step 4: Final commit (if any remaining unstaged changes)**

Review `git status` for any remaining changes and commit them.
