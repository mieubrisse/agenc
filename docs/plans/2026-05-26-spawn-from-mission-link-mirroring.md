Spawn-from-Mission Session Link Mirroring — Implementation Plan
=================================================================

**Goal:** Spawned child missions appear in the same tmux sessions as their parent. Source field becomes the dispatch key for UI affordance; provenance is persisted automatically.

**Architecture:** CLI auto-populates `source="mission"` and `source_id=$AGENC_MISSION_UUID` when invoked from inside a mission. Server's `spawnWrapper` reads source/source_id, looks up parent's pane, and mirrors the parent's link-set onto the child via `getLinkedPaneSessions` (existing helper). `req.Headless` short-circuits all linking. Per-session link failures degrade to pool-only.

**Tech Stack:** Go, tmux, SQLite. Working directory: `/Users/odyssey/.agenc/missions/beabd207-a9ba-48ca-a3c0-6283f30dfb39/agent`. Solo repo, commits land directly on `main`.

**Design doc:** `docs/plans/2026-05-26-spawn-from-mission-link-mirroring-design.md` (commit `251fb52`).

**Provenance:** AgenC mission `beabd207-a9ba-48ca-a3c0-6283f30dfb39`.

---

Execution conventions
---------------------

- **Build/check commands need `dangerouslyDisableSandbox: true`** — the Go build cache lives outside the default sandbox.
- After every server-side edit chunk, run `make check` to catch lint/format/vet/test failures fast.
- Commit messages: concise summary + `AgenC mission: beabd207-a9ba-48ca-a3c0-6283f30dfb39` trailer. No Co-Authored-By.
- Final push: `git pull --rebase && git push`.
- Don't `--no-verify`; pre-commit hook runs `make check` and that gate is mandatory.

---

Task 1: Server-side `resolveLinkSessions` helper + spawnWrapper rewrite
------------------------------------------------------------------------

**Files:**
- Modify: `internal/server/missions.go` (CreateMissionRequest field doc + `spawnWrapper` body + new helper)

**Step 1.1: Update CreateMissionRequest documentation**

In `internal/server/missions.go` around line 286-304, update the `TmuxSession` field comment to note the new `Source="mission"` precedence, and add a comment near `Source`/`SourceID` noting their dual role as provenance + UI dispatch:

```go
// CreateMissionRequest is the JSON body for POST /missions.
type CreateMissionRequest struct {
    Repo   string `json:"repo"`
    Prompt string `json:"prompt"`
    // TmuxSession is the name of the user's currently-attached tmux session.
    // Used only when Source is empty (the user-terminal spawn path). When
    // Source is "mission", the link-set is derived server-side from the
    // parent mission's current tmux state and TmuxSession is ignored.
    TmuxSession    string `json:"tmux_session"`
    Headless       bool   `json:"headless"`
    Adjutant       bool   `json:"adjutant"`
    // Source identifies what kind of caller created this mission and acts as
    // the dispatch key for UI affordance at spawn time:
    //   "mission" → mirror parent's tmux link-set (parent's UUID in SourceID)
    //   "cron"    → pool-only (cron UUID in SourceID)
    //   ""        → use TmuxSession (user-terminal path)
    // Source/SourceID also persist to the missions row as durable provenance.
    Source         string `json:"source"`
    SourceID       string `json:"source_id"`
    SourceMetadata string `json:"source_metadata"`
    CloneFrom      string `json:"clone_from"`
    NoFocus        bool   `json:"no_focus"`
}
```

**Step 1.2: Add `resolveLinkSessions` helper**

Insert immediately before `spawnWrapper` (around line 529 in `internal/server/missions.go`):

```go
// resolveLinkSessions returns the set of tmux session names to link a newly-
// created mission's pool window into. The source field acts as the dispatch
// key:
//   - Source == "mission" → mirror the parent mission's current link-set
//     (every non-pool session the parent's pane is visible in).
//   - Otherwise → fall back to req.TmuxSession as a single-element slice (the
//     user-terminal path), or empty for pool-only.
//
// All failure modes (parent not found, parent has no tmux pane, parent is
// pool-only, tmux query failed) degrade to an empty slice — the caller will
// leave the child pool-only. Spawn must never fail because of a parent-
// resolution problem.
func (s *Server) resolveLinkSessions(req CreateMissionRequest) []string {
    if req.Source == "mission" && req.SourceID != "" {
        parent, err := s.db.GetMission(req.SourceID)
        if err != nil || parent == nil {
            s.logger.Printf("Warning: parent mission %s not found, child spawning pool-only", req.SourceID)
            return nil
        }
        if parent.TmuxPane == nil || *parent.TmuxPane == "" {
            s.logger.Printf("Warning: parent mission %s has no tmux pane, child spawning pool-only", database.ShortID(req.SourceID))
            return nil
        }
        poolName := s.getPoolSessionName()
        sessions := getLinkedPaneSessions(poolName)[*parent.TmuxPane]
        if len(sessions) == 0 {
            s.logger.Printf("Info: parent mission %s is pool-only, child spawning pool-only", database.ShortID(req.SourceID))
            return nil
        }
        return sessions
    }
    if req.TmuxSession != "" {
        return []string{req.TmuxSession}
    }
    return nil
}
```

**Step 1.3: Rewrite the link block in `spawnWrapper`**

Replace lines 552-560 of `internal/server/missions.go` (the current single-session `if req.TmuxSession != ""` block) with:

```go
    if !req.Headless {
        for _, session := range s.resolveLinkSessions(req) {
            if err := linkPoolWindowByPane(paneID, session); err != nil {
                s.logger.Printf("Warning: failed to link child %s into session %s: %v (continuing)", missionRecord.ShortID, session, err)
                continue
            }
            if !req.NoFocus {
                focusPaneInSession(paneID, session)
            }
        }
    }
```

**Important:** the *previous* code wrapped a link failure in `s.destroyPoolWindow(paneID)` + return error. We are deliberately removing that — per the design, spawn never fails because of a link problem. The pool window stands; the user can attach manually.

Update the `spawnWrapper` doc comment (lines 529-531) to reflect the new behavior:

```go
// spawnWrapper launches the wrapper process for a mission.
// All missions run in a pool window. After the pool window is created, the
// window is linked into zero or more user sessions per resolveLinkSessions
// (source-driven). req.Headless skips all linking. Link failures degrade
// gracefully — the spawn always succeeds as long as the pool window itself
// was created.
```

**Step 1.4: Verify the build passes**

Run: `make check` (with `dangerouslyDisableSandbox: true`).

Expected: lint clean, vet clean, tests pass, no new dead code.

**Step 1.5: Commit**

```bash
git add internal/server/missions.go
git commit -m "Server: mirror parent's tmux link-set when source=mission

AgenC mission: beabd207-a9ba-48ca-a3c0-6283f30dfb39"
```

---

Task 2: CLI auto-population from `AGENC_MISSION_UUID`
------------------------------------------------------

**Files:**
- Modify: `cmd/mission_new.go` (top of `runMissionNew`; small validation in `init`-time? no — at runtime)

**Step 2.1: Add auto-population + validation to `runMissionNew`**

Insert at the top of `runMissionNew` (currently at `cmd/mission_new.go:67`), immediately after the `ensureConfigured` and `ensureServerRunning` calls but before any branching:

```go
func runMissionNew(cmd *cobra.Command, args []string) error {
    if _, err := ensureConfigured(); err != nil {
        return err
    }
    ensureServerRunning()

    // Provenance auto-population: when invoked from inside a mission and the
    // caller didn't explicitly set --source, stamp source="mission" and
    // source_id=$AGENC_MISSION_UUID. The calling agent cannot forget to record
    // provenance — the CLI does it. Explicit --source always wins (e.g., a
    // cron firing from a mission context with --source=cron).
    if sourceFlag == "" {
        if parentUUID := os.Getenv(config.MissionUUIDEnvVar); parentUUID != "" {
            sourceFlag = "mission"
            sourceIDFlag = parentUUID
        }
    }

    // Cheap validation: source and source-id must be set together.
    if (sourceFlag == "") != (sourceIDFlag == "") {
        return stacktrace.NewError("--%s and --%s must be set together", "source", "source-id")
    }
    if len(sourceIDFlag) > 256 {
        return stacktrace.NewError("--source-id exceeds 256 characters")
    }

    if cloneFlag != "" {
        return runMissionNewWithClone()
    }
    // ... rest unchanged
}
```

Note: `config.MissionUUIDEnvVar` is the correct const (`"AGENC_MISSION_UUID"`, defined in `internal/config/config.go:51` and set by `BuildClaudeCmd` for every Claude spawned inside a mission). Existing precedent: `cmd/notifications_create.go:52` reads the same env var with the same const.

**Step 2.2: Verify the build passes**

Run: `make check` (with `dangerouslyDisableSandbox: true`).

Expected: lint clean, vet clean, tests pass.

**Step 2.3: Quick manual sanity check**

Run a non-mutating CLI smoke test to confirm the new code doesn't break help/parsing:

```bash
./_build/agenc-test mission new --help
```

Expected: prints help text without error, doesn't crash on the new validation block (no `--source` passed = no validation triggered).

**Step 2.4: Commit**

```bash
git add cmd/mission_new.go
git commit -m "CLI: auto-populate source=mission from \$AGENC_MISSION_UUID

AgenC mission: beabd207-a9ba-48ca-a3c0-6283f30dfb39"
```

---

Task 3: Architecture documentation update
------------------------------------------

**Files:**
- Modify: `docs/system-architecture.md` (section "Calling pane and session resolution", around line 572)

**Step 3.1: Append the mission-spawn dispatch paragraph**

After the existing "Direct CLI" / "Keybinding → run-shell" / "Keybinding → display-popup" table (around line 587) and the surrounding explanation (~line 596), add a new subsection:

```markdown
**Source-driven UI dispatch for mission creation.** When a CLI command creates a new mission, the `source` field on `CreateMissionRequest` doubles as the dispatch key for the new mission's UI affordance at spawn time:

| `source` | UI affordance | Driver |
|----------|---------------|--------|
| `"mission"` | Mirror parent's tmux link-set: server looks up the parent mission's pane via `source_id`, calls `getLinkedPaneSessions(poolName)`, and links the child's pool window into every session the parent currently appears in. | A Claude agent running inside another mission |
| `"cron"` | Pool-only | launchd-fired cron job |
| `""` (empty) | Single session from `tmux_session` field (the legacy user-terminal path) | User typing `agenc mission new` in their own tmux shell |

The CLI auto-populates `source="mission"` and `source_id=$AGENC_MISSION_UUID` whenever it detects it is running from inside a mission (`cmd/mission_new.go:runMissionNew`). The calling agent does not need to opt in — the CLI cannot forget. Explicit `--source=X` overrides the auto-detection (e.g., a cron firing from a mission context).

`source`/`source_id` are persisted to the `missions` table as durable provenance: every mission-spawned child carries a permanent pointer back to its parent. UI affordance and provenance ride the same field by design.

The "calling session" concept does not apply to mission-originated CLI calls: a mission has no single "calling session" because its pane can be linked into multiple sessions simultaneously. The parent's link-set is the replacement.
```

**Step 3.2: Commit**

```bash
git add docs/system-architecture.md
git commit -m "Doc: source as UI-dispatch key for mission creation

AgenC mission: beabd207-a9ba-48ca-a3c0-6283f30dfb39"
```

---

Task 4: E2E tests
------------------

**Files:**
- Modify: `scripts/e2e-test.sh` (new section near end)

**Step 4.1: Inspect the existing test harness shape**

Run: `grep -n "run_test\|--- " scripts/e2e-test.sh | head -40`

Read how parent missions are typically created in tests and how `sqlite3` queries are run. Identify how the test session is established (likely `tmux new-session -d` or similar) and how the test environment's tmux namespace gets discovered (`cat _test-env/namespace`).

**Step 4.2: Add the new test section**

Append before the test teardown (find the section that's last and add this after it):

```bash
echo
echo "--- Mission spawn-from-mission session mirroring ---"

# Discover the test environment's namespaced pool session
ns=$(cat _test-env/namespace)
pool_session="agenc-${ns}-pool"
test_session="agenc-e2e-link-mirror-${ns}"
db="_test-env/database.sqlite"

# Create a tmux session the test will use as the "user's session"
tmux new-session -d -s "${test_session}" -x 200 -y 50

# Create a parent mission (blank — no repo needed)
parent_out=$("${agenc_test}" mission new --blank 2>&1)
parent_uuid=$(echo "${parent_out}" | grep -oE '[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}' | head -1)
# Fallback: pull from DB if not in stdout
if [ -z "${parent_uuid}" ]; then
    parent_uuid=$(sqlite3 "${db}" "SELECT id FROM missions ORDER BY created_at DESC LIMIT 1")
fi

run_test "parent uuid resolved" 0 test -n "${parent_uuid}"

# Link parent into the test session via attach (simulates the user attaching)
# attach reads AGENC_CALLING_SESSION_NAME env var
AGENC_CALLING_SESSION_NAME="${test_session}" run_test "attach parent into test session" 0 \
    "${agenc_test}" mission attach --no-focus "${parent_uuid}"

# --- Test 1: happy path — child spawned from inside parent mirrors into test session ---
AGENC_MISSION_UUID="${parent_uuid}" "${agenc_test}" mission new --blank > /dev/null 2>&1
child_uuid=$(sqlite3 "${db}" "SELECT id FROM missions WHERE source='mission' AND source_id='${parent_uuid}' ORDER BY created_at DESC LIMIT 1")
run_test "child mission created with source=mission" 0 test -n "${child_uuid}"

child_pane=$(sqlite3 "${db}" "SELECT tmux_pane FROM missions WHERE id='${child_uuid}'")
run_test "child has a tmux pane" 0 test -n "${child_pane}"

# Child's pane should appear in the test session (mirror of parent)
session_panes=$(tmux list-panes -s -t "${test_session}" -F '#{pane_id}' 2>/dev/null | tr -d '%')
echo "${session_panes}" | grep -q "^${child_pane}$"
run_test "child pane linked into parent's test session" 0 test "$?" -eq 0

# --- Test 2: headless — provenance persists but no linking happens ---
AGENC_MISSION_UUID="${parent_uuid}" "${agenc_test}" mission new --blank --headless > /dev/null 2>&1
headless_child=$(sqlite3 "${db}" "SELECT id FROM missions WHERE source='mission' AND source_id='${parent_uuid}' ORDER BY created_at DESC LIMIT 1")
run_test "headless child has source=mission" 0 test "${headless_child}" != "${child_uuid}"

headless_pane=$(sqlite3 "${db}" "SELECT tmux_pane FROM missions WHERE id='${headless_child}'")
# Headless child's pane should NOT be in the test session
tmux list-panes -s -t "${test_session}" -F '#{pane_id}' 2>/dev/null | tr -d '%' | grep -qv "^${headless_pane}$"
run_test "headless child NOT linked into test session" 0 test -n "${headless_pane}"

# --- Test 3: explicit override — cron source from inside a mission stays pool-only ---
AGENC_MISSION_UUID="${parent_uuid}" "${agenc_test}" mission new --blank --source=cron --source-id=fake-cron-uuid > /dev/null 2>&1
cron_child=$(sqlite3 "${db}" "SELECT id FROM missions WHERE source='cron' AND source_id='fake-cron-uuid' ORDER BY created_at DESC LIMIT 1")
run_test "explicit --source=cron overrides auto-detection" 0 test -n "${cron_child}"

# --- Test 4: missing parent — child still spawns pool-only ---
"${agenc_test}" mission new --blank --source=mission --source-id=00000000-0000-0000-0000-000000000000 > /dev/null 2>&1
missing_parent_child=$(sqlite3 "${db}" "SELECT id FROM missions WHERE source='mission' AND source_id='00000000-0000-0000-0000-000000000000' ORDER BY created_at DESC LIMIT 1")
run_test "child with missing parent still created" 0 test -n "${missing_parent_child}"

# --- Test 5: pool-only parent — child stays pool-only ---
# Create a second parent, do not attach it anywhere
pool_only_parent_out=$("${agenc_test}" mission new --blank 2>&1)
pool_only_parent=$(sqlite3 "${db}" "SELECT id FROM missions WHERE source IS NULL OR source = '' ORDER BY created_at DESC LIMIT 1")
AGENC_MISSION_UUID="${pool_only_parent}" "${agenc_test}" mission new --blank > /dev/null 2>&1
pool_only_child=$(sqlite3 "${db}" "SELECT id FROM missions WHERE source='mission' AND source_id='${pool_only_parent}' ORDER BY created_at DESC LIMIT 1")
pool_only_child_pane=$(sqlite3 "${db}" "SELECT tmux_pane FROM missions WHERE id='${pool_only_child}'")
# Pane exists in pool but not in test_session
tmux list-panes -s -t "${test_session}" -F '#{pane_id}' 2>/dev/null | tr -d '%' | grep -q "^${pool_only_child_pane}$" && pool_only_result=1 || pool_only_result=0
run_test "child of pool-only parent stays pool-only" 0 test "${pool_only_result}" -eq 0

# Cleanup the test session (pool stays for teardown)
tmux kill-session -t "${test_session}" 2>/dev/null || true
```

**Step 4.3: Run E2E**

Run: `make e2e` (with `dangerouslyDisableSandbox: true`).

Expected: all 5 new tests pass alongside the existing E2E suite. Watch for any pre-existing flakes that might confuse the diagnosis.

**If a test fails:**
- Re-read the failure carefully — the most likely failure modes are (a) shell-quoting/extraction issues in `parent_uuid` resolution, (b) timing race where the wrapper hasn't yet stored the child's pane ID when we query, or (c) tmux session name mismatch.
- Fix the test or the implementation as appropriate (per truth-seeking principle: if the test reveals a real bug in the implementation, fix the impl).
- Re-run `make e2e`.

**Step 4.4: Commit E2E**

```bash
git add scripts/e2e-test.sh
git commit -m "E2E: spawn-from-mission link mirroring coverage

AgenC mission: beabd207-a9ba-48ca-a3c0-6283f30dfb39"
```

---

Task 5: Final integration check and push
-----------------------------------------

**Step 5.1: Full make check + e2e together**

```bash
make check && make e2e
```

(With `dangerouslyDisableSandbox: true`.)

Expected: both green.

**Step 5.2: Rebase against origin and push**

```bash
git pull --rebase
git push
```

If the rebase surfaces conflicts, resolve them manually (do NOT use `--ours`/`--theirs` per the global CLAUDE.md). Re-run `make check` after rebase before pushing.

**Step 5.3: Manual real-world verification**

Per the repo CLAUDE.md, tmux-pane/session changes can't be fully verified by unit/E2E tests — they need a live tmux session with real user interaction. After push, suggest to the user:

> The fix is live on main. To verify the bug is actually fixed in your real setup:
> 1. Open a fresh AgenC mission via the palette
> 2. From inside that mission's Claude bash, run `agenc mission new --blank`
> 3. Confirm the new mission's window appears in your current tmux session (not just in the pool)
> 4. Bonus: link the parent into a second session via `agenc mission attach`, then repeat — child should appear in both sessions

---

Out-of-band considerations
--------------------------

**If `make check` reveals that `getLinkedPaneSessions` is called from a context that the new code path doesn't expect** (e.g., it's an internal helper with usage patterns we missed), pause and re-read its callers. The design assumes it returns `map[string][]string` with the pool session already filtered out — verified at `internal/server/pool.go:241-277`.

**If E2E reveals timing issues** where the child's `tmux_pane` is not yet populated when the test queries the DB: add a short polling loop in the test (poll the DB for the child's pane up to ~2s before failing) rather than adding a sleep into production code. The `SetTmuxPane` write happens synchronously inside `spawnWrapper` before `linkPoolWindowByPane`, so the pane should be queryable by the time the CLI returns — but `_build/agenc-test mission new` is a separate process and there's at least one process boundary.

**Do not** introduce new fields on `CreateMissionRequest`. The whole point of the design is that the `Source`/`SourceID` fields we already have are doing the work — adding `ParentMissionUUID` was rejected during design.

**Do not** modify `mission_attach.go` or `mission_detach.go` in this PR. The "detach from inside a mission" case has different semantics and is a separate fix (mentioned as out-of-scope in the design doc).
