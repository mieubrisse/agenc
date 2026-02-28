Server-Side Mission Launch Implementation Plan
================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make all mission wrappers run in tmux pool windows via the server, eliminating in-process wrapper execution from the CLI.

**Architecture:** The CLI detects the current tmux session (if any) and passes it to the server. The server spawns wrapper processes in pool windows and optionally links them into the caller's tmux session. The CLI never runs `wrapper.Run()` directly.

**Tech Stack:** Go, Cobra CLI, tmux, Unix sockets (HTTP client/server)

**Design doc:** `docs/plans/2026-02-28-server-side-mission-launch-design.md`

---

### Task 1: Add tmux session detection helper

**Files:**
- Modify: `cmd/mission_helpers.go`

**Step 1: Add `getCurrentTmuxSessionName` function**

Add to `cmd/mission_helpers.go` after the existing `lookupWindowTitle` function (after line 174):

```go
// getCurrentTmuxSessionName returns the name of the tmux session the caller
// is running in. Returns an empty string if not inside tmux.
func getCurrentTmuxSessionName() string {
	if os.Getenv("TMUX") == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
```

Add `"os"` to the import block (it already imports `"os/exec"` and `"strings"`).

**Step 2: Verify it compiles**

Run: `make check`

**Step 3: Commit**

```
git add cmd/mission_helpers.go
git commit -m "Add tmux session detection helper"
```

---

### Task 2: Add `AttachMission` client method

**Files:**
- Modify: `internal/server/client.go`

**Step 1: Add `AttachMission` method**

Add to `internal/server/client.go` after the `ReloadMission` method (after line 237):

```go
// AttachMission ensures the mission's wrapper is running in the pool and links
// the pool window into the given tmux session.
func (c *Client) AttachMission(id string, tmuxSession string) error {
	body := AttachRequest{TmuxSession: tmuxSession}
	return c.Post("/missions/"+id+"/attach", body, nil)
}
```

This uses the existing `AttachRequest` struct already defined in `internal/server/missions.go:521-524`.

**Step 2: Verify it compiles**

Run: `make check`

**Step 3: Commit**

```
git add internal/server/client.go
git commit -m "Add AttachMission client method"
```

---

### Task 3: Fix `spawnWrapper` to allow pool-only (no link)

**Files:**
- Modify: `internal/server/missions.go:280-337`

**Step 1: Change `spawnWrapper` to handle empty `TmuxSession` for non-headless missions**

Currently lines 312-316 error when `TmuxSession` is empty:

```go
// Interactive: spawn wrapper in the pool, then link into caller's session
tmuxSession := req.TmuxSession
if tmuxSession == "" {
    return fmt.Errorf("tmux_session is required for interactive missions")
}
```

Replace the entire non-headless branch (lines 312-336) with:

```go
// Spawn wrapper in the pool
resumeCmd := fmt.Sprintf("'%s' mission resume %s", agencBinpath, missionRecord.ID)
if req.Prompt != "" {
    resumeCmd += fmt.Sprintf(" --prompt '%s'", strings.ReplaceAll(req.Prompt, "'", "'\\''"))
}

poolWindowTarget, err := s.createPoolWindow(missionRecord.ID, resumeCmd)
if err != nil {
    return fmt.Errorf("failed to create pool window: %w", err)
}

// Link the pool window into the caller's session (if provided)
tmuxSession := req.TmuxSession
if tmuxSession != "" {
    if err := linkPoolWindow(poolWindowTarget, tmuxSession); err != nil {
        s.destroyPoolWindow(missionRecord.ID)
        return fmt.Errorf("failed to link pool window: %w", err)
    }
}
```

This also replaces the headless path (lines 286-310) which forked a detached `agenc mission resume --headless` process. All missions now run in pool windows. Remove the `if req.Headless { ... }` block and the `syscall` import if no longer needed.

Check if `syscall` is still used elsewhere in the file. The `stopWrapper` function (line 405) uses `syscall.SIGTERM` and `syscall.SIGKILL`, so `syscall` stays in the import.

**Step 2: Verify it compiles**

Run: `make check`

**Step 3: Commit**

```
git add internal/server/missions.go
git commit -m "Allow spawnWrapper to create pool-only windows without linking"
```

---

### Task 4: Refactor `mission new` CLI to use server-only flow

**Files:**
- Modify: `cmd/mission_new.go`
- Modify: `cmd/command_str_consts.go`

**Step 1: Add `--focus` flag**

Add the flag variable alongside the existing flags (near line 24):

```go
var focusFlag bool
```

Add flag name constant to `cmd/command_str_consts.go` in the mission new flags section (after line 91):

```go
focusFlagName    = "focus"
```

Register the flag in the `init()` function (near line 51):

```go
missionNewCmd.Flags().BoolVar(&focusFlag, focusFlagName, false, "focus the new mission's tmux window after creation")
```

**Step 2: Rewrite `createAndLaunchMission`**

Replace the function body (lines 344-379) with:

```go
func createAndLaunchMission(
	agencDirpath string,
	gitRepoName string,
	initialPrompt string,
) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	// Detect tmux session — omit if headless flag is set
	tmuxSession := ""
	if !headlessFlag {
		tmuxSession = getCurrentTmuxSessionName()
	}

	missionRecord, err := client.CreateMission(server.CreateMissionRequest{
		Repo:        gitRepoName,
		Prompt:      initialPrompt,
		TmuxSession: tmuxSession,
		CronID:      cronIDFlag,
		CronName:    cronNameFlag,
		Timeout:     timeoutFlag,
	})
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission")
	}

	fmt.Printf("Created mission: %s\n", missionRecord.ShortID)

	if tmuxSession != "" {
		fmt.Println("Launched in tmux pool")
		if focusFlag {
			focusMissionWindow(missionRecord.ShortID, tmuxSession)
		}
	} else {
		fmt.Println("Running in background (pool window)")
	}

	return nil
}
```

**Step 3: Add `focusMissionWindow` helper**

Add to `cmd/mission_helpers.go`:

```go
// focusMissionWindow switches the tmux focus to the window for the given
// mission short ID in the specified session.
func focusMissionWindow(shortID string, tmuxSession string) {
	target := fmt.Sprintf("%s:%s", tmuxSession, shortID)
	exec.Command("tmux", "select-window", "-t", target).Run()
}
```

Add `"fmt"` to the import block in `cmd/mission_helpers.go` if not already present.

**Step 4: Rewrite `createAndLaunchAdjutantMission`**

Replace the function body (lines 227-246) with:

```go
func createAndLaunchAdjutantMission(agencDirpath string, initialPrompt string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	tmuxSession := ""
	if !headlessFlag {
		tmuxSession = getCurrentTmuxSessionName()
	}

	missionRecord, err := client.CreateMission(server.CreateMissionRequest{
		Adjutant:    true,
		Prompt:      initialPrompt,
		TmuxSession: tmuxSession,
	})
	if err != nil {
		return stacktrace.Propagate(err, "failed to create adjutant mission")
	}

	fmt.Printf("Created Adjutant mission: %s\n", missionRecord.ShortID)

	if tmuxSession != "" {
		fmt.Println("Launched in tmux pool")
		if focusFlag {
			focusMissionWindow(missionRecord.ShortID, tmuxSession)
		}
	} else {
		fmt.Println("Running in background (pool window)")
	}

	return nil
}
```

**Step 5: Rewrite `runMissionNewWithClone`**

Replace the function body (lines 134-161) with:

```go
func runMissionNewWithClone() error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	sourceMission, err := client.GetMission(cloneFlag)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get source mission")
	}

	tmuxSession := ""
	if !headlessFlag {
		tmuxSession = getCurrentTmuxSessionName()
	}

	missionRecord, err := client.CreateMission(server.CreateMissionRequest{
		Repo:        sourceMission.GitRepo,
		Prompt:      promptFlag,
		CloneFrom:   sourceMission.ID,
		TmuxSession: tmuxSession,
	})
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission")
	}

	fmt.Printf("Created mission: %s (cloned from %s)\n", missionRecord.ShortID, sourceMission.ShortID)
	fmt.Printf("Mission directory: %s\n", config.GetMissionDirpath(agencDirpath, missionRecord.ID))

	if tmuxSession != "" {
		fmt.Println("Launched in tmux pool")
		if focusFlag {
			focusMissionWindow(missionRecord.ShortID, tmuxSession)
		}
	} else {
		fmt.Println("Running in background (pool window)")
	}

	return nil
}
```

**Step 6: Remove the `wrapper` import**

Remove the `wrapper` import from `cmd/mission_new.go` line 17:

```go
"github.com/odyssey/agenc/internal/wrapper"
```

Also remove the `lookupWindowTitle` calls that are no longer needed — the server has its own `lookupWindowTitle` method and handles window naming via the pool.

**Step 7: Verify it compiles**

Run: `make check`

**Step 8: Commit**

```
git add cmd/mission_new.go cmd/mission_helpers.go cmd/command_str_consts.go
git commit -m "Refactor mission new to delegate all wrapper management to server"
```

---

### Task 5: Refactor `mission resume` CLI to use attach endpoint

**Files:**
- Modify: `cmd/mission_resume.go`

**Step 1: Add `--focus` flag to resume command**

Add at the top of the file alongside the command definition:

```go
var resumeFocusFlag bool
```

Register in `init()`:

```go
missionResumeCmd.Flags().BoolVar(&resumeFocusFlag, focusFlagName, false, "focus the mission's tmux window after attaching")
```

**Step 2: Rewrite `resumeMission`**

Replace the function body (lines 90-139) with:

```go
func resumeMission(client *server.Client, missionID string) error {
	tmuxSession := getCurrentTmuxSessionName()
	if tmuxSession == "" {
		return stacktrace.NewError("mission resume requires tmux; run inside a tmux session")
	}

	missionRecord, err := client.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	if missionRecord.Status == "archived" {
		if err := client.UnarchiveMission(missionID); err != nil {
			return stacktrace.Propagate(err, "failed to unarchive mission")
		}
		fmt.Printf("Unarchived mission: %s\n", database.ShortID(missionID))
	}

	// Migrate old .assistant marker if present
	if err := config.MigrateAssistantMarkerIfNeeded(agencDirpath, missionID); err != nil {
		return stacktrace.Propagate(err, "failed to migrate assistant marker")
	}

	fmt.Printf("Resuming mission: %s\n", database.ShortID(missionID))

	if err := client.AttachMission(missionID, tmuxSession); err != nil {
		return stacktrace.Propagate(err, "failed to attach mission")
	}

	if resumeFocusFlag {
		focusMissionWindow(missionRecord.ShortID, tmuxSession)
	}

	return nil
}
```

**Step 3: Remove dead imports**

Remove imports that are no longer needed from `cmd/mission_resume.go`:
- `"os"` (was used for `os.Stat`)
- `"github.com/odyssey/agenc/internal/claudeconfig"` (was used for `GetLastSessionID`)
- `"github.com/odyssey/agenc/internal/wrapper"` (was used for `NewWrapper`)

Keep:
- `"fmt"`
- `"strings"`
- `"github.com/mieubrisse/stacktrace"`
- `"github.com/spf13/cobra"`
- `"github.com/odyssey/agenc/internal/config"` (for `MigrateAssistantMarkerIfNeeded`)
- `"github.com/odyssey/agenc/internal/database"` (for `ShortID`)
- `"github.com/odyssey/agenc/internal/server"` (for `Client`)

Also remove the `missionHasConversation` function (lines 144-147) — no longer called.

**Step 4: Remove the PID check**

The old `resumeMission` checked if the wrapper was already running (lines 109-116). This is now handled server-side by `ensureWrapperInPool` which is idempotent — if the wrapper is already running, it just links the window. Remove this check.

**Step 5: Verify it compiles**

Run: `make check`

**Step 6: Commit**

```
git add cmd/mission_resume.go
git commit -m "Refactor mission resume to use server attach endpoint"
```

---

### Task 6: Manual verification

**Step 1: Build**

Run: `make build`

**Step 2: Test mission new in tmux**

From inside a tmux session:

```
./agenc mission new --blank --focus
```

Expected: Mission created, pool window created and linked, focus switches to it.

**Step 3: Test mission new outside tmux**

From a plain terminal:

```
./agenc mission new --blank
```

Expected: Mission created, pool window created, no link. Output says "Running in background".

**Step 4: Test mission new --headless inside tmux**

```
./agenc mission new --blank --headless
```

Expected: Mission created, pool window created, no link (despite being in tmux).

**Step 5: Test mission resume in tmux**

Stop a mission, then:

```
./agenc mission resume <short-id> --focus
```

Expected: Wrapper starts in pool, window linked and focused.

**Step 6: Test mission resume outside tmux**

```
./agenc mission resume <short-id>
```

Expected: Error message saying resume requires tmux.

**Step 7: Commit final state if any fixups were needed**

```
git add -A
git commit -m "Fix issues found during manual verification"
```
