Mission Update Command & Config Column in `ls -a`
==================================================

**Created**: 2026-02-11  **Type**: feature  **Status**: Open

---

Description
-----------

Rename `mission update-config` to `mission update` (keeping the old name as an
alias) and add a CONFIG column to `mission ls -a` that shows each mission's
pinned config commit hash and how far behind it is from the current shadow repo
HEAD.

Context
-------

The `mission update-config` command already exists and handles the full config
rebuild workflow: comparing commits, showing diffs, rebuilding the per-mission
config directory, and updating the DB. The user wants:

1. A shorter command name (`mission update` instead of `mission update-config`)
2. Visibility into which config version each mission is running
3. No automatic Claude restart — the user will stop and restart manually

The existing command signals running missions to restart via the wrapper socket.
This auto-restart behavior will be removed per the user's explicit request.

Design Decision
---------------

**Option A selected** (Quality score: 8.5/10): Rename with alias, CONFIG column
on `-a` only, remove auto-restart signaling.

This is the minimal, focused approach: rename the command (with backward-compat
alias), add one column to the extended listing, and remove the restart signal.
No new commands or flags needed.

User Stories
------------

### View config staleness at a glance

**As a** mission operator, **I want** to see which config version each mission
is on when listing missions, **so that** I can identify which missions need a
config update.

**Test Steps:**

1. **Setup**: Create 3 missions. Update `~/.claude/CLAUDE.md` twice so the
   shadow repo advances 2 commits past the first mission's pinned commit.
2. **Action**: Run `agenc mission ls -a`
3. **Assert**: Each mission shows its 12-char config hash. Missions behind HEAD
   show `(-N)` in red where N is the number of commits behind. A mission at
   HEAD shows no suffix.

### Update a mission's config

**As a** mission operator, **I want** to run `agenc mission update` to rebuild
a mission's config, **so that** it picks up my latest `~/.claude` changes.

**Test Steps:**

1. **Setup**: Create a mission, then modify `~/.claude/CLAUDE.md` so the shadow
   repo advances.
2. **Action**: Run `agenc mission update <short-id>`
3. **Assert**: Config is rebuilt, DB `config_commit` is updated to shadow repo
   HEAD. The old command `agenc mission update-config <short-id>` still works.

### No automatic restart on update

**As a** mission operator, **I want** config update to NOT auto-restart running
Claude sessions, **so that** I have control over when the restart happens.

**Test Steps:**

1. **Setup**: Start a mission (so it's RUNNING). Modify `~/.claude`.
2. **Action**: Run `agenc mission update <short-id>`
3. **Assert**: Config is rebuilt, but the running Claude session is NOT
   restarted. A message tells the user to manually restart.

Implementation Plan
-------------------

### Phase 1: Rename command

- [ ] `cmd/mission_update_config.go`: Change `Use` from `updateConfigCmdStr` to
  `updateCmdStr`, add `Aliases: []string{updateConfigCmdStr}` to keep backward
  compatibility
- [ ] `cmd/command_str_consts.go`: No changes needed — `updateCmdStr` already
  exists

### Phase 2: Remove auto-restart signaling

- [ ] `cmd/mission_update_config.go`: In `updateMissionConfig()`, remove the
  block that checks if the mission is running and sends a restart signal via the
  wrapper socket (lines 200-217). Replace with a message: "Note: restart the
  mission to pick up config changes"
- [ ] Remove the `wrapper` import if no longer used in this file
- [ ] Remove the `errors` import if no longer used

### Phase 3: Add CONFIG column to `mission ls -a`

- [ ] `cmd/ansi_colors.go`: Add `ansiRed = "\033[31m"` constant
- [ ] `internal/claudeconfig/build.go`: Add a new exported function
  `CountCommitsBehind(agencDirpath string, missionCommitHash string) (int, error)`
  that runs `git rev-list --count <missionCommit>..<HEAD>` in the shadow repo
  and returns the count. Return 0 if the mission commit IS the HEAD. Return
  error if the commit is not found.
- [ ] `cmd/mission_ls.go`: In the `-a` branch, add a "CONFIG" column header
  after PANE. For each mission, compute the display string:
  - If `m.ConfigCommit` is nil: show `"--"`
  - Otherwise: show `shortHash(*m.ConfigCommit)` and if behind > 0, append
    ` (-N)` wrapped in `ansiRed`/`ansiReset`
- [ ] `cmd/mission_ls.go`: Compute the current shadow repo HEAD once at the top
  of `runMissionLs` (only when `lsAllFlag` is true) to avoid calling
  `GetShadowRepoCommitHash` per mission. Pass it to `CountCommitsBehind`.

### Phase 4: Build and verify

- [ ] Run `make build` to verify compilation
- [ ] Manual test: `./agenc mission ls -a` shows CONFIG column

Technical Details
-----------------

- **Files to modify**: `cmd/mission_update_config.go`, `cmd/mission_ls.go`,
  `cmd/ansi_colors.go`, `internal/claudeconfig/build.go`
- **No new files needed**
- **New function**: `CountCommitsBehind(agencDirpath, commitHash)` in
  `internal/claudeconfig/build.go`
- **New constant**: `ansiRed` in `cmd/ansi_colors.go`
- **Git operation**: `git rev-list --count <old>..<HEAD>` in shadow repo dir

Testing Strategy
----------------

- **Unit tests**: `CountCommitsBehind` can be tested with a temporary git repo
  with known commits
- **Manual tests**: Verify `mission ls -a` output, verify `mission update` and
  `mission update-config` both work

Acceptance Criteria
-------------------

- [ ] `agenc mission update` works identically to the old `update-config`
- [ ] `agenc mission update-config` still works (alias)
- [ ] `agenc mission ls -a` shows a CONFIG column with 12-char commit hashes
- [ ] Missions behind HEAD show `(-N)` in red
- [ ] Missions at HEAD show no suffix
- [ ] Missions without a config commit show `--`
- [ ] Running `mission update` does NOT auto-restart Claude sessions
- [ ] A helpful message tells the user to restart manually

Risks & Considerations
----------------------

- **Performance**: `git rev-list --count` per mission could be slow with many
  missions. Mitigated: the shadow repo is tiny (few files, few commits), so
  rev-list is effectively instant. We also compute HEAD once and reuse it.
- **Missing commits**: If a mission's pinned commit no longer exists in the
  shadow repo (e.g., after a force-push or repo recreation), `rev-list` will
  fail. Handle gracefully with `"??"` display.
- **Breaking scripts**: The rename could break users who scripted
  `update-config`. Mitigated by keeping `update-config` as an alias.
