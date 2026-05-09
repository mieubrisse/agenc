LAST PROMPT Column Implementation Plan
=======================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the LAST ACTIVE column (heartbeat-derived) with a LAST PROMPT column (`last_user_prompt_at`-derived) across every mission picker — including the `mission attach` picker which currently has no column at all — and fix the bug where unprompted missions disappear from search-fzf the moment the user types.

**Architecture:** All infrastructure is already in place: `last_user_prompt_at` is a populated DB column, the `UserPromptSubmit` hook updates it via the wrapper, and the picker sort already uses it as tier 2. The work is mechanical column renaming/swap across rendering call sites, a SQL ORDER BY change, a sort-fallback simplification, and one new merge step in `runMissionSearchFzf` that adds case-insensitive substring matching over `ListMissions` to recover unprompted missions FTS5 cannot index.

**Tech Stack:** Go 1.x, sqlite (stdlib + FTS5), Cobra CLI, fzf integration, tableprinter (internal). Build/test via Makefile (`make build`, `make e2e`). Pre-commit hook enforces `make check`.

**Conventions to obey while implementing:**
- Project CLAUDE.md: use `bd` for task tracking — NOT TaskCreate/TodoWrite. Treat each task in this plan as a `bd` issue if you create one.
- Build with `make build` (sandbox-disable required) — never `go build` directly.
- E2E with `make e2e` (mandatory for behavioral changes; the `mission attach` flow specifically needs manual tmux verification and should be flagged to the user, per CLAUDE.md "Tmux integration changes require manual testing").
- Auto-commit and push after each task (project rule). Pre-push: `git pull --rebase`. Single-line commit messages, no Co-Authored-By trailers.
- The header literal across the codebase is `"LAST ACTIVE"` (space, not underscore). Use `"LAST PROMPT"` (space) for the new column.

---

Task 1: Replace `formatLastActive` with `formatLastPrompt` (TDD)
=================================================================

**Files:**
- Modify: `cmd/mission_ls.go:180-187` — rename and re-key the function
- Test: `cmd/mission_format_test.go` (new file) OR add to existing `cmd/mission_sort_test.go`-adjacent test file

**Step 1: Write failing tests**

Create `cmd/mission_format_test.go`:

```go
package cmd

import (
	"strings"
	"testing"
	"time"
)

func TestFormatLastPrompt_NilReturnsDoubleDash(t *testing.T) {
	got := formatLastPrompt(nil, time.Now())
	if got != "--" {
		t.Fatalf("formatLastPrompt(nil, ...) = %q, want %q", got, "--")
	}
}

func TestFormatLastPrompt_NonNilReturnsLocalFormatted(t *testing.T) {
	ts := time.Date(2026, 5, 9, 14, 30, 0, 0, time.UTC)
	got := formatLastPrompt(&ts, time.Now())
	// "2006-01-02 15:04" layout in user's local TZ
	if got == "" || strings.Contains(got, "--") {
		t.Fatalf("formatLastPrompt(&t, ...) returned empty or sentinel: %q", got)
	}
	expected := ts.Local().Format("2006-01-02 15:04")
	if got != expected {
		t.Fatalf("formatLastPrompt(&t, ...) = %q, want %q", got, expected)
	}
}
```

**Step 2: Run tests to verify they fail (function does not exist yet)**

```
make check  # or: go test ./cmd -run TestFormatLastPrompt -v
```
Expected: FAIL with "undefined: formatLastPrompt"

**Step 3: Replace `formatLastActive` with `formatLastPrompt`**

In `cmd/mission_ls.go`, replace lines 180-187:

```go
// formatLastPrompt returns a human-readable timestamp of the user's last
// prompt for this mission. Returns "--" when no prompt has been recorded.
// The createdAt parameter is unused for display; it exists for symmetry with
// the sort-side COALESCE(last_user_prompt_at, created_at) key.
func formatLastPrompt(lastUserPromptAt *time.Time, _ time.Time) string {
	if lastUserPromptAt == nil {
		return "--"
	}
	return lastUserPromptAt.Local().Format("2006-01-02 15:04")
}
```

**Step 4: Update both call sites in `mission_ls.go`** (lines ~99 and ~108)

Replace:
```go
formatLastActive(m.LastHeartbeat, m.CreatedAt),
```
with:
```go
formatLastPrompt(m.LastUserPromptAt, m.CreatedAt),
```

Also update the column header literal in `mission_ls.go:86`:
```go
tbl = tableprinter.NewTable("LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO")
```
becomes:
```go
tbl = tableprinter.NewTable("LAST PROMPT", "ID", "STATUS", "SESSION", "REPO")
```

(There is also an `lsAllFlag` variant near line 84-86 with PANE column — apply the same header rename there.)

**Step 5: Verify tests pass**

```
make check
```
Expected: PASS for the two new tests; existing tests still pass.

**Step 6: Commit**

```
git add cmd/mission_ls.go cmd/mission_format_test.go
git commit -m "Replace formatLastActive with formatLastPrompt; switch mission ls header"
git pull --rebase && git push
```

---

Task 2: Update `missionPickerEntry` struct + `buildMissionPickerEntries`
=========================================================================

**Files:**
- Modify: `cmd/mission_helpers.go:23-30` (struct), `cmd/mission_helpers.go:59-76` (builder)

**Step 1: Rename struct field**

In `cmd/mission_helpers.go`, change line 25:
```go
LastActive string // formatted timestamp
```
to:
```go
LastPrompt string // formatted timestamp from last_user_prompt_at; "--" when nil
```

**Step 2: Update builder to use the new helper**

In `buildMissionPickerEntries` (line 68), change:
```go
LastActive: formatLastActive(m.LastHeartbeat, m.CreatedAt),
```
to:
```go
LastPrompt: formatLastPrompt(m.LastUserPromptAt, m.CreatedAt),
```

**Step 3: Build will fail until Task 3 fixes call sites — proceed directly to Task 3 without committing.**

(Don't commit here; the workspace is mid-rename.)

---

Task 3: Sweep all picker call sites (header strings + field references)
========================================================================

This is mechanical: every site that read `e.LastActive` now reads `e.LastPrompt`, and every header string `"LAST ACTIVE"` becomes `"LAST PROMPT"`.

**Files (modify):**
- `cmd/mission_archive.go:65,68`
- `cmd/mission_detach.go:83` (and corresponding header — search for `"LAST ACTIVE"` near line 78)
- `cmd/mission_inspect.go:73` (and header)
- `cmd/mission_print.go:85` (and header)
- `cmd/mission_rm.go:79` (and header)
- `cmd/mission_reload.go:90` (and header)
- `cmd/mission_stop.go:71` (and header)

**Step 1: Find all references**

```
grep -rn 'e\.LastActive\|"LAST ACTIVE"' cmd/
```
Expected output: list of every file/line that needs updating. Each is mechanical.

**Step 2: For each match, replace**
- `e.LastActive` → `e.LastPrompt`
- `"LAST ACTIVE"` → `"LAST PROMPT"`

**Step 3: Build to confirm sweep is complete**

```
make build  # sandbox disabled
```
Expected: clean build, no references to `LastActive`.

**Step 4: `mission attach` — add the column (NEW behavior)**

`cmd/mission_attach.go` currently has NO LAST PROMPT column. It needs the column added in two places:

(a) Line 122-126, the initial-input table:

```go
var buf bytes.Buffer
tbl := tableprinter.NewTable("LAST PROMPT", "ID", "SESSION", "REPO", "MATCH").WithWriter(&buf)
for _, e := range entries {
	tbl.AddRow(e.LastPrompt, e.ShortID, e.Session, e.Repo, "")
}
tbl.Print()
```

(b) Line 144-148, the `FzfSearchPickerConfig`:

```go
return runFzfSearchPicker(FzfSearchPickerConfig{
	Prompt:        "Search missions: ",
	Headers:       []string{"LAST PROMPT", "ID", "SESSION", "REPO", "MATCH"},
	ReloadCommand: reloadCmd,
	InitialInput:  initialInput.String(),
})
```

**Step 5: `mission_search_fzf.go` — add the column to BOTH render paths**

(a) `runMissionSearchFzf` (around line 107):

Change `tableprinter.NewTable("ID", "SESSION", "REPO", "MATCH")` → `tableprinter.NewTable("LAST PROMPT", "ID", "SESSION", "REPO", "MATCH")`.

The `cols` slice in the `row` struct must also gain the LAST PROMPT value as the first element. Two row-construction sites:

Line 56-63 (direct mission ID resolution):
```go
session := resolveSessionName(m)
repo := formatRepoDisplay(m.GitRepo, m.IsAdjutant, cfg)
lastPrompt := formatLastPrompt(m.LastUserPromptAt, m.CreatedAt)
rows = append(rows, row{
	shortID: m.ShortID,
	cols:    []string{lastPrompt, m.ShortID, session, repo, ""},
})
```

Line 78-99 (FTS results — the `SearchResult` struct from FTS may not carry `LastUserPromptAt` and `CreatedAt`; verify):

```
grep -n "type SearchResult" internal/database/
```

If `SearchResult` does NOT carry those fields, you have two choices:
- (preferred) Look up the `database.Mission` for each result via `client.GetMission(r.MissionID)` and call `formatLastPrompt(m.LastUserPromptAt, m.CreatedAt)`. Cost: N+1 RPC per result. With the FTS cap of 30, this is bounded.
- (alternative) Extend `SearchResult` to include the timestamp; update the SQL in `internal/database/search.go` to JOIN/select it.

Use the JOIN-extend approach (alternative) — N+1 lookups bother performance and add complexity. Add `last_user_prompt_at` and `created_at` to the `SearchResult` struct, update `SearchMissions`'s SQL to also select these from `missions` (the FTS query already joins to `missions`), and adjust scanning. Then in mission_search_fzf.go:

```go
lastPrompt := formatLastPrompt(r.LastUserPromptAt, r.CreatedAt)
rows = append(rows, row{
	shortID: shortID,
	cols:    []string{lastPrompt, shortID, session, repo, snippet},
})
```

(b) `printRecentMissionsForFzf` (line 142-149) — same as the mission_attach.go initial-input table:

```go
var buf strings.Builder
tbl := tableprinter.NewTable("LAST PROMPT", "ID", "SESSION", "REPO", "MATCH").WithWriter(&buf)
for _, e := range entries {
	tbl.AddRow(e.LastPrompt, e.ShortID, e.Session, e.Repo, "")
}
tbl.Print()
```

**Step 6: Build, verify clean**

```
make build  # sandbox disabled
```

**Step 7: Commit**

```
git add cmd/ internal/database/search.go  # if SearchResult extended
git commit -m "Display LAST PROMPT column in all mission pickers; add to mission attach"
git pull --rebase && git push
```

---

Task 4: Update `sortMissionsForPicker` (simplify tier 2, drop tier 3)
=======================================================================

**Files:**
- Modify: `cmd/mission_sort.go`
- Update: `cmd/mission_sort_test.go`

**Step 1: Update the sort logic**

In `cmd/mission_sort.go`, replace the body:

```go
func sortMissionsForPicker(missions []*database.Mission) {
	sort.SliceStable(missions, func(i, j int) bool {
		mi, mj := missions[i], missions[j]

		// Tier 1: needs_attention first
		iAttn := mi.ClaudeState != nil && *mi.ClaudeState == "needs_attention"
		jAttn := mj.ClaudeState != nil && *mj.ClaudeState == "needs_attention"
		if iAttn != jAttn {
			return iAttn
		}

		// Tier 2: COALESCE(last_user_prompt_at, created_at) DESC
		iTime := mi.CreatedAt
		if mi.LastUserPromptAt != nil {
			iTime = *mi.LastUserPromptAt
		}
		jTime := mj.CreatedAt
		if mj.LastUserPromptAt != nil {
			jTime = *mj.LastUserPromptAt
		}
		return iTime.After(jTime)
	})
}
```

Update the doc-comment at the top to match:

```go
// sortMissionsForPicker sorts missions in-place using two tiers:
//  1. Missions with claude_state "needs_attention" float to the top
//  2. Sorted by COALESCE(last_user_prompt_at, created_at) DESC so brand-new
//     unprompted missions interleave with prompted ones by user-interaction time
```

**Step 2: Update tests**

In `cmd/mission_sort_test.go`, the existing tests should mostly continue to pass (tier 1 unchanged; brand-new mission with `LastUserPromptAt=nil` already expected to appear at top under sane test data). Verify each test's expected ordering matches the new semantic. Replace any `LastHeartbeat`-based test setup (if present) with `CreatedAt` for the nil-prompt case.

Add a new test case asserting:

```go
{
	name: "unprompted recent mission outranks older prompted mission",
	in: []*database.Mission{
		{ShortID: "older_prompted", CreatedAt: now.Add(-3 * time.Hour), LastUserPromptAt: timePtr(now.Add(-1 * time.Hour))},
		{ShortID: "fresh_unprompted", CreatedAt: now, LastUserPromptAt: nil},
	},
	want: []string{"fresh_unprompted", "older_prompted"},
},
```

**Step 3: Run tests**

```
make check
```
Expected: PASS.

**Step 4: Commit**

```
git add cmd/mission_sort.go cmd/mission_sort_test.go
git commit -m "Simplify mission picker sort to COALESCE(last_user_prompt_at, created_at)"
git pull --rebase && git push
```

---

Task 5: Update SQL ORDER BY in `buildListMissionsQuery`
=========================================================

**Files:**
- Modify: `internal/database/queries.go:39`
- Verify: `internal/database/database_test.go` (existing TestListMissions tests)

**Step 1: Update ORDER BY**

In `internal/database/queries.go`, change line 39:

```go
query += " ORDER BY COALESCE(last_heartbeat, created_at) DESC"
```

to:

```go
query += " ORDER BY COALESCE(last_user_prompt_at, created_at) DESC, created_at DESC"
```

The secondary `created_at DESC` is the stable tiebreak for identical-timestamp pairs.

**Step 2: Verify existing tests still pass**

```
make check
```

Specifically check:
- `TestListMissions_SortsByNewestActivity` — still asserts the right ordering under the new key
- `TestListMissions_BrandNewMissionAppearsFirst` — passes natively; brand-new mission has `last_user_prompt_at=NULL` and recent `created_at`, so it COALESCEs to its `created_at` and sorts to top.

If a test fails because it was setting `LastHeartbeat` to control ordering, update the setup to use `LastUserPromptAt` (or `CreatedAt`) instead.

**Step 3: Commit**

```
git add internal/database/queries.go internal/database/database_test.go
git commit -m "Order missions by COALESCE(last_user_prompt_at, created_at) DESC"
git pull --rebase && git push
```

---

Task 6: Add substring-merge in `runMissionSearchFzf` (closes agenc-re9n)
=========================================================================

**Files:**
- Modify: `cmd/mission_search_fzf.go`
- Test: `cmd/mission_search_fzf_test.go` (new) — extract merge logic into a pure helper for unit testing

**Step 1: Extract a pure helper for substring matching**

Add to `cmd/mission_search_fzf.go`:

```go
// matchMissionSubstring returns true if the lowercased query is a substring
// of any of the mission's lowercased ResolvedSessionTitle, Prompt, or GitRepo.
// Empty fields never match a non-empty query (strings.Contains("", q) is false).
func matchMissionSubstring(m *database.Mission, lowerQuery string) bool {
	title := resolveSessionName(m)
	if strings.Contains(strings.ToLower(title), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(m.Prompt), lowerQuery) {
		return true
	}
	if strings.Contains(strings.ToLower(m.GitRepo), lowerQuery) {
		return true
	}
	return false
}
```

Verify `m.Prompt` and `m.GitRepo` are direct string fields on `database.Mission` (per `internal/database/missions.go`). Adjust if they are pointers.

**Step 2: Write failing tests**

`cmd/mission_search_fzf_test.go` (new):

```go
package cmd

import (
	"testing"

	"github.com/odyssey/agenc/internal/database"
)

func TestMatchMissionSubstring_TitleHit(t *testing.T) {
	m := &database.Mission{Prompt: "irrelevant", GitRepo: "github.com/foo/bar"}
	// resolveSessionName falls back to Prompt when no session title set;
	// we want the title-bearing path covered too — set up via a helper if needed.
	// (If resolveSessionName has more inputs, mock minimally.)
	if !matchMissionSubstring(m, "irrelevant") {
		t.Fatal("expected match against prompt-derived title")
	}
}

func TestMatchMissionSubstring_RepoHit(t *testing.T) {
	m := &database.Mission{GitRepo: "github.com/Foo/Bar"}
	if !matchMissionSubstring(m, "foo/bar") {
		t.Fatal("expected case-insensitive match against GitRepo")
	}
}

func TestMatchMissionSubstring_PromptHit(t *testing.T) {
	m := &database.Mission{Prompt: "Authentication feature", GitRepo: "x"}
	if !matchMissionSubstring(m, "auth") {
		t.Fatal("expected case-insensitive substring match against Prompt")
	}
}

func TestMatchMissionSubstring_NoHit(t *testing.T) {
	m := &database.Mission{Prompt: "x", GitRepo: "y"}
	if matchMissionSubstring(m, "nothing") {
		t.Fatal("expected no match")
	}
}

func TestMatchMissionSubstring_EmptyFields(t *testing.T) {
	m := &database.Mission{Prompt: "", GitRepo: ""}
	if matchMissionSubstring(m, "anything") {
		t.Fatal("empty fields should not match a non-empty query")
	}
}
```

**Step 3: Run tests to verify failure** (no `matchMissionSubstring` yet)

```
go test ./cmd -run TestMatchMissionSubstring -v
```
Expected: FAIL.

**Step 4: Add the helper from Step 1; re-run tests**

Expected: PASS.

**Step 5: Wire the merge into `runMissionSearchFzf`**

After the existing FTS loop (after line 99), add:

```go
// Merge: case-insensitive substring matches over ListMissions for missions
// not yet seen via FTS. This recovers unprompted missions and ones whose
// ResolvedSessionTitle/repo aren't indexed by FTS.
const substringMergeCap = 30
lowerQuery := strings.ToLower(query)
allMissions, listErr := client.ListMissions(server.ListMissionsRequest{IncludeArchived: true})
if listErr == nil {
	appended := 0
	for _, m := range allMissions {
		if appended >= substringMergeCap {
			break
		}
		if seenMissionIDs[m.ID] {
			continue
		}
		if !matchMissionSubstring(m, lowerQuery) {
			continue
		}
		seenMissionIDs[m.ID] = true
		appended++

		session := truncatePrompt(resolveSessionName(m), 30)
		repo := formatRepoDisplay(m.GitRepo, m.IsAdjutant, cfg)
		lastPrompt := formatLastPrompt(m.LastUserPromptAt, m.CreatedAt)
		rows = append(rows, row{
			shortID: m.ShortID,
			cols:    []string{lastPrompt, m.ShortID, session, repo, ""},
		})
	}
}
```

(Place this AFTER the FTS append loop and BEFORE the `if len(rows) == 0` check at line 101.)

Required imports: `"github.com/odyssey/agenc/internal/server"` (already present).

**Step 6: Build and verify**

```
make build  # sandbox disabled
```

**Step 7: Commit**

```
git add cmd/mission_search_fzf.go cmd/mission_search_fzf_test.go
git commit -m "Merge ListMissions substring matches into mission search-fzf (closes agenc-re9n)"
git pull --rebase && git push
```

---

Task 7: E2E tests — unprompted mission visibility
===================================================

**Files:**
- Modify: `scripts/e2e-test.sh`

**Step 1: Add a section to the E2E script**

Append to the appropriate section:

```bash
echo "--- LAST PROMPT column ---"

# Confirm mission ls renders the LAST PROMPT header (not LAST ACTIVE)
run_test_output_contains "mission ls header is LAST PROMPT" \
    "LAST PROMPT" \
    "${agenc_test}" mission ls

# Confirm mission ls header is no longer LAST ACTIVE
if "${agenc_test}" mission ls 2>&1 | grep -q "LAST ACTIVE"; then
    echo "FAIL: mission ls still contains LAST ACTIVE header"
    exit 1
else
    echo "PASS: mission ls header swapped"
fi
```

**Step 2: Add an unprompted-mission visibility test if `mission new` allows creating a mission without dispatching a prompt**

Check `agenc-test mission new --help` first to see if there's a `--no-prompt` or similar flag. If creating an unprompted mission is non-trivial in headless E2E, mark this as a manual verification item instead and document it in the commit.

**Step 3: Run E2E**

```
make e2e
```
Expected: all tests pass.

**Step 4: Commit**

```
git add scripts/e2e-test.sh
git commit -m "E2E: assert mission ls renders LAST PROMPT header"
git pull --rebase && git push
```

---

Task 8: Beads cleanup
======================

**Step 1: Verify `agenc-297` is closed**

```
bd show agenc-297
```
If not closed, close it:
```
bd close agenc-297 --reason="Last user prompt is now displayed and used as the sort key in mission pickers (delivered with LAST PROMPT column)"
```

**Step 2: Close `agenc-vhkg` (superseded — Mission Attach now has the LAST PROMPT column)**

```
bd close agenc-vhkg --reason="Superseded — column restored as LAST PROMPT (last_user_prompt_at) instead of LAST ACTIVE (last_heartbeat)"
```

**Step 3: Close `agenc-re9n` (absorbed — substring merge implemented in Task 6)**

```
bd close agenc-re9n --reason="Implemented as part of LAST PROMPT column work — runMissionSearchFzf now merges case-insensitive substring matches over ListMissions"
```

**Step 4: bd auto-commits its DB; ensure exports are pushed**

```
git status  # check for .beads/* changes
git add .beads/
git commit -m "Close agenc-vhkg, agenc-re9n, agenc-297 (LAST PROMPT column delivered)"
git pull --rebase && git push
```

---

Task 9: Manual verification (REQUIRED — flag to user)
=======================================================

Per CLAUDE.md, tmux integration changes require manual verification. The Mission Attach picker is tmux-integrated. Before declaring this work complete, flag this checklist to the user:

🚨 **MANUAL VERIFICATION REQUIRED** 🚨

In a real tmux session (not the test environment), run:

1. `agenc mission attach` — confirm:
   - LAST PROMPT column appears as the first column
   - Prompted missions show timestamps (e.g., `2026-05-09 14:30`)
   - Unprompted missions (or pre-migration ones) show `--`
   - Sort order is by COALESCE: brand-new mission appears at the top, older prompted missions below

2. With the picker open, type a partial repo name (e.g., `agenc`) — confirm:
   - Unprompted mission with that repo still appears in results (would have disappeared before this change)

3. Type a partial session-title fragment — confirm matches surface for unprompted missions.

4. `agenc mission ls` — confirm column header reads `LAST PROMPT`, not `LAST ACTIVE`.

If any check fails, file a bd bug and roll back the offending commit.

---

Final verification
===================

After all tasks:

- `make build` passes (sandbox disabled)
- `make e2e` passes
- Manual tmux checklist above passes
- `bd ready` shows the closed beads no longer in the queue

Update `docs/system-architecture.md` ONLY if any of the change categories listed in `agent/CLAUDE.md` apply. None of the changes here add/remove packages, change process boundaries, modify directory layout, change DB schema, or alter cross-cutting patterns — so no architecture-doc update is required. (Document this decision in the final commit message if helpful.)
