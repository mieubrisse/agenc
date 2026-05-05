Mission Reload `--prompt` Flag — Implementation Plan
=====================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `--prompt` string flag to `agenc mission reload <id>` so callers (especially missions self-reloading) can feed Claude a follow-up message that runs immediately after resume. Drop the unused multi-mission reload path. Add a per-mission reload guard to prevent concurrent reloads from interleaving.

**Architecture:** Single-mission only. CLI sends `{prompt}` in the JSON body to `POST /missions/{id}/reload`. The server threads the prompt through `reloadMissionInTmux` into `buildWrapperResumeCmd`, which already escapes single quotes. The pool window's `mission resume --run-wrapper` already accepts `--prompt`, so downstream wiring is unchanged. Concurrency control: per-mission `sync.Map` lock on the `Server` struct, acquired via `LoadOrStore` (atomic test-and-set) and released via `defer`.

**Tech Stack:** Go, Cobra (CLI), `net/http` (server), `sync.Map` (per-mission locks), tmux (`respawn-pane`).

**Reference docs:** Read `docs/plans/2026-05-05-mission-reload-prompt-design.md` for the full design, edge case enumeration, and error matrix. Read `CLAUDE.md` for repo conventions (especially the `make build` / `make e2e` requirements and the tmux-integration manual-testing rule).

**Build commands** (per `CLAUDE.md`):
- `make build` — full quality check + binary. Requires `dangerouslyDisableSandbox: true` because the Go build cache lives outside sandbox-writable paths.
- `make check` — quality checks only (no binary).
- `make e2e` — end-to-end tests; mandatory for behavioral changes.

**Commit hygiene:** Single-line commit messages, no `Co-Authored-By`. Auto-commit and push at the end (per AgenC's git workflow). Use `git pull --rebase` before pushing.

---

Task 1: Per-mission reload guard
--------------------------------

**Why first:** It's a small, self-contained primitive with no dependencies on other tasks. It can be unit-tested in isolation before any handler wiring.

**Files:**
- Modify: `internal/server/server.go` — add `reloadsInProgress sync.Map` field on `Server`.
- Create: `internal/server/reload_guard.go` — the helper.
- Create: `internal/server/reload_guard_test.go` — unit tests.

**Step 1: Write the failing test**

Create `internal/server/reload_guard_test.go`:

```go
package server

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestTryAcquireReloadLock_ExclusivePerMission(t *testing.T) {
	s := &Server{}

	release1, ok1 := s.tryAcquireReloadLock("mission-a")
	if !ok1 {
		t.Fatalf("first acquire should succeed")
	}

	_, ok2 := s.tryAcquireReloadLock("mission-a")
	if ok2 {
		t.Fatalf("second concurrent acquire on same mission should fail")
	}

	release1()

	release3, ok3 := s.tryAcquireReloadLock("mission-a")
	if !ok3 {
		t.Fatalf("acquire after release should succeed")
	}
	release3()
}

func TestTryAcquireReloadLock_DifferentMissionsConcurrent(t *testing.T) {
	s := &Server{}

	release1, ok1 := s.tryAcquireReloadLock("mission-a")
	release2, ok2 := s.tryAcquireReloadLock("mission-b")

	if !ok1 || !ok2 {
		t.Fatalf("acquires for different missions should both succeed")
	}

	release1()
	release2()
}

func TestTryAcquireReloadLock_RaceCondition(t *testing.T) {
	s := &Server{}
	const goroutines = 100
	var wg sync.WaitGroup
	var successCount atomic.Int32

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			release, ok := s.tryAcquireReloadLock("mission-x")
			if ok {
				successCount.Add(1)
				release()
			}
		}()
	}
	wg.Wait()

	// Note: this test doesn't assert exactly 1 success — multiple acquires
	// can succeed serially as long as they release first. What we DO assert
	// is that no goroutine that got ok=true left the slot held without
	// releasing, which is verified by checking the map is empty afterward.
	if _, exists := s.reloadsInProgress.Load("mission-x"); exists {
		t.Fatalf("reload lock should be empty after all goroutines complete")
	}
}

func TestTryAcquireReloadLock_StrictMutualExclusion(t *testing.T) {
	s := &Server{}
	const goroutines = 100
	var wg sync.WaitGroup
	var inFlight atomic.Int32
	var maxInFlight atomic.Int32
	heldGate := make(chan struct{})

	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			release, ok := s.tryAcquireReloadLock("mission-y")
			if !ok {
				return
			}
			n := inFlight.Add(1)
			for {
				cur := maxInFlight.Load()
				if n <= cur || maxInFlight.CompareAndSwap(cur, n) {
					break
				}
			}
			<-heldGate
			inFlight.Add(-1)
			release()
		}()
	}

	// Let the lucky goroutine sit holding the lock briefly so all losers
	// have a chance to attempt and fail.
	close(heldGate)
	wg.Wait()

	if maxInFlight.Load() > 1 {
		t.Fatalf("more than one goroutine held the lock simultaneously: max=%d", maxInFlight.Load())
	}
}
```

**Step 2: Run test to verify it fails**

```bash
go test ./internal/server/ -run TestTryAcquireReloadLock -v
```

Expected: compile error — `tryAcquireReloadLock` undefined, `reloadsInProgress` undefined.

**Step 3: Add the field to `Server`**

In `internal/server/server.go`, inside the `Server struct` definition (after the existing `loopHealth sync.Map` field around line 52), add:

```go
	// reloadsInProgress holds missionIDs currently being reloaded.
	// Acquired via tryAcquireReloadLock to prevent concurrent reloads of
	// the same mission from interleaving (which would race on stopWrapper
	// and respawn-pane). Different missions reload concurrently — the
	// lock is per-mission, not global.
	reloadsInProgress sync.Map
```

(`sync` is already imported.)

**Step 4: Create the helper**

Create `internal/server/reload_guard.go`:

```go
package server

// tryAcquireReloadLock attempts to claim an exclusive reload slot for the
// given mission. Returns a release func and true on success, or (nil, false)
// if a reload is already in progress for this mission.
//
// Callers MUST defer the release func to ensure the slot is freed even on
// panic or error.
//
// The lock is per-mission: different missions reload concurrently. Memory
// footprint is O(active reloads) — entries are deleted on release.
func (s *Server) tryAcquireReloadLock(missionID string) (release func(), ok bool) {
	if _, loaded := s.reloadsInProgress.LoadOrStore(missionID, struct{}{}); loaded {
		return nil, false
	}
	return func() { s.reloadsInProgress.Delete(missionID) }, true
}
```

**Step 5: Run tests to verify they pass**

```bash
go test ./internal/server/ -run TestTryAcquireReloadLock -v
```

Expected: all four tests PASS.

**Step 6: Run race detector**

```bash
go test ./internal/server/ -run TestTryAcquireReloadLock -race
```

Expected: PASS with no race warnings.

**Step 7: Commit**

```bash
git add internal/server/server.go internal/server/reload_guard.go internal/server/reload_guard_test.go
git commit -m "Add per-mission reload guard to prevent concurrent reload races"
```

---

Task 2: Server-side prompt threading and 409 response
-----------------------------------------------------

**Why second:** With the lock primitive in place, we can wire it into the handler alongside the prompt-threading change. Both touch the same handler, so they go in one logical commit.

**Files:**
- Modify: `internal/server/missions.go` — `ReloadMissionRequest`, `handleReloadMission`, `reloadMissionInTmux`.
- Create or extend: `internal/server/missions_test.go` — handler tests.

**Step 1: Read current handler code**

Re-read `internal/server/missions.go` lines 600–650 (`ReloadMissionRequest` and `handleReloadMission`) and lines 1044–1091 (`reloadMissionInTmux`). Confirm the current shape matches the design doc's data flow section.

**Step 2: Write failing tests for prompt threading**

The `buildWrapperResumeCmd` already accepts `prompt`, so the threading test is really "does the handler reach `buildWrapperResumeCmd` with the right prompt?" Since `reloadMissionInTmux` shells out to tmux (untestable in unit tests), the cleanest unit-level coverage is on `buildWrapperResumeCmd` directly.

Create `internal/server/missions_test.go` (if it doesn't exist) with:

```go
package server

import (
	"strings"
	"testing"
)

func TestBuildWrapperResumeCmd_NoPromptOmitsFlag(t *testing.T) {
	s := &Server{agencDirpath: "/tmp/agenc-test"}
	cmd, err := s.buildWrapperResumeCmd("mission-id-123", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(cmd, "--prompt") {
		t.Errorf("empty prompt should not produce --prompt flag, got: %q", cmd)
	}
	if !strings.Contains(cmd, "mission resume --run-wrapper mission-id-123") {
		t.Errorf("missing resume invocation, got: %q", cmd)
	}
}

func TestBuildWrapperResumeCmd_PromptThreadsThrough(t *testing.T) {
	s := &Server{agencDirpath: "/tmp/agenc-test"}
	cmd, err := s.buildWrapperResumeCmd("mission-id-123", "hello world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(cmd, "--prompt 'hello world'") {
		t.Errorf("expected --prompt 'hello world' in command, got: %q", cmd)
	}
}

func TestBuildWrapperResumeCmd_EscapesSingleQuotes(t *testing.T) {
	s := &Server{agencDirpath: "/tmp/agenc-test"}
	cmd, err := s.buildWrapperResumeCmd("mission-id-123", "don't 'do' it")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Single-quote escaping in shell: ' becomes '\''
	want := `--prompt 'don'\''t '\''do'\'' it'`
	if !strings.Contains(cmd, want) {
		t.Errorf("expected escaped form %q in command, got: %q", want, cmd)
	}
}

func TestBuildWrapperResumeCmd_PreservesShellMetachars(t *testing.T) {
	s := &Server{agencDirpath: "/tmp/agenc-test"}
	payload := "$(rm -rf /); echo hi && `whoami`"
	cmd, err := s.buildWrapperResumeCmd("mission-id-123", payload)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The single-quoted argument preserves all metachars literally.
	want := "--prompt '" + payload + "'"
	if !strings.Contains(cmd, want) {
		t.Errorf("expected literal preservation %q in command, got: %q", want, cmd)
	}
}
```

**Step 3: Run tests to confirm baseline behavior**

```bash
go test ./internal/server/ -run TestBuildWrapperResumeCmd -v
```

Expected: tests pass without code changes — `buildWrapperResumeCmd` already supports prompts. These tests **lock in** the existing behavior so the upcoming handler changes don't regress it.

If any test fails, stop and re-read `buildWrapperResumeCmd` to understand the divergence before continuing.

**Step 4: Update `ReloadMissionRequest`**

In `internal/server/missions.go`, replace the empty struct:

```go
// ReloadMissionRequest is the optional JSON body for POST /missions/{id}/reload.
type ReloadMissionRequest struct {
	// Prompt, when non-empty, is appended to the resume command and feeds
	// into Claude's `-c` resume as an initial follow-up message. The mission
	// must have a live tmux pane for prompts to be honored — otherwise the
	// handler returns 400.
	Prompt string `json:"prompt"`
}
```

**Step 5: Update `reloadMissionInTmux` signature**

Find `reloadMissionInTmux` (around line 1045 in missions.go). Change its signature and the `buildWrapperResumeCmd` call:

```go
func (s *Server) reloadMissionInTmux(missionRecord *database.Mission, paneID string, prompt string) error {
```

And later in the function (currently line 1081):

```go
	resumeCommand, err := s.buildWrapperResumeCmd(missionRecord.ID, prompt)
```

(Change `""` to `prompt`.)

**Step 6: Update `handleReloadMission`**

Replace the body of `handleReloadMission` (currently lines 606–647) with:

```go
func (s *Server) handleReloadMission(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	var req ReloadMissionRequest
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
		}
	}

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	missionRecord, err := s.db.GetMission(resolvedID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}
	if missionRecord == nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if missionRecord.Status == "archived" {
		return newHTTPError(http.StatusBadRequest, "cannot reload archived mission")
	}

	// Check for old-format mission (no agent/ subdirectory)
	agentDirpath := config.GetMissionAgentDirpath(s.agencDirpath, resolvedID)
	if _, statErr := os.Stat(agentDirpath); os.IsNotExist(statErr) {
		return newHTTPError(http.StatusBadRequest, "mission uses old directory format; archive and create a new mission")
	}

	// Acquire per-mission reload lock so concurrent reloads of the same
	// mission cannot interleave their stopWrapper + respawn-pane sequences.
	release, ok := s.tryAcquireReloadLock(resolvedID)
	if !ok {
		return newHTTPError(http.StatusConflict, "reload already in progress for mission "+database.ShortID(resolvedID))
	}
	defer release()

	// Detect tmux context and reload approach
	if missionRecord.TmuxPane != nil && *missionRecord.TmuxPane != "" {
		paneID := *missionRecord.TmuxPane
		if err := s.reloadMissionInTmux(missionRecord, paneID, req.Prompt); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to reload mission: %s", err.Error())
		}
	} else {
		// Non-tmux: only stop the wrapper. There is no pane to respawn into,
		// so a prompt cannot be honored on this path.
		if req.Prompt != "" {
			return newHTTPErrorf(http.StatusBadRequest,
				"--prompt requires a mission with a live tmux pane; mission %s has none — try 'agenc mission attach' to start it fresh",
				database.ShortID(resolvedID))
		}
		if err := s.stopWrapper(resolvedID); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to stop wrapper: %s", err.Error())
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
	return nil
}
```

**Step 7: Build to confirm compilation**

```bash
go build ./...
```

Expected: clean build. If errors, fix them before proceeding.

**Step 8: Run server tests**

```bash
go test ./internal/server/ -v
```

Expected: all tests pass, including new `TestBuildWrapperResumeCmd_*` tests.

**Step 9: Commit**

```bash
git add internal/server/missions.go internal/server/missions_test.go
git commit -m "Thread prompt through mission reload and gate with reload lock"
```

---

Task 3: Update server client signature
--------------------------------------

**Why third:** Client signature change is small and isolated; doing it before the CLI change means the CLI's compile error tells us exactly what to fix.

**Files:**
- Modify: `internal/server/client.go` — `ReloadMission` method.
- Modify: `cmd/mission_reload.go` — update the (still-multi-mission) call sites to compile.

**Step 1: Update `ReloadMission`**

In `internal/server/client.go` (around line 316), change:

```go
// ReloadMission reloads a mission's wrapper via the server. When prompt is
// non-empty, it is appended to the resume command and fed to Claude's
// `-c` resume.
func (c *Client) ReloadMission(id string, prompt string) error {
	body := ReloadMissionRequest{Prompt: prompt}
	return c.Post("/missions/"+id+"/reload", body, nil)
}
```

**Step 2: Build to find call sites**

```bash
go build ./...
```

Expected: errors at `cmd/mission_reload.go:55` and `:94` — `ReloadMission` now takes 2 args.

**Step 3: Pass empty prompt at both sites (temporary)**

In `cmd/mission_reload.go`, change line 55:

```go
		if err := client.ReloadMission(missionID, ""); err != nil {
```

And line 94:

```go
			if err := client.ReloadMission(entry.MissionID, ""); err != nil {
```

(These are placeholders — Task 4 rewrites the CLI to use a real prompt and drops the multi-mission call site.)

**Step 4: Build again**

```bash
go build ./...
```

Expected: clean build.

**Step 5: Run tests**

```bash
go test ./internal/server/ ./cmd/ -v
```

Expected: pass.

**Step 6: Commit**

```bash
git add internal/server/client.go cmd/mission_reload.go
git commit -m "Add prompt parameter to client.ReloadMission"
```

---

Task 4: CLI `--prompt` flag and single-mission shape
-----------------------------------------------------

**Why fourth:** CLI is the user-facing surface; with the server contract finalized, this is straightforward. Also drops the unused multi-mission code.

**Files:**
- Modify: `cmd/mission_reload.go` — add `--prompt` flag, switch to single-mission shape.

**Step 1: Replace the file**

Replace the entire contents of `cmd/mission_reload.go` with:

```go
package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
)

var reloadPromptFlag string

var missionReloadCmd = &cobra.Command{
	Use:   reloadCmdStr + " [mission-id]",
	Short: "Reload a mission in-place (preserves tmux pane)",
	Long: `Reload a mission in-place (preserves tmux pane).

Stops the mission wrapper and restarts it in the same tmux pane, preserving
window position, title, and conversation state. Useful after updating the
mission config or upgrading the agenc binary.

Without arguments, opens an interactive fzf picker showing running missions.
With an argument, accepts a mission ID (short 8-char hex or full UUID).

The --prompt flag, when set, is fed to Claude as a follow-up message that
runs immediately after the reload completes. The mission must have a live
tmux pane for --prompt to apply.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMissionReload,
}

func init() {
	missionCmd.AddCommand(missionReloadCmd)
	missionReloadCmd.Flags().StringVar(&reloadPromptFlag, promptFlagName, "", "follow-up prompt to send after reload (requires a mission with a live tmux pane)")
}

func runMissionReload(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	if len(args) == 1 {
		input := args[0]
		if !looksLikeMissionID(input) {
			return stacktrace.NewError("not a valid mission ID: %s", input)
		}
		missionID, err := client.ResolveMissionID(input)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve mission ID")
		}
		if err := client.ReloadMission(missionID, reloadPromptFlag); err != nil {
			return stacktrace.Propagate(err, "failed to reload mission %s", database.ShortID(missionID))
		}
		fmt.Printf("Mission '%s' reloaded\n", database.ShortID(missionID))
		return nil
	}

	// No args: list running missions and show fzf single-select picker
	missions, err := client.ListMissions(server.ListMissionsRequest{})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	runningMissions := filterRunningMissions(missions)
	if len(runningMissions) == 0 {
		fmt.Println("No running missions to reload.")
		return nil
	}

	entries := buildMissionPickerEntries(runningMissions, defaultPromptMaxLen)

	result, err := Resolve("", Resolver[missionPickerEntry]{
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Session, e.Repo}
		},
		FzfPrompt:  "Select mission to reload: ",
		FzfHeaders: []string{"LAST ACTIVE", "ID", "SESSION", "REPO"},
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	entry := result.Items[0]
	if err := client.ReloadMission(entry.MissionID, reloadPromptFlag); err != nil {
		return stacktrace.Propagate(err, "failed to reload mission %s", entry.ShortID)
	}
	fmt.Printf("Mission '%s' reloaded\n", database.ShortID(entry.MissionID))
	return nil
}
```

**Step 2: Verify the `Resolver` struct still has the right fields**

Run:

```bash
grep -n "type Resolver\|MultiSelect" cmd/*.go | head -20
```

If `MultiSelect` is a field on `Resolver`, my replacement above (which omits it) will default to `false`, which is the desired single-select behavior. If `MultiSelect` is required for any reason or there's a different field name, adjust.

**Step 3: Build**

```bash
go build ./...
```

Expected: clean build.

**Step 4: Run cmd tests**

```bash
go test ./cmd/ -v
```

Expected: pass. The existing `mission_reload_test.go` only tests `looksLikeMissionID` and `allLookLikeMissionIDs`, which are untouched.

**Step 5: Smoke-test `--help`**

```bash
go run . mission reload --help
```

Expected: output includes `--prompt` line and `Args: at most 1`.

**Step 6: Commit**

```bash
git add cmd/mission_reload.go
git commit -m "Add --prompt flag to mission reload, drop multi-mission path"
```

---

Task 5: Regenerate CLI docs
---------------------------

**Files:**
- Modify (auto): `docs/cli/agenc_mission_reload.md`, `docs/cli/agenc_mission.md` (if it summarizes children).

**Step 1: Run the docs generator**

```bash
make build
```

Expected: this runs `genprime` and `gendocs` as part of the build pipeline. The CLI markdown is regenerated automatically. **Use `dangerouslyDisableSandbox: true`** in the Bash tool call because the Go build cache is outside the sandbox.

**Step 2: Verify regenerated doc**

```bash
grep -A2 "prompt" docs/cli/agenc_mission_reload.md
```

Expected: shows the `--prompt` flag and its description.

**Step 3: Commit**

```bash
git add docs/cli/
git commit -m "Regenerate CLI docs for mission reload --prompt"
```

---

Task 6: E2E tests
-----------------

**Files:**
- Modify: `scripts/e2e-test.sh`.

**Step 1: Find an appropriate section**

Open `scripts/e2e-test.sh` and locate the section header for mission commands (likely `--- Mission ---` or similar).

```bash
grep -n "^echo \"---" scripts/e2e-test.sh
```

Pick the section that already exercises `mission` subcommands. If none exists for reload, create a new section.

**Step 2: Add tests**

Append (or insert in the appropriate section):

```bash
echo "--- Mission Reload ---"

run_test_output_contains "mission reload --help mentions --prompt" \
    "prompt" \
    "${agenc_test}" mission reload --help

run_test "mission reload with bad ID exits nonzero" \
    1 \
    "${agenc_test}" mission reload not-a-real-mission-id-zzzzzzzz --prompt "hi"
```

(`1` for the second test means we expect exit code 1; adjust to whatever `stacktrace.NewError` returns through cobra. If it uses `2`, change to `2`. Check by running the CLI with the bad ID first to learn the exit code.)

**Step 3: Run E2E**

```bash
make e2e
```

Use `dangerouslyDisableSandbox: true`.

Expected: all tests pass. If exit code mismatch, adjust the expected code.

**Step 4: Commit**

```bash
git add scripts/e2e-test.sh
git commit -m "Add E2E tests for mission reload --prompt flag"
```

---

Task 7: Full check
------------------

**Step 1: Run full quality gate**

```bash
make build
```

Use `dangerouslyDisableSandbox: true`.

Expected: tidy/format/vet/lint/deadcode/tests all pass, binary in `_build/agenc`.

**Step 2: Run E2E suite**

```bash
make e2e
```

Use `dangerouslyDisableSandbox: true`.

Expected: full suite passes.

**Step 3: If anything failed**

Fix the underlying issue. Do NOT skip hooks or comment out tests. Per `CLAUDE.md`: never use `--no-verify`.

---

Task 8: Manual verification (human-required)
---------------------------------------------

Per `CLAUDE.md` tmux-integration rule: changes that touch tmux pane respawn or wrapper startup require live manual verification. `make e2e` alone is not sufficient.

**Surface this to the user with the 🚨 ACTION REQUIRED 🚨 callout** before declaring the work complete:

🚨 **ACTION REQUIRED** 🚨

Manual test the new `--prompt` flag end-to-end:

1. Start (or attach to) a mission. Send a few prompts to establish a Claude session.
2. From a *separate* terminal, run:
   ```
   agenc mission reload <mission-id> --prompt "what was my last message?"
   ```
3. Confirm: the mission's pane respawns, Claude resumes the session, and the prompt is automatically submitted (Claude responds to "what was my last message?").
4. As a second test, have the mission self-reload by running the same command from inside the mission's own terminal. Confirm the new Claude that comes up after the respawn receives the prompt.

If either step fails (pane doesn't respawn, prompt is missing, prompt arrives garbled), report back — the implementation needs adjustment.

---

Task 9: Push
------------

**Step 1: Pull-rebase**

Per `CLAUDE.md` ("Git Push Workflow"):

```bash
git pull --rebase
```

Resolve conflicts if any (manually edit, do NOT use `--ours`/`--theirs`).

**Step 2: Push**

```bash
git push -u origin feat/mission-reload-prompt
```

**Step 3: Open PR (if collaborator workflow)**

This repo has 2 contributors (per `git shortlog -sn --all | wc -l`). Open a PR via `gh pr create` with a summary linking to the design doc.

---

Notes for the executor
----------------------

- **Do not** use `TaskCreate` / `TodoWrite` tools — this repo uses `bd` (beads). Either ignore task tracking entirely (the plan above is your task list) or use `bd create` for any sub-issues that emerge.
- **Do not** invoke the `agenc-engineer` skill — banned in this repo per `CLAUDE.md`.
- **Single-line commit messages** only; no `Co-Authored-By` footer.
- **Sandbox:** `make build`, `make check`, and `make e2e` need `dangerouslyDisableSandbox: true`.
- **Reference:** `docs/plans/2026-05-05-mission-reload-prompt-design.md` is the source of truth for the design. If a task feels ambiguous, re-read the relevant section there before guessing.
