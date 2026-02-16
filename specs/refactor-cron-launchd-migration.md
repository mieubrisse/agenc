# Migrate Cron Scheduling from Custom Scheduler to macOS launchd

**Created**: 2026-02-16
**Type**: refactor-architecture
**Status**: Open

## Description

Replace the custom cron scheduler in `internal/daemon/cron_scheduler.go` with a macOS launchd-based implementation where the daemon dynamically generates and manages launchd plist files based on the `crons` section of `config.yml`. Each cron job will be represented as a separate plist in `~/Library/LaunchAgents/`, and the daemon will be responsible for keeping these plists synchronized with the configuration file.

## Context

### User Request
The user requested migration from the custom cron scheduler to macOS launchd for better reliability, OS-level scheduling, and reduced daemon complexity.

### Current Problems
1. **Polling overhead**: 60-second polling loop runs continuously even when no crons are configured
2. **In-memory state**: Cron state lives only in daemon memory, lost on restart
3. **Manual scheduling**: Custom implementation of cron expression parsing and timing
4. **Double-fire bug**: The guard against double-firing uses the wrong database query (checks by cron_id instead of mission_id)
5. **Resource usage**: Daemon must stay running for crons to execute

### Design Decision Rationale
**Selected: Option 1 - Daemon-Managed Plist Sync (Score: 8.5/10)**

This option scored highest because:
- **Leverages existing patterns**: Reuses the fsnotify config watcher pattern already in the codebase
- **Minimal user disruption**: No changes to config.yml format or CLI commands
- **Clean daemon architecture**: Removes scheduling loop, daemon becomes pure plist sync manager
- **OS-level reliability**: launchd handles scheduling, survives daemon restarts
- **Simpler than alternatives**: No wrapper scripts (Option 2), no cron daemon interaction (Option 3)

Trade-offs accepted:
- Daemon must run to sync config changes (acceptable since daemon already required for missions)
- macOS-specific solution (acceptable given project scope)
- No max-concurrent limiting (acceptable since macOS handles process management)

## User Stories

### Story 1: Dynamic Cron Synchronization
**As an** AgenC user, **I want** my cron configuration changes in `config.yml` to automatically update launchd, **so that** I don't need to manually manage plist files or restart services.

**Test Steps:**
1. Setup: Daemon running, one cron configured in config.yml
2. Action: Edit config.yml to add a second cron job, save file
3. Assert:
   - Within 1 second, two plist files exist in `~/Library/LaunchAgents/`
   - Both plists are loaded via `launchctl list | grep agenc-cron`
   - `agenc cron ls` shows both crons with status "enabled"

### Story 2: Cron Execution After Daemon Restart
**As an** AgenC user, **I want** my scheduled cron jobs to continue executing even after the daemon restarts, **so that** my automation remains reliable.

**Test Steps:**
1. Setup: Cron configured to run every minute, daemon running, at least one successful execution logged
2. Action: Kill the daemon process (`pkill agenc`), wait 90 seconds, restart daemon
3. Assert:
   - `agenc cron history` shows at least one new execution during daemon downtime
   - Logs show launchd spawned the mission, not the daemon
   - Daemon startup logs show "synced N cron plists from config"

### Story 3: Cron Disable/Enable Toggle
**As an** AgenC user, **I want** to disable a cron job without removing it from config.yml, **so that** I can temporarily pause automation.

**Test Steps:**
1. Setup: Cron configured with `enabled: true`, daemon running, plist loaded
2. Action: Run `agenc cron disable my-cron`
3. Assert:
   - `agenc cron ls` shows status "disabled"
   - `launchctl list | grep my-cron` returns empty (plist unloaded)
   - Plist file still exists in `~/Library/LaunchAgents/` but is not scheduled
4. Action: Run `agenc cron enable my-cron`
5. Assert:
   - `agenc cron ls` shows status "enabled"
   - `launchctl list | grep my-cron` shows the job loaded

### Story 4: Cron Deletion Cleanup
**As an** AgenC user, **I want** removed cron jobs to be cleaned up from launchd automatically, **so that** I don't accumulate stale plists.

**Test Steps:**
1. Setup: Two crons configured, daemon running, both plist files exist and loaded in launchd
2. Action: Remove one cron from config.yml, save file
3. Assert:
   - Only one plist remains in `~/Library/LaunchAgents/`
   - `launchctl list | grep agenc-cron` shows only one job (removed cron unloaded from launchd)
   - `agenc cron ls` shows only the remaining cron

### Story 5: Double-Fire Prevention
**As an** AgenC user, **I want** each cron trigger to create exactly one mission, **so that** I don't waste resources on duplicate executions.

**Test Steps:**
1. Setup: Cron scheduled to run every minute, execution takes 90 seconds
2. Action: Wait for two consecutive scheduled times (minute N and minute N+1)
3. Assert:
   - `agenc mission ls` shows exactly 2 missions created, not 3 or more
   - Second mission started only after first mission completed
   - Logs show "skipping cron trigger: previous mission still running"

## Implementation Plan

### Phase 1: Plist Generation and Sync Infrastructure
- [ ] Create `internal/launchd/plist.go` module for plist generation
  - [ ] Struct `Plist` with fields: Label, ProgramArguments, StartCalendarInterval, StandardOutPath, StandardErrorPath
  - [ ] Method `GeneratePlistXML() ([]byte, error)` to render plist XML
  - [ ] Method `WriteToDisk(targetPath string) error` to write plist atomically
  - [ ] Function `CronToPlistFilename(cronName string) string` to generate `agenc-cron-{cronName}.plist`
  - [ ] Function `PlistDirpath() string` returning `~/Library/LaunchAgents/`
- [ ] Create `internal/launchd/manager.go` for launchctl operations
  - [ ] Method `LoadPlist(plistPath string) error` wrapping `launchctl load`
  - [ ] Method `UnloadPlist(plistPath string) error` wrapping `launchctl unload`
  - [ ] Method `IsLoaded(label string) bool` checking `launchctl list`
  - [ ] Method `RemovePlist(plistPath string) error` for cleanup (MUST: unload from launchd THEN delete file)
- [ ] Create `internal/daemon/cron_syncer.go` to replace cron_scheduler.go
  - [ ] Method `SyncCronsToLaunchd(crons []config.CronConfig) error`
  - [ ] Logic: compute desired plists, compare to existing files, add/update/remove as needed
  - [ ] Reconciliation on startup: scan ~/Library/LaunchAgents/ for agenc-cron-*.plist files, remove orphans not in config (unload + delete)
  - [ ] Handle enable/disable state: unload disabled crons (but keep plist file), load enabled crons
- [ ] Update `internal/daemon/daemon.go` to integrate syncer
  - [ ] Replace `cronScheduler` field with `cronSyncer`
  - [ ] In `Run()`: call `cronSyncer.SyncCronsToLaunchd()` on startup
  - [ ] In config watcher callback: call `cronSyncer.SyncCronsToLaunchd()` on config change

### Phase 2: Cron Command Compatibility
- [ ] Audit `cmd/cron_*.go` commands for launchd compatibility
  - [ ] `cron ls`: read from database (enabled/disabled state), no changes needed
  - [ ] `cron history`: read from database, no changes needed
  - [ ] `cron logs`: read from mission logs at `$AGENC_DIRPATH/missions/<uuid>/claude-output.log`, no changes needed
  - [ ] `cron enable <name>`: set `enabled: true` in config.yml, save, let watcher trigger sync
  - [ ] `cron disable <name>`: set `enabled: false` in config.yml, save, let watcher trigger sync
- [ ] Update `cmd/cron_enable.go` and `cmd/cron_disable.go` to modify config.yml directly
  - [ ] Load config.yml using existing config package
  - [ ] Mutate the matching cron's `enabled` field
  - [ ] Write back to config.yml using YAML marshaler
  - [ ] Output: "Cron '{name}' {enabled|disabled}. Daemon will sync plists within 1 second."

### Phase 3: Double-Fire Prevention Fix
- [ ] Update `internal/db/missions.go` to add query function
  - [ ] Add `GetMostRecentMissionForCron(cronID string) (*Mission, error)`
  - [ ] Query: `SELECT * FROM missions WHERE cron_id = ? ORDER BY created_at DESC LIMIT 1`
- [ ] Update plist generation to invoke with `--headless` flag
  - [ ] ProgramArguments: `["/path/to/agenc", "mission", "new", "--headless", "--cron-trigger={cronName}", "{prompt}"]`
- [ ] Update `cmd/mission_new.go` to check for running mission on cron trigger
  - [ ] When `--cron-trigger` flag present, query `GetMostRecentMissionForCron(cronName)`
  - [ ] If mission exists and status != "completed", log "Skipping cron trigger: previous mission still running" and exit 0
  - [ ] If no running mission, proceed with mission creation

### Phase 4: Cleanup and Removal of Old Scheduler
- [ ] Delete `internal/daemon/cron_scheduler.go` entirely
- [ ] Remove all references to `cronScheduler` from daemon.go
- [ ] Remove `maxConcurrentMissions` from config.yml schema and database (no longer needed)
- [ ] Update `docs/system-architecture.md` to reflect launchd-based scheduling
  - [ ] Section "Cron Scheduling" should describe daemon plist sync instead of polling loop
  - [ ] Diagram showing config.yml → fsnotify → daemon → launchd plist sync

### Phase 5: Testing and Validation
- [ ] Unit tests for `internal/launchd/plist.go`
  - [ ] Test XML generation for various cron schedules
  - [ ] Test filename sanitization (spaces, special chars)
- [ ] Unit tests for `internal/launchd/manager.go`
  - [ ] Mock `launchctl` commands, test load/unload/isLoaded
- [ ] Integration test for full sync workflow
  - [ ] Start daemon, modify config.yml, assert plists created
  - [ ] Disable cron, assert plist unloaded
  - [ ] Remove cron, assert plist deleted
- [ ] Manual smoke test checklist (see Testing Strategy section)

## Technical Details

### Modules to Create/Modify

#### New Files
- `internal/launchd/plist.go` - Plist struct and XML generation
- `internal/launchd/manager.go` - launchctl wrapper functions
- `internal/daemon/cron_syncer.go` - Replaces cron_scheduler.go

#### Modified Files
- `internal/daemon/daemon.go` - Remove cronScheduler, add cronSyncer
- `cmd/cron_enable.go` - Modify config.yml instead of sending daemon command
- `cmd/cron_disable.go` - Modify config.yml instead of sending daemon command
- `cmd/mission_new.go` - Add `--cron-trigger` flag and double-fire prevention logic
- `internal/database/missions.go` - Add `GetMostRecentMissionForCron()`
- `docs/system-architecture.md` - Update cron scheduling section

#### Deleted Files
- `internal/daemon/cron_scheduler.go`

### Key Technical Changes

#### Launchd Plist Format
Each cron generates a plist with this structure:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>agenc-cron-{cronName}</string>
    <key>ProgramArguments</key>
    <array>
        <string>/absolute/path/to/agenc</string>
        <string>mission</string>
        <string>new</string>
        <string>--headless</string>
        <string>--cron-trigger={cronName}</string>
        <string>{prompt}</string>
    </array>
    <key>StartCalendarInterval</key>
    <dict>
        <key>Minute</key>
        <integer>0</integer>
        <key>Hour</key>
        <integer>9</integer>
    </dict>
    <key>StandardOutPath</key>
    <string>/dev/null</string>
    <key>StandardErrorPath</key>
    <string>/dev/null</string>
</dict>
</plist>
```

For cron expressions like `0 9 * * *`, parse into StartCalendarInterval dict. For complex expressions not supported by launchd (e.g., `*/5 * * * *`), log warning and skip plist generation.

#### Fsnotify Config Watcher Integration
Reuse existing watcher in `daemon.go`:

```go
// Existing pattern in daemon.go around line 200
watcher.Add(configPath)
case event := <-watcher.Events:
    if event.Op&fsnotify.Write == fsnotify.Write {
        // Existing config reload logic
        // Add: cronSyncer.SyncCronsToLaunchd(newConfig.Crons)
    }
```

#### Launchctl Command Execution
Use `exec.Command` with proper error handling:

```go
cmd := exec.Command("launchctl", "load", plistPath)
output, err := cmd.CombinedOutput()
if err != nil {
    return stacktrace.Propagate(err, "Failed to load plist: %s", string(output))
}
```

#### Double-Fire Prevention Flow
1. launchd triggers at scheduled time
2. Invokes `agenc mission new --headless --cron-trigger=my-cron "Do X"`
3. `mission_new.go` checks `--cron-trigger` flag
4. Queries database for most recent mission with matching cron_id
5. If found and status != "completed", exit 0 (skip)
6. Otherwise, proceed with normal mission creation

### Dependencies
- **launchctl**: macOS built-in, no installation needed
- **fsnotify**: Already in go.mod (used by config watcher)
- **gronx**: Can be removed from go.mod after migration
- **YAML parser**: Already in go.mod (for config.yml manipulation)

## Testing Strategy

### Unit Tests

#### `internal/launchd/plist_test.go`
- `TestGeneratePlistXML`: Verify XML structure for various cron schedules
- `TestCronToPlistFilename`: Verify filename sanitization (spaces → dashes, special chars removed)
- `TestWriteToDisk`: Verify atomic write and file permissions (0644)

#### `internal/launchd/manager_test.go`
- `TestLoadPlist`: Mock launchctl, verify command arguments
- `TestUnloadPlist`: Mock launchctl, verify unload command
- `TestIsLoaded`: Mock `launchctl list`, test parsing output

#### `internal/daemon/cron_syncer_test.go`
- `TestSyncCronsToLaunchd_AddNew`: Assert new plists created and loaded
- `TestSyncCronsToLaunchd_RemoveOld`: Assert deleted crons removed from disk
- `TestSyncCronsToLaunchd_UpdateExisting`: Assert modified crons regenerate plist
- `TestSyncCronsToLaunchd_DisabledCron`: Assert disabled cron unloaded but not deleted

#### `internal/database/missions_test.go`
- `TestGetMostRecentMissionForCron`: Insert multiple missions, verify correct one returned

### Integration Tests

#### `test/integration/cron_lifecycle_test.go`
1. Start daemon with temp config.yml (one cron)
2. Assert plist exists and is loaded
3. Add second cron to config, wait 1s
4. Assert two plists exist
5. Disable first cron
6. Assert first plist unloaded
7. Remove second cron from config
8. Assert second plist deleted

#### `test/integration/double_fire_test.go`
1. Configure cron to run every minute
2. Mock a long-running mission (sleep 90s)
3. Trigger cron manually via `agenc mission new --cron-trigger=test`
4. Wait 10s, trigger again
5. Assert only one mission in database

### Manual Verification Checklist
- [ ] Fresh install: `make build && ./agenc daemon start`, add cron via config.yml, verify plist created
- [ ] Config hot-reload: Edit config.yml while daemon running, verify plist updated within 1s
- [ ] Daemon restart: Kill daemon, verify cron executes via launchd, restart daemon, verify sync on startup
- [ ] Disable/enable: Run `agenc cron disable`, verify `launchctl list` shows unloaded, re-enable, verify loaded
- [ ] Delete cron: Remove from config.yml, verify plist file deleted from `~/Library/LaunchAgents/`
- [ ] Double-fire: Schedule cron every 1min, create slow mission, verify no duplicate missions
- [ ] Commands work: `agenc cron ls`, `agenc cron history`, `agenc cron logs` all return expected data

## Acceptance Criteria

- [ ] No `cron_scheduler.go` file exists in the codebase
- [ ] Daemon startup logs "Synced N cron plists from config" message
- [ ] Config watcher triggers plist sync within 500ms of config.yml write
- [ ] Each enabled cron has exactly one plist in `~/Library/LaunchAgents/` with correct schedule
- [ ] `launchctl list | grep agenc-cron` shows all enabled crons loaded
- [ ] Disabled crons remain in config.yml but are not loaded in launchd
- [ ] Removed crons trigger plist deletion and launchctl unload
- [ ] `agenc cron enable/disable` commands modify config.yml and trigger sync
- [ ] `agenc cron ls/history/logs` commands return correct data
- [ ] Double-fire prevention: no duplicate missions for crons scheduled more frequently than mission duration
- [ ] All unit tests pass: `go test ./internal/launchd/... ./internal/daemon/...`
- [ ] Integration tests pass: `go test ./test/integration/...`
- [ ] Manual smoke test checklist completed with no failures
- [ ] `docs/system-architecture.md` updated to reflect launchd-based architecture

## Risks & Considerations

### Risk: Cron Expression Compatibility
**Issue**: launchd's `StartCalendarInterval` is less flexible than cron expressions (e.g., no `*/5` syntax for "every 5 minutes").

**Mitigation**:
- Document supported formats in config.yml comments
- Log warning and skip unsupported expressions during sync
- Consider future enhancement: generate multiple StartCalendarInterval dicts for simple intervals (e.g., `*/5` → 5 separate intervals)

### Risk: Config.yml Write Conflicts
**Issue**: Multiple agents or users editing config.yml simultaneously could cause race conditions in enable/disable commands.

**Mitigation**:
- Use file locking (flock) when modifying config.yml
- Read-modify-write with retry logic on conflict
- Defer to existing config package patterns (check if already handles this)

### Risk: Orphaned Plists After Unclean Shutdown
**Issue**: If daemon crashes during sync, plists may be left in inconsistent state (created but not loaded, or loaded but config.yml doesn't match).

**Mitigation**:
- Sync operation is idempotent: on startup, reconcile all plists with config.yml
- Daemon startup always runs full sync, fixing any inconsistencies
- Atomic file writes for plists (write to temp file, rename)

### Risk: User Permission Issues
**Issue**: `~/Library/LaunchAgents/` may not be writable in some edge cases (restrictive permissions, disk full).

**Mitigation**:
- Check directory writability on daemon startup, log fatal error if not writable
- Wrap all file operations in error handling with clear messages
- Document required permissions in README

### Risk: launchctl Command Availability
**Issue**: launchctl could be missing or broken on user's system.

**Mitigation**:
- On daemon startup, run `launchctl version` to verify availability
- Log fatal error if launchctl not found
- Document macOS requirement (10.10+ or similar)

### Risk: Migration Path for Existing Users
**Issue**: Users upgrading from old cron scheduler will have in-flight cron state that won't migrate automatically.

**Mitigation**:
- Not critical: cron state is ephemeral (only "last run time"), can be recomputed from mission history
- On first startup after upgrade, sync all crons to launchd regardless of previous state
- Log message: "Migrated cron configuration to launchd"
- Consider: query database for most recent mission per cron_id to initialize "last run" state

### Risk: Plist Orphan Detection After Uninstall/Reinstall
**Issue**: If user uninstalls and reinstalls AgenC, old plists may be left behind in both launchd and the filesystem.

**Mitigation**:
- All plists use `agenc-cron-{cronName}` naming pattern as AgenC's namespace
- On every daemon startup, syncer scans `~/Library/LaunchAgents/` for `agenc-cron-*.plist`
- Any plists found that aren't in current config.yml are: (1) unloaded from launchd via `launchctl unload`, (2) deleted from filesystem
- This two-step removal (unload then delete) ensures both launchd state and files are cleaned up
- This reconciliation ensures orphaned plists are always cleaned up
- Users can manually clean up with: `launchctl list | grep agenc-cron | awk '{print $3}' | xargs -n1 launchctl remove && rm ~/Library/LaunchAgents/agenc-cron-*.plist`
