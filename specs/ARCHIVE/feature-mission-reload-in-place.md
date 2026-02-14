# Feature Specification: Mission Reload In-Place

**Created**: 2026-02-14
**Type**: feature
**Status**: Open

---

Description
-----------

Implement `agenc mission reload` command that stops and restarts a mission in the exact same tmux pane, preserving window position, title, and user context. This replaces the current workflow of manually stopping and resuming missions across different windows.

The feature uses tmux's `remain-on-exit` option to keep panes alive after process exit, then uses `respawn-pane` to restart the wrapper in the same pane.

Context
-------

**User Need**: Currently, reloading a mission (e.g., after `agenc mission reconfig`) requires:
1. `agenc mission stop <uuid>` (closes the window)
2. `agenc tmux window new -a -- agenc mission resume <uuid>` (opens in a new window)

This disrupts the user's workspace organization. Windows change positions, context is lost, and the workflow is cumbersome.

**Research Findings**: Tmux provides `remain-on-exit` to keep panes alive after process exit (shows as "dead pane") and `respawn-pane` to restart processes in the same pane. This combination allows seamless in-place restarts without changing window/pane structure.

**Design Decision**: After evaluating multiple approaches (restart command, SIGHUP signal, tmux hooks), the selected approach is:
- Use `remain-on-exit on` to keep the pane alive after wrapper exits
- Stop the wrapper gracefully (SIGTERM)
- Use `tmux respawn-pane` to restart `agenc mission resume <uuid>` in the same pane
- Restore `remain-on-exit off` after reload completes

**Quality Score**: 9/10
- High reliability (uses tmux primitives correctly)
- Simple implementation (no wrapper state changes needed)
- Perfect UX (preserves exact pane position and window structure)
- Self-contained in reload command logic
- No database schema changes required

User Stories
------------

### Story 1: Reload after config update

**As a** developer, **I want** to reload my mission after updating its config, **so that** I can apply new settings without losing my workspace layout.

**Test Steps:**
1. **Setup**: Start a mission in the AgenC tmux session. Note the window number and pane position. Run `agenc mission reconfig <uuid>` to update the config.
2. **Action**: Run `agenc mission reload <uuid>` (or use command palette entry).
3. **Assert**:
   - Mission restarts in the same pane/window
   - Window title remains unchanged
   - Window number/position unchanged
   - Mission resumes with `claude -c` (conversation preserved)
   - Database `tmux_pane` column correctly restored

### Story 2: Reload after binary upgrade

**As a** user, **I want** to reload running missions after upgrading the agenc binary, **so that** missions pick up the new wrapper code without manual window juggling.

**Test Steps:**
1. **Setup**: Start 3 missions in different windows. Note their window numbers. Upgrade the agenc binary.
2. **Action**: Run `agenc mission reload` with fzf picker, select all 3 missions.
3. **Assert**:
   - All 3 missions restart in their original windows
   - Each preserves its window title
   - All conversations preserved (claude -c used)
   - No windows created or destroyed

### Story 3: Reload when not in tmux

**As a** developer, **I want** graceful behavior when reloading a mission that's not in tmux, **so that** the command works in all contexts.

**Test Steps:**
1. **Setup**: Start a mission outside tmux (e.g., `agenc mission new` in a regular terminal).
2. **Action**: Run `agenc mission reload <uuid>`.
3. **Assert**:
   - Command prints a warning: "Mission is not running in tmux; reload will not preserve pane position"
   - Falls back to stop + resume workflow
   - Mission restarts successfully (user must manually attach)

### Story 4: Reload via command palette

**As a** tmux user, **I want** a "Reload Mission" palette entry, **so that** I can quickly reload the focused mission without typing UUIDs.

**Test Steps:**
1. **Setup**: Start a mission in the AgenC tmux session, focus its pane.
2. **Action**: Open command palette (prefix + k), select "🔄 Reload Mission".
3. **Assert**:
   - Focused mission reloads in-place
   - Window/pane structure preserved
   - Palette closes automatically after execution

### Story 5: Reload when wrapper already stopped

**As a** user, **I want** reload to be idempotent, **so that** I can safely retry if something goes wrong.

**Test Steps:**
1. **Setup**: Start a mission, then stop it with `agenc mission stop <uuid>`. Pane/window remains.
2. **Action**: Run `agenc mission reload <uuid>`.
3. **Assert**:
   - Detects wrapper is not running (PID file missing or stale)
   - Skips stop phase, proceeds directly to respawn
   - Mission resumes successfully in the same pane

Implementation Plan
-------------------

### Phase 1: Core reload command

- [ ] Create `cmd/mission_reload.go`
  - [ ] Define `missionReloadCmd` cobra command
  - [ ] Implement `runMissionReload(cmd, args)` entry point
  - [ ] Accept mission ID as argument or use fzf picker (reuse `buildMissionPickerEntries`)
  - [ ] Support multi-select for batch reload
  - [ ] Call `reloadMission(db, missionID)` for each selected mission

- [ ] Implement `reloadMission(db *database.DB, missionID string) error`
  - [ ] Read mission record from database
  - [ ] Verify mission exists and is not archived
  - [ ] Check if wrapper is running (read PID file, check `daemon.IsProcessRunning`)
  - [ ] Detect tmux context:
    - [ ] Query database for `tmux_pane` value
    - [ ] If `tmux_pane` is NULL or empty, mission is not in tmux
    - [ ] Verify pane still exists: `tmux has-session -t <session>` and `tmux list-panes -t <window>` include pane ID
  - [ ] Branch on tmux detection:
    - [ ] **Tmux path**: Call `reloadMissionInTmux(db, missionRecord, paneID)`
    - [ ] **Non-tmux path**: Print warning, call `stopMissionWrapper(missionID)` then `resumeMission(db, missionID)` (reuse existing functions)

- [ ] Implement `reloadMissionInTmux(db *database.DB, mission *database.Mission, paneID string) error`
  - [ ] Resolve window ID from pane ID: `tmux display-message -p -t %<paneID> "#{window_id}"`
  - [ ] Set `remain-on-exit on` for the window: `tmux set-option -w -t <windowID> remain-on-exit on`
  - [ ] Stop wrapper gracefully: call `stopMissionWrapper(missionID)` (reuse from `mission_stop.go`)
  - [ ] Wait for wrapper to exit (already handled in `stopMissionWrapper`)
  - [ ] Respawn pane: `tmux respawn-pane -k -t %<paneID> 'agenc mission resume <missionID>'`
    - [ ] Use absolute path to `agenc` binary (resolve via `os.Executable()`)
    - [ ] Include `-k` flag to kill existing process (no-op since already stopped, but ensures clean state)
  - [ ] Restore `remain-on-exit off`: `tmux set-option -wu -t <windowID> remain-on-exit`
  - [ ] Print success message: "Mission <short_id> reloaded in-place"

### Phase 2: Tmux integration

- [ ] Update `internal/config/agenc_config.go`
  - [ ] Modify `BuiltinPaletteCommands` map
  - [ ] Update `reloadMission` entry:
    - Old: `Command: "agenc mission stop $AGENC_CALLING_MISSION_UUID && agenc tmux window new -a -- agenc mission resume $AGENC_CALLING_MISSION_UUID"`
    - New: `Command: "agenc mission reload $AGENC_CALLING_MISSION_UUID"`
  - [ ] Keep existing title/description/keybinding (if any)

- [ ] Verify tmux keybinding generation includes reload command
  - [ ] Check `internal/tmux/keybindings.go`
  - [ ] Ensure `GenerateKeybindingsContent` processes palette commands correctly
  - [ ] No changes needed (existing logic handles command palette entries)

### Phase 3: Edge case handling

- [ ] Handle mission with no conversation
  - [ ] In `reloadMissionInTmux`, check if conversation exists (reuse `missionHasConversation` from `mission_resume.go`)
  - [ ] If no conversation, respawn with fresh session: `agenc mission resume <missionID>` (existing logic handles this)
  - [ ] Document behavior: reload preserves conversation state automatically

- [ ] Handle pane closed externally
  - [ ] In `reloadMissionInTmux`, verify pane exists before setting `remain-on-exit`
  - [ ] If pane doesn't exist, print error: "Mission's tmux pane no longer exists; use `agenc mission resume` to restart in a new window"
  - [ ] Return error (don't fall back to non-tmux path, as that would create a new window)

- [ ] Handle tmux not available
  - [ ] In `runMissionReload`, check if `tmux` command exists before processing
  - [ ] If not found and any mission has `tmux_pane` set, print error: "Reload requires tmux, but tmux is not installed or not in PATH"
  - [ ] Suggest: "Use `agenc mission stop && agenc mission resume` instead"

- [ ] Handle old-format mission (no agent/ subdirectory)
  - [ ] In `reloadMission`, check if `agent/` exists (reuse pattern from `mission_resume.go`)
  - [ ] If old format, return error with migration instructions (same as resume command)

### Phase 4: Testing and documentation

- [ ] Add unit tests
  - [ ] Create `cmd/mission_reload_test.go`
  - [ ] Test mission ID resolution (canonical vs fzf picker)
  - [ ] Test tmux detection logic (with/without `tmux_pane`)
  - [ ] Test error cases (mission not found, archived, old format)

- [ ] Add integration tests
  - [ ] Create test that:
    - Starts a mission in tmux
    - Runs reload command
    - Verifies pane ID unchanged
    - Verifies window ID unchanged
    - Verifies conversation preserved

- [ ] Update documentation
  - [ ] Add CLI reference: `docs/cli/agenc_mission_reload.md` (auto-generated by cobra)
  - [ ] Update architecture doc: mention reload command in "Mission Lifecycle" section
  - [ ] Update README.md: add reload to common workflows section

Technical Details
-----------------

**Modules to create/modify**:
- New: `cmd/mission_reload.go` — main command implementation
- Modify: `internal/config/agenc_config.go` — update palette command
- Modify: `docs/system-architecture.md` — document reload in lifecycle

**Key changes**:
1. **Tmux primitives used**:
   - `remain-on-exit on/off` — window-scoped option to prevent pane auto-close
   - `respawn-pane -k -t %<paneID> <command>` — restart process in existing pane
   - `display-message -p -t %<paneID> "#{window_id}"` — resolve pane to window

2. **Reuse existing functions**:
   - `stopMissionWrapper(missionID)` from `cmd/mission_stop.go` — graceful shutdown with SIGTERM/SIGKILL
   - `resumeMission(db, missionID)` from `cmd/mission_resume.go` — wrapper spawning with conversation detection
   - `missionHasConversation(agencDirpath, missionID)` from `cmd/mission_resume.go` — check for resumable session
   - `buildMissionPickerEntries(db, missions)` from `cmd/mission_helpers.go` — fzf picker integration

3. **Database interaction**:
   - Read `tmux_pane` column to detect tmux context
   - No writes needed (wrapper's `registerTmuxPane()` handles re-registration on spawn)

4. **Error handling**:
   - Graceful degradation when not in tmux (warn + fallback)
   - Clear error messages for missing panes, old-format missions
   - Idempotent behavior (safe to retry)

**Key dependencies**:
- `github.com/spf13/cobra` — CLI framework (existing)
- `os/exec` — tmux command execution (existing)
- Database and config packages (existing)

Testing Strategy
----------------

**Unit tests** (`cmd/mission_reload_test.go`):
- Mission ID resolution (short ID, full UUID, fzf picker)
- Tmux detection logic (with/without `tmux_pane` value)
- Error paths (not found, archived, no tmux, old format)
- Mock tmux commands to verify correct flags/arguments

**Integration tests** (new test in `cmd/` or `internal/wrapper/`):
- Start mission in tmux (requires tmux integration test harness)
- Capture pane ID and window ID
- Run reload command
- Verify pane/window IDs unchanged
- Verify wrapper PID changed (new process)
- Verify conversation preserved (claude -c used)
- Verify `tmux_pane` database column re-populated

**E2E tests** (manual QA checklist):
- Reload after `agenc mission reconfig`
- Reload multiple missions via multi-select picker
- Reload mission started outside tmux (verify fallback)
- Reload when pane manually closed (verify error message)
- Reload via command palette (prefix + k)
- Reload when wrapper already stopped (verify idempotent)

**Edge case tests**:
- Mission with no conversation (verify fresh session)
- Old-format mission (verify error + migration instructions)
- Tmux not installed (verify clear error)
- Binary path with spaces (verify proper quoting in respawn command)

Acceptance Criteria
-------------------

- [ ] `agenc mission reload <uuid>` restarts mission in same tmux pane
- [ ] Window number, position, and title preserved
- [ ] Conversation preserved (uses `claude -c`)
- [ ] Command palette entry "🔄 Reload Mission" uses new command
- [ ] Works with fzf picker (no args) and multi-select
- [ ] Graceful fallback when mission not in tmux
- [ ] Clear error messages for edge cases (pane gone, old format, no tmux)
- [ ] Idempotent (safe to retry if wrapper already stopped)
- [ ] No database schema changes required
- [ ] Documentation updated (CLI reference, architecture doc)
- [ ] Integration tests pass

Risks & Considerations
----------------------

**Risk: Tmux version compatibility**
- Mitigation: `remain-on-exit` and `respawn-pane` exist in tmux 1.9+ (project requires 3.0+, so safe)
- Document minimum tmux version in README if not already present

**Risk: Pane closed between stop and respawn**
- Mitigation: Set `remain-on-exit on` BEFORE stopping wrapper
- Restore `remain-on-exit off` AFTER respawn completes
- If pane manually closed during operation, return clear error (don't auto-create window)

**Risk: Absolute path to agenc binary**
- Mitigation: Use `os.Executable()` to resolve path (already used in `tmux_attach.go`)
- Quote path in respawn command to handle spaces

**Risk: Race condition with wrapper re-registration**
- Mitigation: Wrapper's `registerTmuxPane()` writes to database on startup (existing behavior)
- Database `tmux_pane` cleared by old wrapper on exit, re-populated by new wrapper
- No coordination needed (wrapper lifecycle handles it)

**Risk: User confusion about reload vs restart**
- Mitigation: Use consistent terminology in docs/help text
- "Reload" = in-place restart (preserves pane)
- "Restart" = graceful/hard via socket (no pane change)
- "Resume" = start stopped mission (may create new window)

**Consideration: Support for headless missions**
- Decision: Reload command only supports interactive missions (those with `tmux_pane`)
- Headless missions are cron-spawned and exit naturally; no reload needed
- If user tries to reload headless mission, return clear error: "Cannot reload headless mission"

**Consideration: Future enhancement — reload all running missions**
- Out of scope for initial implementation
- Could add `agenc mission reload --all` flag later
- Would iterate over missions with non-null `tmux_pane` and running wrapper PID

---

### Critical Files for Implementation

- `cmd/mission_reload.go` — Core implementation: new command with tmux reload logic
- `cmd/mission_stop.go` — Reference pattern: `stopMissionWrapper()` function to reuse
- `cmd/mission_resume.go` — Reference pattern: `resumeMission()` and `missionHasConversation()` to reuse
- `internal/config/agenc_config.go` — Update palette command: change `reloadMission` entry to use new command
- `internal/database/database.go` — Reference for database schema: `tmux_pane` column usage and queries
