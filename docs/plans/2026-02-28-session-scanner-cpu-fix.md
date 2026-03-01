# Session Scanner CPU Fix Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Fix 100% CPU usage in the agenc server caused by the session scanner globbing across all 753 mission directories every 3 seconds.

**Architecture:** Replace the broad `filepath.Glob` with targeted lookups for only missions currently running in the tmux pool. Discover running missions by listing live panes in `agenc-pool`, resolve each pane to a mission via the database, compute the project directory path directly, and scan only those JSONL files.

**Tech Stack:** Go, tmux CLI, SQLite

**Design doc:** `docs/plans/2026-02-28-session-scanner-cpu-fix-design.md`

---

### Task 1: Extract path encoding helper from `ProjectDirectoryExists`

**Files:**
- Modify: `internal/claudeconfig/build.go:719-733`
- Modify: `internal/claudeconfig/build_test.go` (add test)

**Step 1: Write the failing test**

Add to `internal/claudeconfig/build_test.go`:

```go
func TestComputeProjectDirpath(t *testing.T) {
	result, err := ComputeProjectDirpath("/Users/odyssey/.agenc/missions/abc-123/agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, ".claude", "projects", "-Users-odyssey--agenc-missions-abc-123-agent")
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/claudeconfig/ -run TestComputeProjectDirpath -v`
Expected: FAIL — `ComputeProjectDirpath` is not defined.

**Step 3: Write the implementation**

In `internal/claudeconfig/build.go`, add the new function and refactor `ProjectDirectoryExists` to delegate:

```go
// ComputeProjectDirpath returns the absolute path to the Claude Code project
// directory for the given agent directory path. Claude Code transforms absolute
// paths into project directory names by converting both slashes and dots to
// hyphens.
// For example: /Users/name/.config/path -> ~/.claude/projects/-Users-name--config-path
func ComputeProjectDirpath(agentDirpath string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	projectDirName := strings.ReplaceAll(strings.ReplaceAll(agentDirpath, "/", "-"), ".", "-")
	return filepath.Join(homeDir, ".claude", "projects", projectDirName), nil
}

// ProjectDirectoryExists checks whether Claude Code has created a project
// directory for the given agent directory path.
func ProjectDirectoryExists(agentDirpath string) bool {
	projectDirpath, err := ComputeProjectDirpath(agentDirpath)
	if err != nil {
		return false
	}
	_, err = os.Stat(projectDirpath)
	return err == nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/claudeconfig/ -run TestComputeProjectDirpath -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/claudeconfig/build.go internal/claudeconfig/build_test.go
git commit -m "Extract ComputeProjectDirpath from ProjectDirectoryExists"
```

---

### Task 2: Add `listPoolPaneIDs` helper to `pool.go`

**Files:**
- Modify: `internal/server/pool.go`

**Step 1: Write the failing test**

No unit test for this function — it calls `tmux` directly (same pattern as `getLinkedPaneIDs`, `poolWindowExists`, `poolSessionExists` which are all untested). Integration testing is covered by Task 5.

**Step 2: Write the implementation**

Add to `internal/server/pool.go`:

```go
// listPoolPaneIDs returns the pane IDs (without "%" prefix) of all panes
// currently running in the agenc-pool tmux session. Returns an empty slice
// if the pool doesn't exist or tmux is not running.
func listPoolPaneIDs() []string {
	cmd := exec.Command("tmux", "list-panes", "-t", poolSessionName, "-F", "#{pane_id}")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var paneIDs []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip the "%" prefix — DB stores pane IDs without it
		paneIDs = append(paneIDs, strings.TrimPrefix(line, "%"))
	}
	return paneIDs
}
```

**Step 3: Build to verify compilation**

Run: `go build ./internal/server/`
Expected: builds successfully

**Step 4: Commit**

```bash
git add internal/server/pool.go
git commit -m "Add listPoolPaneIDs helper for session scanner"
```

---

### Task 3: Rewrite `runSessionScannerCycle`

**Files:**
- Modify: `internal/server/session_scanner.go`

**Step 1: Rewrite the cycle function**

Replace `runSessionScannerCycle` and delete `buildJSONLGlobPattern` and `extractSessionAndMissionID`:

```go
// runSessionScannerCycle scans JSONL files for missions currently running in
// the tmux pool. For each running mission, it computes the project directory
// path directly (no glob), lists JSONL files in that directory, and
// incrementally scans for metadata changes.
func (s *Server) runSessionScannerCycle() {
	paneIDs := listPoolPaneIDs()

	for _, paneID := range paneIDs {
		mission, err := s.db.GetMissionByTmuxPane(paneID)
		if err != nil {
			s.logger.Printf("Session scanner: failed to resolve pane '%s': %v", paneID, err)
			continue
		}
		if mission == nil {
			continue
		}

		agentDirpath := config.GetMissionAgentDirpath(s.agencDirpath, mission.ID)
		projectDirpath, err := claudeconfig.ComputeProjectDirpath(agentDirpath)
		if err != nil {
			s.logger.Printf("Session scanner: failed to compute project dir for mission '%s': %v", database.ShortID(mission.ID), err)
			continue
		}

		s.scanMissionJSONLFiles(mission.ID, projectDirpath)
	}
}

// scanMissionJSONLFiles scans all JSONL files in a mission's project directory
// for metadata changes (custom titles, auto summaries).
func (s *Server) scanMissionJSONLFiles(missionID string, projectDirpath string) {
	entries, err := os.ReadDir(projectDirpath)
	if err != nil {
		// Directory may not exist yet (mission hasn't started a session)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		jsonlFilepath := filepath.Join(projectDirpath, entry.Name())

		fileInfo, err := entry.Info()
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

		// Track whether display-relevant data changed (for tmux reconciliation)
		customTitleChanged := customTitle != "" && customTitle != sess.CustomTitle
		autoSummaryChanged := autoSummary != "" && autoSummary != sess.AutoSummary

		if err := s.db.UpdateSessionScanResults(sessionID, customTitle, autoSummary, fileSize); err != nil {
			s.logger.Printf("Session scanner: failed to update session '%s': %v", sessionID, err)
			continue
		}

		// Reconcile tmux window title when display-relevant data changes
		if customTitleChanged || autoSummaryChanged {
			s.reconcileTmuxWindowTitle(missionID)
		}
	}
}
```

Also update the imports at the top of the file. The new imports needed are:

```go
import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)
```

Remove `"context"` from imports (no longer used in this file — `runSessionScannerLoop` uses it but it's already imported via the `context` reference in the function signature).

Actually, `context` IS used by `runSessionScannerLoop`. Keep it. Just add `"github.com/odyssey/agenc/internal/database"`.

**Step 2: Build to verify compilation**

Run: `go build ./internal/server/`
Expected: builds successfully

**Step 3: Commit**

```bash
git add internal/server/session_scanner.go
git commit -m "Rewrite session scanner to only scan running missions"
```

---

### Task 4: Update tests

**Files:**
- Modify: `internal/server/session_scanner_test.go`

**Step 1: Remove `TestExtractSessionAndMissionID`**

Delete the entire `TestExtractSessionAndMissionID` function (lines 12-59) — `extractSessionAndMissionID` no longer exists. Also remove the `"github.com/odyssey/agenc/internal/database"` import if it was only used by that test.

**Step 2: Verify remaining tests still pass**

The `TestScanJSONLFromOffset*`, `TestTruncateTitle`, `TestExtractRepoShortName`, and `TestDetermineBestTitle` tests should all still pass since their underlying functions are unchanged.

Run: `go test ./internal/server/ -v`
Expected: All remaining tests PASS

**Step 3: Commit**

```bash
git add internal/server/session_scanner_test.go
git commit -m "Remove test for deleted extractSessionAndMissionID"
```

---

### Task 5: Full build and verification

**Files:** None (verification only)

**Step 1: Run the full build**

Run: `make build`
Expected: All checks pass, binary builds successfully

**Step 2: Verify the fix works**

Run the built binary as a server and confirm CPU usage stays low:

```bash
./agenc server stop
./agenc server start
```

Then check CPU after a few seconds:

```bash
ps aux | grep "agenc server" | grep -v grep
```

Expected: CPU% should be near 0%, not 99%.

**Step 3: Commit (if any cleanup needed)**

No commit needed if Tasks 1-4 are clean.

---

### Task 6: Update architecture documentation

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Update the session scanner loop description**

Find the session scanner description (section "8. Session scanner loop") and update it to reflect the new behavior:

Before:
> - Globs for JSONL files under `missions/*/claude-config/projects/*/*.jsonl`

After:
> - Queries the tmux pool for currently running missions via `listPoolPaneIDs()`
> - Resolves each pane ID to a mission via the database
> - For each running mission, computes the project directory path directly and lists JSONL files in that single directory (no glob across all missions)

Remove any references to `extractSessionAndMissionID` if present.

**Step 2: Commit**

```bash
git add docs/system-architecture.md
git commit -m "Update architecture doc for session scanner CPU fix"
```
