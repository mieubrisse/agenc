JSONL-Based Idle Detection — Implementation Plan
==================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace prompt-submission-based idle detection with JSONL file modification time, so long-running Claude work is not mistaken for idleness.

**Architecture:** The server's idle timeout cycle stats the most recently modified JSONL conversation log for each mission. A new exported function in `internal/session/` combines project directory lookup with JSONL file discovery. The idle timeout function uses this instead of database timestamps.

**Tech Stack:** Go, os.Stat

**Design doc:** `docs/plans/2026-02-25-jsonl-idle-detection-design.md`

---

### Task 1: Add `FindActiveJSONLPath` to session package

**Files:**
- Modify: `internal/session/session.go` (append new exported function after `TailJSONLFile`, ~line 326)

**Step 1: Add the exported function**

Append at the end of `internal/session/session.go`:

```go
// FindActiveJSONLPath returns the filesystem path of the most recently modified
// JSONL conversation log for the given mission, or "" if none exists. This is
// used by the idle timeout system to check whether Claude is actively working.
func FindActiveJSONLPath(claudeConfigDirpath string, missionID string) string {
	projectDirpath := findProjectDirpath(claudeConfigDirpath, missionID)
	if projectDirpath == "" {
		return ""
	}
	return findMostRecentJSONL(projectDirpath)
}
```

This reuses the existing unexported `findProjectDirpath` (line 78) and `findMostRecentJSONL` (line 139).

**Step 2: Verify it compiles**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Expected: Build succeeds.

**Step 3: Commit**

```bash
git add internal/session/session.go
git commit -m "Add FindActiveJSONLPath for JSONL-based idle detection"
```

---

### Task 2: Rewrite `missionIdleDuration` to use JSONL mtime

**Files:**
- Modify: `internal/server/idle_timeout.go:1-9` (imports) and `internal/server/idle_timeout.go:94-105` (`missionIdleDuration` function)

**Step 1: Update imports**

Add `"os"` to the standard library imports, and add `"github.com/odyssey/agenc/internal/claudeconfig"` and `"github.com/odyssey/agenc/internal/session"` to the project imports. Remove the `"github.com/odyssey/agenc/internal/config"` import (no longer used). The import block should become:

```go
import (
	"context"
	"os"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/session"
)
```

**Step 2: Rewrite `missionIdleDuration`**

Replace the entire `missionIdleDuration` function (lines 94-105) with:

```go
// missionIdleDuration returns how long a mission has been idle by checking the
// modification time of the active JSONL conversation log. Claude Code writes to
// this file whenever it does anything (streaming, tool calls, thinking), so a
// recently modified file means Claude is actively working.
//
// Falls back to created_at if the JSONL file cannot be located (mission has no
// session yet, or the project directory doesn't exist).
func (s *Server) missionIdleDuration(m *database.Mission, now time.Time) time.Duration {
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(s.agencDirpath, m.ID)
	jsonlFilepath := session.FindActiveJSONLPath(claudeConfigDirpath, m.ID)
	if jsonlFilepath != "" {
		if info, err := os.Stat(jsonlFilepath); err == nil {
			return now.Sub(info.ModTime())
		}
	}
	return now.Sub(m.CreatedAt)
}
```

**Step 3: Verify the `config` import is no longer needed**

Check that `config.GetMissionPIDFilepath` (used in `isWrapperRunning`) is the only usage of the `config` package in this file. It is — `isWrapperRunning` uses it at line 86. So we still need `config`. Update the import block to keep it:

```go
import (
	"context"
	"os"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/session"
)
```

**Step 4: Verify it compiles**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Expected: Build succeeds.

**Step 5: Manual smoke test**

1. Check the daemon logs (`~/.agenc/daemon.log` or similar) for idle timeout messages
2. Start a mission that is actively working — confirm it is NOT killed after 30 minutes
3. Let a mission sit idle at the prompt — confirm it IS killed after 30 minutes
4. Verify a mission with no JSONL file yet (freshly created) falls back to `created_at`

**Step 6: Commit**

```bash
git add internal/server/idle_timeout.go
git commit -m "Use JSONL file mtime for idle detection instead of last_active timestamp"
```
