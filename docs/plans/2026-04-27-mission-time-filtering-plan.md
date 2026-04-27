# Mission Time-Based Filtering Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `--since` and `--until` flags to `agenc mission ls` that filter missions by `created_at` timestamp.

**Architecture:** Three-layer change — database (query builder), server API (query param parsing), CLI (flag parsing and display). Also refactors `Client.ListMissions` from positional args to a struct. See `docs/plans/2026-04-27-mission-time-filtering-design.md` for full design.

**Tech Stack:** Go, SQLite, Cobra CLI, unix-socket HTTP server

---

### Task 1: Add Since/Until to database layer

**Files:**
- Modify: `internal/database/missions.go:57-62` (ListMissionsParams struct)
- Modify: `internal/database/queries.go:9-25` (buildListMissionsQuery conditions)

**Step 1: Add fields to ListMissionsParams**

In `internal/database/missions.go`, add `Since` and `Until` to the struct:

```go
// ListMissionsParams holds optional parameters for filtering missions.
type ListMissionsParams struct {
	IncludeArchived bool
	Source          *string
	SourceID        *string
	Since           *time.Time
	Until           *time.Time
}
```

**Step 2: Add WHERE clauses to query builder**

In `internal/database/queries.go`, add conditions after the SourceID block (after line 25):

```go
if params.Since != nil {
	conditions = append(conditions, "created_at >= ?")
	args = append(args, params.Since.UTC().Format(time.RFC3339))
}
if params.Until != nil {
	conditions = append(conditions, "created_at <= ?")
	args = append(args, params.Until.UTC().Format(time.RFC3339))
}
```

Add `"time"` to the imports in `queries.go`.

**Step 3: Build and verify compilation**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Expected: Compiles cleanly. Existing tests pass (no behavioral change when Since/Until are nil).

**Step 4: Commit**

```
git add internal/database/missions.go internal/database/queries.go
git commit -m "Add Since/Until time filtering to ListMissionsParams"
```

---

### Task 2: Add database unit tests for time filtering

**Files:**
- Modify: `internal/database/database_test.go` (append new test functions)

**Step 1: Write test for Since filter**

Append to `internal/database/database_test.go`:

```go
func TestListMissions_SinceFilter(t *testing.T) {
	db := openTestDB(t)

	// Create two missions with known created_at times
	old, err := db.CreateMission("github.com/owner/old-repo", nil)
	if err != nil {
		t.Fatalf("failed to create old mission: %v", err)
	}
	oldTime := "2026-01-01T00:00:00Z"
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", oldTime, old.ID); err != nil {
		t.Fatalf("failed to backdate old mission: %v", err)
	}

	recent, err := db.CreateMission("github.com/owner/recent-repo", nil)
	if err != nil {
		t.Fatalf("failed to create recent mission: %v", err)
	}
	recentTime := "2026-04-15T12:00:00Z"
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", recentTime, recent.ID); err != nil {
		t.Fatalf("failed to set recent mission time: %v", err)
	}

	// Filter since April 1 — should only return the recent mission
	since := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	missions, err := db.ListMissions(ListMissionsParams{Since: &since})
	if err != nil {
		t.Fatalf("ListMissions with Since failed: %v", err)
	}
	if len(missions) != 1 {
		t.Fatalf("expected 1 mission, got %d", len(missions))
	}
	if missions[0].ID != recent.ID {
		t.Errorf("expected recent mission, got %s", missions[0].ID)
	}
}

func TestListMissions_UntilFilter(t *testing.T) {
	db := openTestDB(t)

	old, err := db.CreateMission("github.com/owner/old-repo", nil)
	if err != nil {
		t.Fatalf("failed to create old mission: %v", err)
	}
	oldTime := "2026-01-15T00:00:00Z"
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", oldTime, old.ID); err != nil {
		t.Fatalf("failed to backdate old mission: %v", err)
	}

	recent, err := db.CreateMission("github.com/owner/recent-repo", nil)
	if err != nil {
		t.Fatalf("failed to create recent mission: %v", err)
	}
	recentTime := "2026-04-15T12:00:00Z"
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", recentTime, recent.ID); err != nil {
		t.Fatalf("failed to set recent mission time: %v", err)
	}

	// Filter until Feb 1 — should only return the old mission
	until := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	missions, err := db.ListMissions(ListMissionsParams{Until: &until})
	if err != nil {
		t.Fatalf("ListMissions with Until failed: %v", err)
	}
	if len(missions) != 1 {
		t.Fatalf("expected 1 mission, got %d", len(missions))
	}
	if missions[0].ID != old.ID {
		t.Errorf("expected old mission, got %s", missions[0].ID)
	}
}

func TestListMissions_SinceAndUntilFilter(t *testing.T) {
	db := openTestDB(t)

	// Create three missions: Jan, March, May
	jan, err := db.CreateMission("github.com/owner/jan", nil)
	if err != nil {
		t.Fatalf("failed to create jan mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", "2026-01-15T00:00:00Z", jan.ID); err != nil {
		t.Fatalf("failed to set jan time: %v", err)
	}

	mar, err := db.CreateMission("github.com/owner/mar", nil)
	if err != nil {
		t.Fatalf("failed to create mar mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", "2026-03-15T00:00:00Z", mar.ID); err != nil {
		t.Fatalf("failed to set mar time: %v", err)
	}

	may, err := db.CreateMission("github.com/owner/may", nil)
	if err != nil {
		t.Fatalf("failed to create may mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", "2026-05-15T00:00:00Z", may.ID); err != nil {
		t.Fatalf("failed to set may time: %v", err)
	}

	// Filter Feb–April: should return only March
	since := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2026, 4, 30, 23, 59, 59, 0, time.UTC)
	missions, err := db.ListMissions(ListMissionsParams{Since: &since, Until: &until})
	if err != nil {
		t.Fatalf("ListMissions with Since+Until failed: %v", err)
	}
	if len(missions) != 1 {
		t.Fatalf("expected 1 mission, got %d", len(missions))
	}
	if missions[0].ID != mar.ID {
		t.Errorf("expected mar mission, got %s", missions[0].ID)
	}
}

func TestListMissions_TimeFilterWithArchived(t *testing.T) {
	db := openTestDB(t)

	m, err := db.CreateMission("github.com/owner/repo", nil)
	if err != nil {
		t.Fatalf("failed to create mission: %v", err)
	}
	if _, err := db.conn.Exec("UPDATE missions SET created_at = ? WHERE id = ?", "2026-03-15T00:00:00Z", m.ID); err != nil {
		t.Fatalf("failed to set time: %v", err)
	}
	if err := db.ArchiveMission(m.ID); err != nil {
		t.Fatalf("failed to archive: %v", err)
	}

	since := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	// Without IncludeArchived: should return 0
	missions, err := db.ListMissions(ListMissionsParams{Since: &since})
	if err != nil {
		t.Fatalf("ListMissions failed: %v", err)
	}
	if len(missions) != 0 {
		t.Fatalf("expected 0 missions without IncludeArchived, got %d", len(missions))
	}

	// With IncludeArchived: should return 1
	missions, err = db.ListMissions(ListMissionsParams{Since: &since, IncludeArchived: true})
	if err != nil {
		t.Fatalf("ListMissions failed: %v", err)
	}
	if len(missions) != 1 {
		t.Fatalf("expected 1 mission with IncludeArchived, got %d", len(missions))
	}
}
```

Add `"time"` to the imports in `database_test.go`.

**Step 2: Run tests**

Run: `go test ./internal/database/ -run "TestListMissions_(Since|Until|TimeFilter)" -v` (with `dangerouslyDisableSandbox: true`)
Expected: All 4 tests PASS.

**Step 3: Commit**

```
git add internal/database/database_test.go
git commit -m "Add unit tests for mission time-range filtering"
```

---

### Task 3: Parse since/until in server API handler

**Files:**
- Modify: `internal/server/missions.go:210-219` (handleListMissions)

**Step 1: Add query param parsing**

In `handleListMissions`, after the existing `source_id` parsing block (after line 219), add:

```go
if sinceStr := r.URL.Query().Get("since"); sinceStr != "" {
	t, err := time.Parse(time.RFC3339, sinceStr)
	if err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid 'since' parameter: expected RFC3339 format")
	}
	params.Since = &t
}
if untilStr := r.URL.Query().Get("until"); untilStr != "" {
	t, err := time.Parse(time.RFC3339, untilStr)
	if err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid 'until' parameter: expected RFC3339 format")
	}
	params.Until = &t
}
```

`time` is already imported in this file.

**Step 2: Build and verify**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Expected: Compiles cleanly.

**Step 3: Commit**

```
git add internal/server/missions.go
git commit -m "Parse since/until query params in mission list API"
```

---

### Task 4: Refactor Client.ListMissions to struct params

**Files:**
- Modify: `internal/server/client.go:209-236` (ListMissions method)

**Step 1: Add ListMissionsRequest struct and rewrite ListMissions**

Replace the existing `ListMissions` method with:

```go
// ListMissionsRequest holds parameters for the ListMissions client call.
type ListMissionsRequest struct {
	IncludeArchived bool
	Source          string
	SourceID        string
	Since           *time.Time
	Until           *time.Time
}

// ListMissions fetches missions from the server with optional filtering.
func (c *Client) ListMissions(req ListMissionsRequest) ([]*database.Mission, error) {
	path := "/missions"
	var params []string
	if req.IncludeArchived {
		params = append(params, "include_archived=true")
	}
	if req.Source != "" {
		params = append(params, "source="+req.Source)
	}
	if req.SourceID != "" {
		params = append(params, "source_id="+req.SourceID)
	}
	if req.Since != nil {
		params = append(params, "since="+req.Since.UTC().Format(time.RFC3339))
	}
	if req.Until != nil {
		params = append(params, "until="+req.Until.UTC().Format(time.RFC3339))
	}
	if len(params) > 0 {
		path += "?" + strings.Join(params, "&")
	}

	var responses []MissionResponse
	if err := c.Get(path, &responses); err != nil {
		return nil, err
	}

	missions := make([]*database.Mission, len(responses))
	for i := range responses {
		missions[i] = responses[i].ToMission()
	}
	return missions, nil
}
```

This will cause compilation errors at all call sites — that is expected and fixed in Task 5.

**Step 2: Do NOT build yet** — proceed to Task 5 to fix call sites first.

---

### Task 5: Update all Client.ListMissions call sites

**Files:** All 15 call sites in `cmd/`. Each is a one-line change.

**Step 1: Update each call site**

Apply these replacements:

| File | Line | Old | New |
|------|------|-----|-----|
| `cmd/mission_archive.go` | 32 | `client.ListMissions(false, "", "")` | `client.ListMissions(server.ListMissionsRequest{})` |
| `cmd/mission_stop.go` | 54 | `client.ListMissions(false, "", "")` | `client.ListMissions(server.ListMissionsRequest{})` |
| `cmd/mission_reload.go` | 62 | `client.ListMissions(false, "", "")` | `client.ListMissions(server.ListMissionsRequest{})` |
| `cmd/mission_detach.go` | 63 | `client.ListMissions(false, "", "")` | `client.ListMissions(server.ListMissionsRequest{})` |
| `cmd/mission_print.go` | 54 | `client.ListMissions(false, "", "")` | `client.ListMissions(server.ListMissionsRequest{})` |
| `cmd/mission_update_config.go` | 89 | `client.ListMissions(false, "", "")` | `client.ListMissions(server.ListMissionsRequest{})` |
| `cmd/mission_update_config.go` | 127 | `client.ListMissions(false, "", "")` | `client.ListMissions(server.ListMissionsRequest{})` |
| `cmd/mission_attach.go` | 47 | `client.ListMissions(true, "", "")` | `client.ListMissions(server.ListMissionsRequest{IncludeArchived: true})` |
| `cmd/mission_inspect.go` | 40 | `client.ListMissions(true, "", "")` | `client.ListMissions(server.ListMissionsRequest{IncludeArchived: true})` |
| `cmd/mission_nuke.go` | 36 | `client.ListMissions(true, "", "")` | `client.ListMissions(server.ListMissionsRequest{IncludeArchived: true})` |
| `cmd/mission_rm.go` | 46 | `client.ListMissions(true, "", "")` | `client.ListMissions(server.ListMissionsRequest{IncludeArchived: true})` |
| `cmd/summary.go` | 63 | `client.ListMissions(true, "", "")` | `client.ListMissions(server.ListMissionsRequest{IncludeArchived: true})` |
| `cmd/cron_history.go` | 51 | `client.ListMissions(true, "cron", cronID)` | `client.ListMissions(server.ListMissionsRequest{IncludeArchived: true, Source: "cron", SourceID: cronID})` |
| `cmd/cron_ls.go` | 68 | `client.ListMissions(true, "cron", cronInfo.ID)` | `client.ListMissions(server.ListMissionsRequest{IncludeArchived: true, Source: "cron", SourceID: cronInfo.ID})` |
| `cmd/mission_ls.go` | 263 | `client.ListMissions(lsAllFlag, "", lsCronFlag)` | `client.ListMissions(server.ListMissionsRequest{IncludeArchived: lsAllFlag, SourceID: lsCronFlag})` |

Ensure each file imports `"github.com/odyssey/agenc/internal/server"` (most already do).

**Step 2: Build and run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: Compiles cleanly. All existing tests pass. No behavioral change.

**Step 3: Commit**

```
git add internal/server/client.go cmd/
git commit -m "Refactor Client.ListMissions to struct-based params"
```

---

### Task 6: Add --since/--until CLI flags with date parsing

**Files:**
- Modify: `cmd/mission_ls.go`

**Step 1: Add flag variables and registration**

Add after line 33 (`var lsCronFlag string`):

```go
var lsSinceFlag string
var lsUntilFlag string
```

In the `init()` function, add after line 43:

```go
missionLsCmd.Flags().StringVar(&lsSinceFlag, "since", "", "show missions created on or after this date (YYYY-MM-DD or RFC3339)")
missionLsCmd.Flags().StringVar(&lsUntilFlag, "until", "", "show missions created on or before this date (YYYY-MM-DD or RFC3339)")
```

**Step 2: Add parseTimeFlag function**

Add to `cmd/mission_ls.go`:

```go
// parseTimeFlag parses a time flag value. Accepts RFC3339 or YYYY-MM-DD.
// For date-only input, isSince controls whether the time is set to start of
// day (true) or end of day (false), using local timezone.
func parseTimeFlag(value string, isSince bool) (time.Time, error) {
	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}

	// Try YYYY-MM-DD in local timezone
	t, err := time.ParseInLocation("2006-01-02", value, time.Now().Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("expected YYYY-MM-DD or RFC3339 format")
	}

	if !isSince {
		// For --until, use end of day
		t = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	}
	return t, nil
}
```

**Step 3: Update fetchMissions to pass time filters**

Replace the `fetchMissions` function:

```go
// fetchMissions fetches missions from the server.
func fetchMissions() ([]*database.Mission, error) {
	client, err := serverClient()
	if err != nil {
		return nil, err
	}

	req := server.ListMissionsRequest{
		IncludeArchived: lsAllFlag,
		SourceID:        lsCronFlag,
	}

	if lsSinceFlag != "" {
		t, err := parseTimeFlag(lsSinceFlag, true)
		if err != nil {
			return nil, fmt.Errorf("invalid --since value %q: %s", lsSinceFlag, err)
		}
		req.Since = &t
	}
	if lsUntilFlag != "" {
		t, err := parseTimeFlag(lsUntilFlag, false)
		if err != nil {
			return nil, fmt.Errorf("invalid --until value %q: %s", lsUntilFlag, err)
		}
		req.Until = &t
	}

	// Validate since is not after until
	if req.Since != nil && req.Until != nil && req.Since.After(*req.Until) {
		return nil, fmt.Errorf("--since (%s) is after --until (%s)",
			lsSinceFlag, lsUntilFlag)
	}

	missions, err := client.ListMissions(req)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list missions")
	}
	return missions, nil
}
```

**Step 4: Update runMissionLs for display limit and feedback**

Add a helper to check if time filters are active:

```go
func hasTimeFilter() bool {
	return lsSinceFlag != "" || lsUntilFlag != ""
}
```

In `runMissionLs`, replace the display limit logic (lines 63-66):

```go
displayMissions := missions
if !lsAllFlag && !hasTimeFilter() && totalCount > defaultMissionLsLimit {
	displayMissions = missions[:defaultMissionLsLimit]
}
```

Add feedback messaging. Before the table print (before line 104 `tbl.Print()`), add:

```go
if hasTimeFilter() {
	fmt.Println(formatTimeFilterMessage(len(displayMissions), lsSinceFlag, lsUntilFlag))
}
```

Add the feedback formatting function:

```go
// formatTimeFilterMessage returns a summary line for time-filtered results.
func formatTimeFilterMessage(count int, since, until string) string {
	noun := "missions"
	if count == 1 {
		noun = "mission"
	}
	if since != "" && until != "" {
		return fmt.Sprintf("Showing %d %s created between %s and %s", count, noun, since, until)
	}
	if since != "" {
		return fmt.Sprintf("Showing %d %s created since %s", count, noun, since)
	}
	return fmt.Sprintf("Showing %d %s created until %s", count, noun, until)
}
```

Update the empty-result messages in `runMissionLs`. Replace the existing empty check (lines 53-60):

```go
if len(missions) == 0 {
	if hasTimeFilter() {
		if lsSinceFlag != "" && lsUntilFlag != "" {
			fmt.Printf("No missions found between %s and %s.\n", lsSinceFlag, lsUntilFlag)
		} else if lsSinceFlag != "" {
			fmt.Printf("No missions found since %s.\n", lsSinceFlag)
		} else {
			fmt.Printf("No missions found until %s.\n", lsUntilFlag)
		}
	} else if lsAllFlag {
		fmt.Println("No missions.")
	} else {
		fmt.Println("No active missions.")
	}
	return nil
}
```

Also update the "and N more" message at the bottom to not show when time filters are active (lines 106-109). The existing condition `!lsAllFlag` already controls this — just add `&& !hasTimeFilter()` to the condition:

```go
if !lsAllFlag && !hasTimeFilter() && totalCount > defaultMissionLsLimit {
```

**Step 5: Build and run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: Compiles and passes.

**Step 6: Commit**

```
git add cmd/mission_ls.go
git commit -m "Add --since and --until flags to mission ls"
```

---

### Task 7: Add E2E tests for time filtering

**Files:**
- Modify: `scripts/e2e-test.sh` (append before Summary section)

**Step 1: Add time filtering tests**

Insert before the Summary section (before line 259 `echo ""`):

```bash
echo "--- Mission time filtering ---"

# Create a mission so we have data to filter (server auto-starts)
run_test "create test mission for time filter" \
    0 \
    "${agenc_test}" mission new --repo "" --headless --prompt "time filter test"

# --since today should include it
run_test_output_contains "mission ls --since today includes recent" \
    "time filter test\|Showing" \
    "${agenc_test}" mission ls --since "$(date +%Y-%m-%d)"

# --until yesterday should exclude it
run_test "mission ls --until yesterday returns empty" \
    0 \
    "${agenc_test}" mission ls --until "$(date -v-1d +%Y-%m-%d)"

# --since after --until should fail
run_test "mission ls --since after --until fails" \
    1 \
    "${agenc_test}" mission ls --since 2026-12-01 --until 2026-01-01

# Invalid date format should fail
run_test "mission ls --since invalid format fails" \
    1 \
    "${agenc_test}" mission ls --since "not-a-date"
```

Note: The `date -v-1d` syntax is macOS-specific (Darwin), which matches the platform.

**Step 2: Run E2E tests**

Run: `make e2e` (with `dangerouslyDisableSandbox: true`)
Expected: All tests pass, including the new time filtering tests.

**Step 3: Commit**

```
git add scripts/e2e-test.sh
git commit -m "Add E2E tests for mission time filtering"
```

---

### Task 8: Final verification and push

**Step 1: Run full quality checks**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: All checks pass.

**Step 2: Push to remote**

```
git pull --rebase
git push
```
