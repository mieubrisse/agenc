Skip Idle Kill for Linked Sessions — Implementation Plan
==========================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Prevent the idle timeout loop from killing wrapper processes whose tmux windows are linked into a user session.

**Architecture:** Add a single tmux query function to `pool.go` that returns which mission windows are linked beyond the pool session. Call it once per idle timeout cycle and skip linked missions.

**Tech Stack:** Go, tmux CLI

**Design doc:** `docs/plans/2026-02-24-skip-idle-kill-linked-sessions-design.md`

---

### Task 1: Add `getLinkedMissionIDs()` to `pool.go`

**Files:**
- Modify: `internal/server/pool.go:111` (append after `poolWindowExists`)

**Step 1: Add the function**

Append this function at the end of `pool.go`:

```go
// getLinkedMissionIDs returns the set of window names (short mission IDs) that
// are linked into at least one tmux session besides agenc-pool. If the tmux
// command fails (e.g., no server running), returns an empty map so the caller
// falls through to the existing idle-kill behavior.
func getLinkedMissionIDs() map[string]bool {
	cmd := exec.Command("tmux", "list-windows", "-a", "-F", "#{session_name} #{window_name}")
	output, err := cmd.Output()
	if err != nil {
		return map[string]bool{}
	}

	// Count which sessions each window name appears in
	windowSessions := make(map[string]map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		sessionName, windowName := parts[0], parts[1]
		if windowSessions[windowName] == nil {
			windowSessions[windowName] = make(map[string]bool)
		}
		windowSessions[windowName][sessionName] = true
	}

	// A window is "linked" if it appears in any session besides agenc-pool
	linked := make(map[string]bool)
	for windowName, sessions := range windowSessions {
		for sessionName := range sessions {
			if sessionName != poolSessionName {
				linked[windowName] = true
				break
			}
		}
	}
	return linked
}
```

**Step 2: Verify it compiles**

Run: `make build`
Expected: Successful build with no errors.

**Step 3: Commit**

```bash
git add internal/server/pool.go
git commit -m "Add getLinkedMissionIDs to detect pool windows linked into user sessions"
```

---

### Task 2: Skip linked missions in idle timeout cycle

**Files:**
- Modify: `internal/server/idle_timeout.go:47-73` (inside `runIdleTimeoutCycle`)

**Step 1: Add link check to the cycle**

In `runIdleTimeoutCycle()`, add the `getLinkedMissionIDs()` call after the mission list fetch, and add the skip check inside the loop. The function should look like this after the change:

```go
func (s *Server) runIdleTimeoutCycle() {
	missions, err := s.db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		s.logger.Printf("Idle timeout: failed to list missions: %v", err)
		return
	}

	linkedMissionIDs := getLinkedMissionIDs()

	now := time.Now()
	for _, m := range missions {
		if !s.isWrapperRunning(m.ID) {
			continue
		}

		idleDuration := s.missionIdleDuration(m, now)
		if idleDuration < defaultIdleTimeout {
			continue
		}

		// Skip missions whose pool window is linked into a user session
		if linkedMissionIDs[database.ShortID(m.ID)] {
			s.logger.Printf("Idle timeout: skipping mission %s (linked into user session, idle for %s)", database.ShortID(m.ID), idleDuration.Round(time.Second))
			continue
		}

		s.logger.Printf("Idle timeout: stopping mission %s (idle for %s)", database.ShortID(m.ID), idleDuration.Round(time.Second))
		if err := s.stopWrapper(m.ID); err != nil {
			s.logger.Printf("Idle timeout: failed to stop mission %s: %v", database.ShortID(m.ID), err)
			continue
		}

		// Also destroy the pool window since the wrapper exited
		s.destroyPoolWindow(m.ID)
	}
}
```

**Step 2: Verify it compiles**

Run: `make build`
Expected: Successful build with no errors.

**Step 3: Manual smoke test**

1. Start a mission: `./agenc mission new "test idle" -r agent`
2. Check that the window is linked: `tmux list-windows -a -F '#{session_name} #{window_name}'` — the mission's short ID should appear in both `agenc-pool` and the user's session.
3. Wait or temporarily lower the timeout to verify the log line "skipping mission ... (linked into user session)" appears in daemon logs.
4. Detach the mission: `./agenc mission detach <id>`
5. Verify the window now only appears in `agenc-pool`.
6. Confirm that after idle timeout, the unlinked mission gets killed as before.

**Step 4: Commit**

```bash
git add internal/server/idle_timeout.go
git commit -m "Skip idle kill for missions linked into user sessions"
```
