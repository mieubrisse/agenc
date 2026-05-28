# Attach Picker Attached-Indicator Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Show a green dot in the `agenc mission attach` picker for each mission currently linked into a tmux session outside the pool.

**Architecture:** The server computes an `is_attached` bool per mission by joining the DB's mission→pane mapping against a single live `tmux list-panes -a` query (`getLinkedPaneIDs`, which already excludes the pool session). The bool rides the existing transient-field rail on `database.Mission` into both mission-bearing response types (`MissionResponse`, `SearchMissionsResponse`). The CLI attach picker renders a green `●` from that bool. The CLI never queries tmux.

**Tech Stack:** Go, tmux, fzf, the project's `tableprinter` (ANSI-aware, runewidth-based).

**Spec:** `docs/superpowers/specs/2026-05-28-attach-picker-tmux-attached-indicator-design.md`
**Deferred cleanup:** bead `agenc-8vj3` (P3) — un-muddy `database.Mission`.

---

### Task 1: Server — pure attached-state computation (TDD)

**Files:**
- Modify: `internal/server/pool.go`
- Test: `internal/server/pool_test.go` (create if absent)

- [ ] **Step 1: Write the failing test**

Add to `internal/server/pool_test.go` (create the file with this content if it does not exist; if it exists, append the function and ensure `package server` and `testing` import are present):

```go
package server

import "testing"

func TestComputeMissionAttached(t *testing.T) {
	attachedPane := "42"
	unlinkedPane := "99"
	linked := map[string]bool{"42": true}

	if !computeMissionAttached(&attachedPane, linked) {
		t.Errorf("pane in linked set should report attached")
	}
	if computeMissionAttached(&unlinkedPane, linked) {
		t.Errorf("pane absent from linked set should report not attached")
	}
	if computeMissionAttached(nil, linked) {
		t.Errorf("nil pane should report not attached")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled — Go build cache): `go test ./internal/server/ -run TestComputeMissionAttached -v`
Expected: FAIL — `undefined: computeMissionAttached`

- [ ] **Step 3: Write minimal implementation**

Add to `internal/server/pool.go` (after `getLinkedPaneIDs`):

```go
// computeMissionAttached reports whether a mission is currently attached: its
// tmux pane is present in the linked-pane set (panes linked into a session
// outside the pool). A mission with no pane is never attached.
func computeMissionAttached(paneID *string, linkedPanes map[string]bool) bool {
	return paneID != nil && linkedPanes[*paneID]
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestComputeMissionAttached -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/pool.go internal/server/pool_test.go
git commit -m "Add computeMissionAttached pure join for tmux attached-state"
```

---

### Task 2: Server — transient `IsAttached` field through the type chain

**Files:**
- Modify: `internal/database/missions.go:44-46` (append after `ClaudeState`)
- Modify: `internal/server/missions.go` (`MissionResponse`, `toMissionResponse`, `ToMission`)
- Modify: `internal/server/search.go` (`SearchMissionsResponse`)

- [ ] **Step 1: Add the transient field to `database.Mission`**

In `internal/database/missions.go`, immediately after the `ClaudeState *string` field (inside the `Mission` struct, after its comment block ending at line 46), add:

```go

	// IsAttached is a transient field populated by the server API, not stored in
	// the database. True if the mission's tmux pane is currently linked into a
	// session outside the pool (i.e. the mission is "attached").
	IsAttached bool
```

- [ ] **Step 2: Add the JSON field to `MissionResponse`**

In `internal/server/missions.go`, in the `MissionResponse` struct, after the `ClaudeState *string` field (the last field, ~line 56), add:

```go

	// IsAttached is true if the mission's tmux pane is currently linked into a
	// session outside the pool. Computed live per request; never persisted.
	IsAttached bool `json:"is_attached"`
```

- [ ] **Step 3: Copy the field in both converters**

In `internal/server/missions.go`, in `toMissionResponse` add `IsAttached: m.IsAttached,` to the returned struct literal (alongside the other fields, e.g. after `IsAdjutant: m.IsAdjutant,`).

In `ToMission` add `IsAttached: mr.IsAttached,` to the returned struct literal (e.g. after `ClaudeState: mr.ClaudeState,`).

- [ ] **Step 4: Add the JSON field to `SearchMissionsResponse`**

In `internal/server/search.go`, in the `SearchMissionsResponse` struct, after `CreatedAt string` (~line 22), add:

```go
	IsAttached           bool    `json:"is_attached"`
```

- [ ] **Step 5: Verify it compiles**

Run (sandbox disabled): `make check`
Expected: build + tests pass (no behavior change yet; field is unset everywhere).

- [ ] **Step 6: Commit**

```bash
git add internal/database/missions.go internal/server/missions.go internal/server/search.go
git commit -m "Add transient IsAttached field to mission response types"
```

---

### Task 3: Server — populate `IsAttached` in the handlers

**Files:**
- Modify: `internal/server/missions.go` (`markMissionsAttached` helper; `handleListMissions` both paths; `handleGetMission`)
- Modify: `internal/server/search.go` (`handleSearchMissions`)

- [ ] **Step 1: Add the `markMissionsAttached` helper**

In `internal/server/missions.go`, after `enrichMissionWithSessionTitle` (~line 141), add:

```go
// markMissionsAttached sets IsAttached on each mission via a single live tmux
// query for the linked-pane set. If tmux is unreachable the set is empty and
// every mission is reported unattached.
func (s *Server) markMissionsAttached(missions []*database.Mission) {
	linkedPanes := getLinkedPaneIDs(s.getPoolSessionName())
	for _, m := range missions {
		m.IsAttached = computeMissionAttached(m.TmuxPane, linkedPanes)
	}
}
```

- [ ] **Step 2: Wire into `handleListMissions` (list path)**

In `internal/server/missions.go` `handleListMissions`, the list path has:

```go
	for _, m := range missions {
		s.enrichMissionWithSessionTitle(m)
	}

	responses := toMissionResponses(missions)
```

Insert `s.markMissionsAttached(missions)` between the loop and `responses := ...`:

```go
	for _, m := range missions {
		s.enrichMissionWithSessionTitle(m)
	}

	s.markMissionsAttached(missions)

	responses := toMissionResponses(missions)
```

- [ ] **Step 3: Wire into `handleListMissions` (single tmux_pane path)**

In the same function, the `if tmuxPane != ""` block has:

```go
		s.enrichMissionWithSessionTitle(mission)
		resp := toMissionResponse(mission)
```

Insert the mark call between them:

```go
		s.enrichMissionWithSessionTitle(mission)
		s.markMissionsAttached([]*database.Mission{mission})
		resp := toMissionResponse(mission)
```

- [ ] **Step 4: Wire into `handleGetMission`**

In `internal/server/missions.go` `handleGetMission` (~line 279):

```go
	s.enrichMissionWithSessionTitle(mission)
	resp := toMissionResponse(mission)
```

becomes:

```go
	s.enrichMissionWithSessionTitle(mission)
	s.markMissionsAttached([]*database.Mission{mission})
	resp := toMissionResponse(mission)
```

- [ ] **Step 5: Wire into `handleSearchMissions`**

In `internal/server/search.go` `handleSearchMissions`, the existing code declares `responses := make([]SearchMissionsResponse, 0, len(results))` immediately before the `for _, sr := range results {` loop. Insert a single new line **directly above** that existing `responses := make(...)` line (do not re-declare `responses`):

```go
	linkedPanes := getLinkedPaneIDs(s.getPoolSessionName())
```

Then inside the `if err == nil && mission != nil {` block (after the existing field assignments, before the closing brace of that block), add:

```go
			resp.IsAttached = computeMissionAttached(mission.TmuxPane, linkedPanes)
```

- [ ] **Step 6: Verify it compiles and tests pass**

Run (sandbox disabled): `make check`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/server/missions.go internal/server/search.go
git commit -m "Populate IsAttached in list, get, and search mission handlers"
```

---

### Task 4: CLI — `attachedDot` helper and picker-entry field

**Files:**
- Modify: `cmd/mission_helpers.go` (`missionPickerEntry`, `buildMissionPickerEntries`, new `attachedDot`)

- [ ] **Step 1: Add the `IsAttached` field to `missionPickerEntry`**

In `cmd/mission_helpers.go`, in the `missionPickerEntry` struct, after the `Repo string` field, add:

```go
	IsAttached bool   // true if mission is currently linked into a non-pool tmux session
```

- [ ] **Step 2: Populate it in `buildMissionPickerEntries`**

In the same file, in `buildMissionPickerEntries`, the appended struct literal currently ends with `Repo: repo,`. Add:

```go
			IsAttached: m.IsAttached,
```

- [ ] **Step 3: Add the `attachedDot` render helper**

In `cmd/mission_helpers.go` (end of file), add:

```go
// attachedDot returns a green dot when the mission is currently attached
// (linked into a tmux session outside the pool), or an empty string otherwise.
// The dot is a middle column in the picker table, so an empty value is safe —
// fzf's leading-whitespace stripping only affects the first column.
func attachedDot(isAttached bool) string {
	if isAttached {
		return ansiGreen + "●" + ansiReset
	}
	return ""
}
```

- [ ] **Step 4: Verify it compiles**

Run (sandbox disabled): `go build ./cmd/...`
Expected: success (helper unused until Task 5 — Go allows unused package-level functions).

- [ ] **Step 5: Commit**

```bash
git add cmd/mission_helpers.go
git commit -m "Add attachedDot helper and IsAttached picker-entry field"
```

---

### Task 5: CLI — render the dot column at every attach-picker render site

All four render sites currently produce the column layout `ID | LAST PROMPT | SESSION | REPO | MATCH`. They must all become `ID | ● | LAST PROMPT | SESSION | REPO | MATCH` identically, or the fixed fzf header will misalign against the reload-command data rows.

**Files:**
- Modify: `cmd/mission_attach.go` (`runMissionSearchPicker`)
- Modify: `cmd/mission_search_fzf.go` (`runMissionSearchFzf`, `appendSubstringMatches`, `printRecentMissionsForFzf`)

- [ ] **Step 1: `mission_attach.go` — initial render table + headers**

In `cmd/mission_attach.go` `runMissionSearchPicker`:

Change the table constructor and row append from:

```go
	tbl := tableprinter.NewTable("ID", "LAST PROMPT", "SESSION", "REPO", "MATCH").WithWriter(&buf)
	for _, e := range entries {
		tbl.AddRow(e.ShortID, e.LastPrompt, e.Session, e.Repo, "")
	}
```

to:

```go
	tbl := tableprinter.NewTable("ID", "●", "LAST PROMPT", "SESSION", "REPO", "MATCH").WithWriter(&buf)
	for _, e := range entries {
		tbl.AddRow(e.ShortID, attachedDot(e.IsAttached), e.LastPrompt, e.Session, e.Repo, "")
	}
```

And change the `Headers` field passed to `runFzfSearchPicker` from:

```go
		Headers:       []string{"ID", "LAST PROMPT", "SESSION", "REPO", "MATCH"},
```

to:

```go
		Headers:       []string{"ID", "●", "LAST PROMPT", "SESSION", "REPO", "MATCH"},
```

- [ ] **Step 2: `mission_search_fzf.go` — direct-ID branch**

In `runMissionSearchFzf`, the `if looksLikeMissionID(query)` branch currently appends:

```go
			rows = append(rows, searchFzfRow{
				shortID: m.ShortID,
				cols:    []string{m.ShortID, lastPrompt, session, repo, ""},
			})
```

Change the `cols` slice to:

```go
				cols:    []string{m.ShortID, attachedDot(m.IsAttached), lastPrompt, session, repo, ""},
```

- [ ] **Step 3: `mission_search_fzf.go` — FTS results branch**

In the `for _, r := range results {` loop, change:

```go
		rows = append(rows, searchFzfRow{
			shortID: shortID,
			cols:    []string{shortID, lastPrompt, session, repo, snippet},
		})
```

to:

```go
		rows = append(rows, searchFzfRow{
			shortID: shortID,
			cols:    []string{shortID, attachedDot(r.IsAttached), lastPrompt, session, repo, snippet},
		})
```

- [ ] **Step 4: `mission_search_fzf.go` — table constructor in `runMissionSearchFzf`**

Change:

```go
	tbl := tableprinter.NewTable("ID", "LAST PROMPT", "SESSION", "REPO", "MATCH").WithWriter(&buf)
```

to:

```go
	tbl := tableprinter.NewTable("ID", "●", "LAST PROMPT", "SESSION", "REPO", "MATCH").WithWriter(&buf)
```

- [ ] **Step 5: `mission_search_fzf.go` — `appendSubstringMatches`**

Change the row append:

```go
		rows = append(rows, searchFzfRow{
			shortID: m.ShortID,
			cols:    []string{m.ShortID, lastPrompt, session, repo, ""},
		})
```

to:

```go
		rows = append(rows, searchFzfRow{
			shortID: m.ShortID,
			cols:    []string{m.ShortID, attachedDot(m.IsAttached), lastPrompt, session, repo, ""},
		})
```

- [ ] **Step 6: `mission_search_fzf.go` — `printRecentMissionsForFzf`**

Change:

```go
	tbl := tableprinter.NewTable("ID", "LAST PROMPT", "SESSION", "REPO", "MATCH").WithWriter(&buf)
	for _, e := range entries {
		tbl.AddRow(e.ShortID, e.LastPrompt, e.Session, e.Repo, "")
	}
```

to:

```go
	tbl := tableprinter.NewTable("ID", "●", "LAST PROMPT", "SESSION", "REPO", "MATCH").WithWriter(&buf)
	for _, e := range entries {
		tbl.AddRow(e.ShortID, attachedDot(e.IsAttached), e.LastPrompt, e.Session, e.Repo, "")
	}
```

- [ ] **Step 7: Verify it compiles and tests pass**

Run (sandbox disabled): `make check`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add cmd/mission_attach.go cmd/mission_search_fzf.go
git commit -m "Render attached-state dot column in mission attach picker"
```

---

### Task 6: E2E regression test + manual-test callout

The green dot only appears when a mission's window is linked into a real non-pool session, which requires a live interactive tmux session — that slice is manual-test-only per the repo's tmux-integration rule. The automatable check is that adding the column did not break the picker's reload command output.

**Files:**
- Modify: `scripts/e2e-test.sh`

- [ ] **Step 1: Add an E2E regression test**

In `scripts/e2e-test.sh`, find the section that exercises mission listing/search (look for an existing `--- ` header near `mission ls` or `mission search-fzf`; if none, add a new `echo "--- Mission attach picker ---"` section). Append:

```bash
# Regression: the attach picker reload command (search-fzf) still renders rows
# after the attached-indicator column was added. We assert the empty-query
# recent list prints at least the header-less row stream without crashing.
run_test_no_crash "mission search-fzf empty query renders" \
    "${agenc_test}" mission search-fzf
```

- [ ] **Step 2: Run the E2E suite**

Run (sandbox disabled): `make e2e`
Expected: all tests pass, including the new one.

- [ ] **Step 3: Commit**

```bash
git add scripts/e2e-test.sh
git commit -m "E2E: assert attach picker reload survives attached-indicator column"
```

- [ ] **Step 4: Manual verification (report to user)**

These cannot be automated (live tmux + fzf). Report to the user that they must verify:

1. With at least one mission attached to the current session and one unattached, open the attach picker (`agenc mission attach` with no args, or the command-palette "Attach Mission" entry).
2. Confirm the attached mission shows a green `●` in the second column and the unattached mission's cell is blank.
3. Type to trigger live search and confirm the dot stays correct in the filtered results.
4. Clear the query and confirm the recent list still shows the dot correctly.

---

### Task 7: Final integration check

- [ ] **Step 1: Full build + checks**

Run (sandbox disabled): `make build`
Expected: PASS.

- [ ] **Step 2: Confirm spec coverage**

Re-read the spec and confirm: server-computed bool ✓, both response types ✓, all four render sites ✓, attach-picker-only ✓, graceful degradation (empty set → blank) ✓, `mission ls` untouched ✓.

- [ ] **Step 3: Push**

```bash
git pull --rebase origin main
git push
```
