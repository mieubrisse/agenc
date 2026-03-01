Server-Side Tmux Pane ID Capture Implementation Plan
=====================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move tmux pane ID capture from the wrapper to the server, eliminating the wrapper→server registration round-trip.

**Architecture:** The server's `createPoolWindow` captures the pane ID at window-creation time using `tmux new-window -P -F '#{pane_id}'`. Both callers (`spawnWrapper`, `ensureWrapperInPool`) store the pane ID in the DB and trigger title reconciliation. The `TmuxPane` field is removed from the PATCH API, and the wrapper's `registerTmuxPane`/`clearTmuxPane` are deleted.

**Tech Stack:** Go, tmux CLI, SQLite

**Design doc:** `docs/plans/2026-03-01-server-side-pane-capture-design.md`

---

Task 1: Update `createPoolWindow` to capture and return pane ID
----------------------------------------------------------------

**Files:**
- Modify: `internal/server/pool.go:44-62` (`createPoolWindow`)

**Step 1: Modify `createPoolWindow` to add `-P -F '#{pane_id}'` and return the pane ID**

The `-P` flag tells `tmux new-window` to print pane info on stdout. `-F '#{pane_id}'` formats that output as just the pane ID (e.g. `%42`).

Change the return signature from `(string, error)` to `(string, string, error)` — `(windowTarget, paneID, error)`.

```go
func (s *Server) createPoolWindow(missionID string, command string) (string, string, error) {
	if err := s.ensurePoolSession(); err != nil {
		return "", "", err
	}

	windowName := database.ShortID(missionID)
	target := fmt.Sprintf("%s:", poolSessionName)

	cmd := exec.Command("tmux", "new-window", "-d", "-P", "-F", "#{pane_id}", "-t", target, "-n", windowName, command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", stacktrace.NewError("failed to create pool window: %v (output: %s)", err, string(output))
	}

	// Parse pane ID from output (e.g. "%42") and strip the "%" prefix
	// to match the DB convention (database stores "42", not "%42").
	paneID := strings.TrimSpace(string(output))
	paneID = strings.TrimPrefix(paneID, "%")

	// Return the full target for linking: "agenc-pool:<windowName>"
	windowTarget := fmt.Sprintf("%s:%s", poolSessionName, windowName)
	s.logger.Printf("Created pool window %s (pane %s) for mission %s", windowTarget, paneID, database.ShortID(missionID))
	return windowTarget, paneID, nil
}
```

**Step 2: Run `make check` to verify compilation fails**

Run: `make check`
Expected: Compilation errors in `spawnWrapper` and `ensureWrapperInPool` because `createPoolWindow` now returns 3 values.

**Step 3: Commit**

```
git add internal/server/pool.go
git commit -m "Add pane ID capture to createPoolWindow via -P -F flags"
```

---

Task 2: Wire pane ID through `spawnWrapper`
--------------------------------------------

**Files:**
- Modify: `internal/server/missions.go:311-341` (`spawnWrapper`)

**Step 1: Update `spawnWrapper` to receive pane ID and store it**

```go
func (s *Server) spawnWrapper(missionRecord *database.Mission, req CreateMissionRequest) error {
	agencBinpath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve agenc binary path: %w", err)
	}

	// Build the wrapper command for the pool window.
	resumeCmd := fmt.Sprintf("'%s' mission resume --run-wrapper %s", agencBinpath, missionRecord.ID)
	if req.Prompt != "" {
		resumeCmd += fmt.Sprintf(" --prompt '%s'", strings.ReplaceAll(req.Prompt, "'", "'\\''"))
	}

	// Create the wrapper window in the pool
	poolWindowTarget, paneID, err := s.createPoolWindow(missionRecord.ID, resumeCmd)
	if err != nil {
		return fmt.Errorf("failed to create pool window: %w", err)
	}

	// Store the pane ID and trigger title reconciliation
	if err := s.db.SetTmuxPane(missionRecord.ID, paneID); err != nil {
		s.logger.Printf("Warning: failed to store pane ID for mission %s: %v", missionRecord.ShortID, err)
	}
	s.reconcileTmuxWindowTitle(missionRecord.ID)

	// Link the pool window into the caller's session (if provided)
	tmuxSession := req.TmuxSession
	if tmuxSession != "" {
		if err := linkPoolWindow(poolWindowTarget, tmuxSession); err != nil {
			s.destroyPoolWindow(missionRecord.ID)
			return fmt.Errorf("failed to link pool window: %w", err)
		}
	}

	return nil
}
```

The only changes from the original:
- `createPoolWindow` returns 3 values now (add `paneID`)
- Two new lines after pool window creation: `SetTmuxPane` + `reconcileTmuxWindowTitle`

**Step 2: Run `make check`**

Run: `make check`
Expected: Still fails — `ensureWrapperInPool` hasn't been updated yet.

**Step 3: Commit**

```
git add internal/server/missions.go
git commit -m "Store pane ID and reconcile title in spawnWrapper"
```

---

Task 3: Wire pane ID through `ensureWrapperInPool`
----------------------------------------------------

**Files:**
- Modify: `internal/server/missions.go:600-629` (`ensureWrapperInPool`)

**Step 1: Update `ensureWrapperInPool` to receive pane ID and store it**

```go
func (s *Server) ensureWrapperInPool(missionRecord *database.Mission) error {
	// Check if the wrapper is already running
	pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, missionRecord.ID)
	pid, err := ReadPID(pidFilepath)
	if err == nil && IsProcessRunning(pid) {
		// Wrapper is already running — ensure pool window exists too
		if poolWindowExists(missionRecord.ID) {
			return nil
		}
		return nil
	}

	// Wrapper not running — spawn it in the pool
	agencBinpath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve agenc binary path: %w", err)
	}

	resumeCmd := fmt.Sprintf("'%s' mission resume --run-wrapper %s", agencBinpath, missionRecord.ID)
	poolWindowTarget, paneID, err := s.createPoolWindow(missionRecord.ID, resumeCmd)
	if err != nil {
		return fmt.Errorf("failed to create pool window: %w", err)
	}

	// Store the pane ID and trigger title reconciliation
	if err := s.db.SetTmuxPane(missionRecord.ID, paneID); err != nil {
		s.logger.Printf("Warning: failed to store pane ID for mission %s: %v", database.ShortID(missionRecord.ID), err)
	}
	s.reconcileTmuxWindowTitle(missionRecord.ID)

	s.logger.Printf("Started wrapper in pool window %s for mission %s", poolWindowTarget, database.ShortID(missionRecord.ID))
	return nil
}
```

**Step 2: Run `make check`**

Run: `make check`
Expected: Compiles. All non-sandbox tests pass.

**Step 3: Commit**

```
git add internal/server/missions.go
git commit -m "Store pane ID and reconcile title in ensureWrapperInPool"
```

---

Task 4: Remove `TmuxPane` from the PATCH API
----------------------------------------------

**Files:**
- Modify: `internal/server/missions.go:688-693` (`UpdateMissionRequest`)
- Modify: `internal/server/missions.go:725-739` (`handleUpdateMission`)

**Step 1: Remove `TmuxPane` from `UpdateMissionRequest`**

Change the struct from:

```go
type UpdateMissionRequest struct {
	ConfigCommit *string `json:"config_commit,omitempty"`
	SessionName  *string `json:"session_name,omitempty"`
	Prompt       *string `json:"prompt,omitempty"`
	TmuxPane     *string `json:"tmux_pane,omitempty"`
}
```

To:

```go
type UpdateMissionRequest struct {
	ConfigCommit *string `json:"config_commit,omitempty"`
	SessionName  *string `json:"session_name,omitempty"`
	Prompt       *string `json:"prompt,omitempty"`
}
```

**Step 2: Remove the `TmuxPane` handling block from `handleUpdateMission`**

Remove this entire block (lines 725-739):

```go
	if req.TmuxPane != nil {
		if *req.TmuxPane == "" {
			if err := s.db.ClearTmuxPane(resolvedID); err != nil {
				return newHTTPErrorf(http.StatusInternalServerError, "failed to clear tmux_pane: %s", err.Error())
			}
		} else {
			if err := s.db.SetTmuxPane(resolvedID, *req.TmuxPane); err != nil {
				return newHTTPErrorf(http.StatusInternalServerError, "failed to set tmux_pane: %s", err.Error())
			}
			// Trigger initial tmux window title reconciliation now that the pane
			// is registered. This replaces the wrapper-side window rename that was
			// removed — the server is the sole owner of window titles.
			s.reconcileTmuxWindowTitle(resolvedID)
		}
	}
```

**Step 3: Run `make check`**

Run: `make check`
Expected: Compilation errors in `internal/wrapper/tmux.go` because it still references `TmuxPane` field on `UpdateMissionRequest`.

**Step 4: Commit**

```
git add internal/server/missions.go
git commit -m "Remove TmuxPane from PATCH /missions/{id} API"
```

---

Task 5: Remove wrapper pane registration
------------------------------------------

**Files:**
- Modify: `internal/wrapper/tmux.go` — delete `registerTmuxPane` and `clearTmuxPane`
- Modify: `internal/wrapper/wrapper.go:234-235` — remove calls in `Run()`
- Modify: `internal/wrapper/wrapper.go:639-640` — remove calls in `RunHeadless()`

**Step 1: Delete `registerTmuxPane` and `clearTmuxPane` from `tmux.go`**

Remove lines 22-43 (both functions and their doc comments):

```go
// registerTmuxPane records the current tmux pane ID in the database so that
// keybindings can resolve which mission is focused. No-ops when not inside tmux
// (e.g. headless mode).
//
// The pane number is stored WITHOUT the "%" prefix that $TMUX_PANE includes,
// since tmux format variables like #{pane_id} omit it. Stripping the prefix
// here keeps the database representation canonical; callers that need the
// tmux-native form (e.g. tmux rename-window -t) should prepend "%" themselves.
func (w *Wrapper) registerTmuxPane() {
	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return
	}
	pane := strings.TrimPrefix(paneID, "%")
	_ = w.client.UpdateMission(w.missionID, server.UpdateMissionRequest{TmuxPane: &pane})
}

// clearTmuxPane removes the tmux pane association for this mission.
func (w *Wrapper) clearTmuxPane() {
	empty := ""
	_ = w.client.UpdateMission(w.missionID, server.UpdateMissionRequest{TmuxPane: &empty})
}
```

**Step 2: Remove calls from `wrapper.go` `Run()`**

Remove these two lines (around lines 234-235):

```go
	w.registerTmuxPane()
	defer w.clearTmuxPane()
```

Keep the surrounding lines intact — the `defer w.resetWindowTabStyle()` on line 236 stays.

**Step 3: Remove calls from `wrapper.go` `RunHeadless()`**

Remove these two lines (around lines 639-640):

```go
	w.registerTmuxPane()
	defer w.clearTmuxPane()
```

**Step 4: Clean up unused imports in `tmux.go`**

After removing `registerTmuxPane` and `clearTmuxPane`, the `server` package import in `tmux.go` is no longer needed. Remove it from the import block. The `os` import is still needed by `resolveWindowID` and `setWindowTabColors`. The `strings` import is still needed by `setWindowTabColors` and `resolveWindowID`.

**Step 5: Run `make check`**

Run: `make check`
Expected: Compiles cleanly. All non-sandbox tests pass.

**Step 6: Commit**

```
git add internal/wrapper/tmux.go internal/wrapper/wrapper.go
git commit -m "Remove wrapper pane registration — server captures pane ID at window creation"
```
