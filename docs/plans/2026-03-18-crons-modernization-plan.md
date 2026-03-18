Crons Modernization Implementation Plan
========================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Make the crons feature functional by connecting launchd-scheduled jobs to
the tmux pool mission system, replacing cron-specific DB columns with generic
source tracking, and removing dead complexity.

**Architecture:** launchd fires `agenc mission new --headless` with source flags →
server creates a normal mission in the tmux pool (no linked session) → standard
idle timeout applies. No special cron lifecycle.

**Tech Stack:** Go, SQLite, launchd plists, Cobra CLI

**Design doc:** `docs/plans/2026-03-18-crons-modernization-design.md`

---

Task 1: Add source columns to database
---------------------------------------

**Files:**
- Modify: `internal/database/migrations.go` (add new migration)
- Modify: `internal/database/database.go` (register migration)
- Modify: `internal/database/missions.go` (update Mission struct, CreateMissionParams, ListMissionsParams)
- Modify: `internal/database/scanners.go` (update scanMission/scanMissions)
- Modify: `internal/database/queries.go` (update buildListMissionsQuery)
- Test: `internal/database/missions_test.go`

**Step 1: Write failing test for source columns**

Add a test that creates a mission with source fields and reads them back:

```go
func TestCreateMissionWithSource(t *testing.T) {
    db := setupTestDB(t)
    source := "cron"
    sourceID := "550e8400-e29b-41d4-a716-446655440000"
    sourceMetadata := `{"cron_name":"daily-sync"}`
    m, err := db.CreateMission("github.com/test/repo", &CreateMissionParams{
        Source:         &source,
        SourceID:       &sourceID,
        SourceMetadata: &sourceMetadata,
    })
    require.NoError(t, err)
    require.NotNil(t, m.Source)
    assert.Equal(t, "cron", *m.Source)
    require.NotNil(t, m.SourceID)
    assert.Equal(t, sourceID, *m.SourceID)
    require.NotNil(t, m.SourceMetadata)
    assert.Equal(t, sourceMetadata, *m.SourceMetadata)

    // Verify round-trip through ListMissions
    params := ListMissionsParams{IncludeArchived: true, Source: &source, SourceID: &sourceID}
    missions, err := db.ListMissions(params)
    require.NoError(t, err)
    require.Len(t, missions, 1)
    assert.Equal(t, m.ID, missions[0].ID)
}
```

**Step 2: Run test to verify it fails**

Run: `make check`
Expected: compilation errors — Source/SourceID/SourceMetadata fields don't exist

**Step 3: Add migration and update structs**

In `internal/database/migrations.go`, add SQL constants:

```go
addSourceColumnSQL         = `ALTER TABLE missions ADD COLUMN source TEXT;`
addSourceIDColumnSQL       = `ALTER TABLE missions ADD COLUMN source_id TEXT;`
addSourceMetadataColumnSQL = `ALTER TABLE missions ADD COLUMN source_metadata TEXT;`
```

Add migration function `migrateAddSourceColumns(conn *sql.DB) error` that:
1. Checks if `source` column exists (idempotent)
2. Adds `source`, `source_id`, `source_metadata` columns
3. Migrates existing cron data: `UPDATE missions SET source = 'cron', source_id = cron_id, source_metadata = json_object('cron_name', cron_name) WHERE cron_id IS NOT NULL`
4. Creates index: `CREATE INDEX IF NOT EXISTS idx_missions_source ON missions(source, source_id, created_at DESC)`

In `internal/database/database.go`, register the migration at the end of the list.

In `internal/database/missions.go`, update:
- `Mission` struct: add `Source *string`, `SourceID *string`, `SourceMetadata *string`
- `CreateMissionParams`: add `Source *string`, `SourceID *string`, `SourceMetadata *string`
- `ListMissionsParams`: add `Source *string`, `SourceID *string`
- `CreateMission`: add source/source_id/source_metadata to INSERT

In `internal/database/scanners.go`:
- Add `source, sourceID, sourceMetadata` to both `scanMission` and `scanMissions`
- Add `sql.NullString` vars and scan them
- Map valid values to `m.Source`, `m.SourceID`, `m.SourceMetadata`

In `internal/database/queries.go`, update `buildListMissionsQuery`:
- Add `source` and `source_id` columns to SELECT
- Add WHERE conditions for `params.Source` and `params.SourceID`

**Step 4: Run test to verify it passes**

Run: `make check`
Expected: PASS

**Step 5: Commit**

```
git add internal/database/
git commit -m "Add source/source_id/source_metadata columns to missions table"
```

---

Task 2: Add ID field to CronConfig
-----------------------------------

**Files:**
- Modify: `internal/config/agenc_config.go` (add ID field to CronConfig)
- Test: `internal/config/agenc_config_test.go`

**Step 1: Write failing test**

```go
func TestCronConfigID(t *testing.T) {
    yaml := `
crons:
  daily-sync:
    id: "550e8400-e29b-41d4-a716-446655440000"
    schedule: "0 9 * * *"
    prompt: "Sync repos"
`
    // Parse and verify ID field is populated
    cfg := parseTestConfig(t, yaml)
    require.NotEmpty(t, cfg.Crons["daily-sync"].ID)
    assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", cfg.Crons["daily-sync"].ID)
}
```

**Step 2: Run test to verify it fails**

Run: `make check`
Expected: CronConfig has no ID field

**Step 3: Add ID field**

In `internal/config/agenc_config.go`, add to `CronConfig`:

```go
type CronConfig struct {
    ID          string            `yaml:"id,omitempty"`          // UUID, auto-generated by cron new
    Schedule    string            `yaml:"schedule"`
    // ... rest unchanged
}
```

**Step 4: Run test to verify it passes**

Run: `make check`
Expected: PASS

**Step 5: Commit**

```
git add internal/config/
git commit -m "Add ID field to CronConfig for stable cron identity"
```

---

Task 3: Update server API to use source columns
------------------------------------------------

**Files:**
- Modify: `internal/server/missions.go` (MissionResponse, toMissionResponse, ToMission, handleCreateMission)
- Modify: `internal/server/client.go` (ListMissions signature)
- Test: `internal/server/missions_test.go`

**Step 1: Write failing test**

Test that creating a mission with source fields returns them in the response.

**Step 2: Run test to verify it fails**

Run: `make check`

**Step 3: Update server structs and handlers**

In `internal/server/missions.go`:

Update `MissionResponse`:
```go
Source         *string `json:"source"`
SourceID       *string `json:"source_id"`
SourceMetadata *string `json:"source_metadata"`
```

Update `CreateMissionRequest`:
```go
Source         string `json:"source"`
SourceID       string `json:"source_id"`
SourceMetadata string `json:"source_metadata"`
```

Update `toMissionResponse` and `ToMission` to map the new fields.

Update `handleCreateMission` (around line 286) to read source fields instead of
cron fields:
```go
if req.Source != "" {
    createParams.Source = &req.Source
}
if req.SourceID != "" {
    createParams.SourceID = &req.SourceID
}
if req.SourceMetadata != "" {
    createParams.SourceMetadata = &req.SourceMetadata
}
```

Remove the old CronID/CronName handling.

Update `handleListMissions` (around line 208) to filter by source/source_id
instead of cron_id:
```go
if source := r.URL.Query().Get("source"); source != "" {
    params.Source = &source
}
if sourceID := r.URL.Query().Get("source_id"); sourceID != "" {
    params.SourceID = &sourceID
}
```

Update `Client.ListMissions` to accept source/sourceID params instead of cronID:
```go
func (c *Client) ListMissions(includeArchived bool, source string, sourceID string) ([]*database.Mission, error) {
```

**Step 4: Run test to verify it passes**

Run: `make check`

**Step 5: Commit**

```
git add internal/server/
git commit -m "Update server API to use source columns instead of cron-specific fields"
```

---

Task 4: Update mission new flags
---------------------------------

**Files:**
- Modify: `cmd/mission_new.go` (replace cron flags with source flags, remove double-fire prevention, remove timeout)

**Step 1: Update flags**

In `cmd/mission_new.go`:

Remove variable declarations (lines 26-29):
- `timeoutFlag`
- `cronIDFlag`
- `cronNameFlag`
- `cronTriggerFlag`

Add new variable declarations:
```go
var sourceFlag string
var sourceIDFlag string
var sourceMetadataFlag string
```

In `init()`, remove old flag registrations (lines 54-61) and add:
```go
missionNewCmd.Flags().StringVar(&sourceFlag, "source", "", "mission source type (internal use)")
missionNewCmd.Flags().StringVar(&sourceIDFlag, "source-id", "", "mission source identifier (internal use)")
missionNewCmd.Flags().StringVar(&sourceMetadataFlag, "source-metadata", "", "mission source metadata JSON (internal use)")
missionNewCmd.Flags().MarkHidden("source")
missionNewCmd.Flags().MarkHidden("source-id")
missionNewCmd.Flags().MarkHidden("source-metadata")
```

Remove `--timeout` and `--headless` timeout validation. Keep `--headless` flag.

Remove the entire `shouldSkipCronTrigger` function (lines 110-131) and the
double-fire check block in `runMissionNew` (lines 78-87).

In `createAndLaunchMission`, update the `CreateMissionRequest` (around line 392):
```go
Source:         sourceFlag,
SourceID:       sourceIDFlag,
SourceMetadata: sourceMetadataFlag,
```

Remove `CronID`, `CronName`, `Timeout` from the request.

**Step 2: Fix all callers**

Search for all callers of `client.ListMissions` — `cron_history.go` and
`cron_logs.go` both call `client.ListMissions(true, name)`. These need to be
updated to the new signature. (cron_logs.go will be deleted in Task 6, but
cron_history.go needs updating.)

**Step 3: Run tests**

Run: `make check`
Expected: PASS

**Step 4: Commit**

```
git add cmd/mission_new.go
git commit -m "Replace cron flags with generic source flags, remove double-fire prevention"
```

---

Task 5: Update CronSyncer to use new flags and validate ID
-----------------------------------------------------------

**Files:**
- Modify: `internal/server/cron_syncer.go` (program args, log paths, UUID validation)
- Modify: `internal/config/config.go` (update GetCronLogFilepaths)
- Test: `internal/server/cron_syncer_test.go`

**Step 1: Write failing test**

Test that SyncCronsToLaunchd generates correct program arguments with source flags
and skips crons without an ID:

```go
func TestSyncCronsToLaunchd_UsesSourceFlags(t *testing.T) {
    // Create a cron with an ID
    crons := map[string]config.CronConfig{
        "daily-sync": {
            ID:       "550e8400-e29b-41d4-a716-446655440000",
            Schedule: "0 9 * * *",
            Prompt:   "Sync repos",
        },
    }
    // Sync and verify plist program arguments contain --source, --source-id, --source-metadata
}

func TestSyncCronsToLaunchd_SkipsCronsWithoutID(t *testing.T) {
    crons := map[string]config.CronConfig{
        "no-id-cron": {
            Schedule: "0 9 * * *",
            Prompt:   "No ID",
        },
    }
    // Sync and verify warning logged, no plist created
}
```

**Step 2: Run test to verify it fails**

Run: `make check`

**Step 3: Update CronSyncer**

In `internal/server/cron_syncer.go`, update `SyncCronsToLaunchd`:

Add UUID validation at the top of the loop (after line 56):
```go
if cronCfg.ID == "" {
    logger.Printf("Cron syncer: skipping '%s' - no ID configured (add an 'id' field to config.yml)", name)
    continue
}
```

Update program arguments (lines 68-86) to use new flags:
```go
programArgs := []string{
    execPath,
    "mission", "new",
    "--headless",
    "--source", "cron",
    "--source-id", cronCfg.ID,
    "--source-metadata", fmt.Sprintf(`{"cron_name":"%s"}`, name),
    "--prompt", cronCfg.Prompt,
}
```

Remove timeout from program args (the `if cronCfg.Timeout != ""` block).

Update log paths (line 89) to use cron ID:
```go
logFilepath := config.GetCronLogFilepath(s.agencDirpath, cronCfg.ID)
```

Set both StandardOutPath and StandardErrorPath to the same file.

In `internal/config/config.go`, replace `GetCronLogFilepaths`:
```go
// GetCronLogFilepath returns the log path for a cron job identified by its UUID.
func GetCronLogFilepath(agencDirpath string, cronID string) string {
    return filepath.Join(GetCronLogDirpath(agencDirpath), fmt.Sprintf("%s.log", cronID))
}
```

**Step 4: Run test to verify it passes**

Run: `make check`

**Step 5: Commit**

```
git add internal/server/cron_syncer.go internal/config/config.go
git commit -m "Update CronSyncer to use source flags and validate cron UUID"
```

---

Task 6: Update cron CLI commands
---------------------------------

**Files:**
- Modify: `cmd/cron_history.go` (use source-based filtering)
- Modify: `cmd/cron_run.go` (tag with source=cron, remove timeout)
- Delete: `cmd/cron_logs.go`
- Modify: `cmd/cron.go` (remove cronLogsCmd registration if it's there)

**Step 1: Update cron history**

In `cmd/cron_history.go`, update `runCronHistory`:
- Look up the cron's UUID from config: `cronCfg.ID`
- Call `client.ListMissions(true, "cron", cronCfg.ID)` instead of
  `client.ListMissions(true, name)`

**Step 2: Update cron run**

In `cmd/cron_run.go`, update `runCronRun`:
- Add `--source`, `--source-id`, `--source-metadata` to cmdArgs
- Remove `--timeout` from cmdArgs
- `--source-metadata` should include `"trigger":"manual"`

```go
cmdArgs := []string{
    "mission", "new",
    "--headless",
    "--source", "cron",
    "--source-id", cronCfg.ID,
    "--source-metadata", fmt.Sprintf(`{"cron_name":"%s","trigger":"manual"}`, name),
    "--prompt", cronCfg.Prompt,
}
```

Update the help text to remove references to "untracked by cron_id".

**Step 3: Delete cron logs**

Delete `cmd/cron_logs.go` entirely. Remove its registration from `cmd/cron.go` if
present (search for `cronLogsCmd`).

**Step 4: Run tests**

Run: `make check`
Expected: PASS

**Step 5: Commit**

```
git add cmd/cron_history.go cmd/cron_run.go cmd/cron.go
git rm cmd/cron_logs.go
git commit -m "Update cron CLI: source-based history, remove logs command"
```

---

Task 7: Update cron new to generate UUID
-----------------------------------------

**Files:**
- Modify: `cmd/cron_new.go` (generate UUID for new crons)
- Modify: `cmd/config_cron_add.go` (if exists, generate UUID)

**Step 1: Update cron new wizard**

In `cmd/cron_new.go`, when creating a new cron config, generate a UUID:

```go
import "github.com/google/uuid"

cronCfg := config.CronConfig{
    ID:       uuid.New().String(),
    Schedule: schedule,
    Prompt:   prompt,
    // ...
}
```

**Step 2: Update config cron add (if exists)**

Check `cmd/config_cron_add.go` — if it creates crons without UUIDs, add UUID
generation there too.

**Step 3: Run tests**

Run: `make check`
Expected: PASS

**Step 4: Commit**

```
git add cmd/cron_new.go cmd/config_cron_add.go
git commit -m "Generate UUID for new cron definitions"
```

---

Task 8: Clean up dead code references
--------------------------------------

**Files:**
- Modify: `internal/database/missions.go` (remove GetMostRecentMissionForCron)
- Modify: `internal/server/missions.go` (remove old cron fields from request handling)
- Modify: various files that reference CronID/CronName on Mission struct
- Modify: `cmd/mission_helpers.go` (if it references cron filtering)

**Step 1: Remove GetMostRecentMissionForCron**

Delete the function at `internal/database/missions.go:128-145`.

**Step 2: Search for remaining cron_id/cron_name references in Go code**

Run: `grep -rn "CronID\|CronName\|cron_id\|cron_name" --include="*.go"` and clean
up any remaining references in Go code. The DB columns stay — we're only removing
Go code references.

Keep the `CronID`/`CronName` fields on the `Mission` struct and in
scanners/queries for now (the DB columns still exist and the scanners need to read
them to avoid scan errors). Just make sure no new code writes to them.

**Step 3: Run tests**

Run: `make check`
Expected: PASS

**Step 4: Commit**

```
git add -A
git commit -m "Remove dead cron code: GetMostRecentMissionForCron, old references"
```

---

Task 9: End-to-end verification
--------------------------------

**Step 1: Build**

Run: `make build`
Expected: Clean build, binary produced

**Step 2: Manual test with a fast-firing cron**

Add a test cron to config.yml:

```yaml
crons:
  test-cron:
    id: "test-uuid-here"
    schedule: "* * * * *"  # every minute
    prompt: "Say hello and list the current directory"
    enabled: true
```

Start the server, wait for the cron to fire, verify:
- `agenc cron ls` shows the cron
- A mission appears in `agenc mission ls` after ~1 minute
- `agenc mission attach <id>` works and shows Claude's output
- `agenc cron history test-cron` shows the run
- Plist log exists at `$AGENC_DIRPATH/logs/crons/<uuid>.log`

**Step 3: Test disable/enable**

```
agenc cron disable test-cron
launchctl list | grep agenc-cron  # should be empty
agenc cron enable test-cron
launchctl list | grep agenc-cron  # should show the job
```

**Step 4: Clean up test cron**

Remove test cron from config.yml. Verify plist is cleaned up.

**Step 5: Commit any test fixes**

---

Task 10: Update documentation
------------------------------

**Files:**
- Modify: `docs/system-architecture.md` (update cron section)
- Modify: auto-generated CLI docs if applicable

**Step 1: Update system architecture**

Update the cron scheduling section to reflect:
- launchd-based scheduling via plist sync
- Generic source columns instead of cron-specific columns
- No double-fire prevention or timeout enforcement

**Step 2: Regenerate CLI docs if applicable**

Run whatever generates `docs/cli/` files (check Makefile).

**Step 3: Commit**

```
git add docs/
git commit -m "Update architecture docs for crons modernization"
```
