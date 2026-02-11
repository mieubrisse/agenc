Fix Mission Clone to Fork Conversation History
===============================================

**Created**: 2026-02-11
**Type**: Bug Fix + Feature Enhancement
**Status**: Open
**Priority**: High
**Related**: `specs/captured-claude-sessions.md`, `cmd/mission_new.go`


Description
-----------

`agenc mission new --clone <id>` creates a new mission from an existing one but does not clone the conversation history. The cloned mission starts with a blank Claude session, losing all context from the source mission. This makes clone effectively useless for its intended purpose: forking an in-progress agent to explore an alternative approach while preserving full conversational context.


Context
-------

### Current Behavior

The clone path in `cmd/mission_new.go:87-130` (`runMissionNewWithClone`):
1. Creates a new DB record with a fresh UUID
2. Uses the **current** shadow repo HEAD as `config_commit` (not the source's)
3. Copies the agent directory via rsync (`mission.CopyAgentDir`)
4. Launches wrapper with `w.Run(false)` (new session, no conversation)

### Problems

1. **No idle wait** - Copies agent dir while source mission may be actively writing files, risking an inconsistent snapshot
2. **No session forking** - Conversation history is not copied; cloned mission starts fresh
3. **Wrong config_commit** - Uses current shadow HEAD instead of source mission's config, which means the clone may get different Claude config than the source
4. **No resume** - Launches as a new session instead of resuming with the forked conversation

### How Claude Code Sessions Work

Sessions are stored in `~/.claude/projects/<encoded-agent-path>/`:
- `<session-uuid>.jsonl` - Full conversation transcript (append-only, one JSON line per event)
- `<session-uuid>/` - Subdirectory containing `subagents/` and `tool-results/`
- `sessions-index.json` - Optional index of sessions with summaries

The project directory name is derived from the agent directory path (slashes become dashes). Each mission gets a unique project directory because each mission has a unique UUID in its agent path.

JSONL lines contain a `sessionId` field matching the session UUID. When forking, all `sessionId` values in the copied JSONL must be replaced with the new session UUID.

Claude Code supports forking natively via:
- `claude -r <session-uuid> --fork-session` - Resumes full history under a new session ID

However, since the cloned mission has a different project directory path, we must **copy** the session files into the new project directory rather than using `--fork-session` in-place.


Design Decision
---------------

**Selected: Option B - Wait-for-Idle then Copy** (Quality score: 8.5/10)

The clone command:
1. Waits for the source mission to reach an idle state (Claude finished processing)
2. Copies agent directory + session files with new UUIDs
3. Launches the cloned mission with `claude -r <new-session-uuid>` to resume from the forked conversation

**Why this option**: Non-disruptive to the source mission. Uses the idle detection already built into the wrapper's socket protocol. The copy happens in a brief window where the source is idle, minimizing risk of inconsistent state.

**Config commit**: Uses the source mission's `config_commit` from the DB, not the current shadow HEAD. This ensures the clone inherits the same Claude config snapshot as the source.


Implementation Plan
-------------------

### Phase 1: Add Idle Query to Wrapper Socket Protocol

- [ ] Add `query_idle` command handler in `internal/wrapper/wrapper.go` (in `handleCommand` switch)
  - Returns `Response` with an additional `Idle bool` field (or encode in a JSON payload)
  - Reads `w.claudeIdle` and returns its current value
- [ ] Extend `Response` struct in `internal/wrapper/socket.go` to include an `Idle` field (or add a dedicated `IdleResponse` type)
- [ ] Add `QueryIdle(socketFilepath string) (bool, error)` helper in `internal/wrapper/client.go` that sends `query_idle` command and returns the idle boolean

### Phase 2: Add Session Copy Utility

- [ ] Create `internal/session/copy.go` with function `CopyAndForkSession`:
  ```
  func CopyAndForkSession(
      srcProjectDirpath string,    // source project dir in ~/.claude/projects/
      dstProjectDirpath string,    // destination project dir
      srcSessionID string,         // source session UUID
      newSessionID string,         // replacement session UUID
  ) error
  ```
  This function:
  - Copies `<srcSessionID>.jsonl` to `<newSessionID>.jsonl` in `dstProjectDirpath`
  - Replaces all `"sessionId":"<srcSessionID>"` occurrences with `"sessionId":"<newSessionID>"` in the copied JSONL (line-by-line to handle large files)
  - Copies the `<srcSessionID>/` subdirectory (subagents, tool-results) as `<newSessionID>/` in `dstProjectDirpath`
  - Copies `sessions-index.json` if it exists, updating the session ID within it
  - Copies `memory/` directory if it exists (auto-memory persists per project)
- [ ] Create `internal/session/find.go` with function `FindLatestSessionID`:
  ```
  func FindLatestSessionID(projectDirpath string) (string, error)
  ```
  Scans the project directory for `.jsonl` files and returns the session UUID from the most recently modified one (extracted from filename by stripping `.jsonl`).
- [ ] Create `internal/session/project.go` with function `FindProjectDirpath`:
  ```
  func FindProjectDirpath(missionID string) (string, error)
  ```
  Scans `~/.claude/projects/` for a directory whose name contains the mission UUID. Returns the full path. This extracts the existing logic from `FindSessionName`.

### Phase 3: Update `BuildMissionConfigDir` for Config Commit

- [ ] Add a `BuildMissionConfigDirFromCommit(agencDirpath, missionID, configCommit string) error` variant in `internal/claudeconfig/build.go` (or add a `configCommit` parameter to the existing function)
  - When `configCommit` is non-empty, temporarily checks out that commit in the shadow repo before copying files, then restores HEAD
  - When empty, uses current HEAD (existing behavior)
  - **Alternative (simpler)**: Since the shadow repo is auto-committed and rarely diverges significantly, and the source agent directory already contains the project-level `.claude/` config, this may not be strictly necessary for the initial implementation. The agent directory copy already captures the source's CLAUDE.md and settings. The per-mission `claude-config/` is rebuilt from shadow regardless. **Decision**: For v1, use the source mission's `config_commit` in the DB record but still build `claude-config/` from current shadow HEAD. Add commit-based checkout in a follow-up if users report config drift issues.

### Phase 4: Update `runMissionNewWithClone` in `cmd/mission_new.go`

- [ ] **Wait for idle**: Before copying, check if the source mission's wrapper is running. If running:
  - Print "Waiting for mission <short-id> to reach idle state..."
  - Poll `QueryIdle(socketFilepath)` every 500ms until it returns `true`
  - Print "Mission is idle, cloning..." once idle
  - If wrapper is not running (stopped mission), skip the idle wait
- [ ] **Use source config_commit**: Replace `claudeconfig.GetShadowRepoCommitHash(agencDirpath)` with `sourceMission.ConfigCommit` when creating the DB record
- [ ] **Copy session files**: After copying the agent directory:
  - Call `session.FindProjectDirpath(sourceMission.ID)` to locate source project dir
  - Compute destination project dir name by encoding the new mission's agent path
  - Create the destination project directory under `~/.claude/projects/`
  - Call `session.FindLatestSessionID(srcProjectDirpath)` to get the source session UUID
  - Generate a new session UUID (`uuid.New().String()`)
  - Call `session.CopyAndForkSession(srcProjectDir, dstProjectDir, srcSessionID, newSessionID)`
  - If source has no session (FindLatestSessionID returns error), skip session copy gracefully
- [ ] **Launch with resume**: Change `w.Run(false)` to `w.Run(true)` so the wrapper starts Claude with `claude -c` (continue), which will pick up the forked session as the latest in the project directory
  - **Alternative**: Use `claude -r <newSessionID>` for explicit session targeting. This requires adding a `SpawnClaudeResumeSession(sessionID)` variant in `internal/mission/mission.go`. **Decision**: Use `claude -c` for v1 — since the cloned mission's project directory has only one session (the copied one), `-c` will naturally find it.

### Phase 5: Project Directory Encoding

- [ ] Add `internal/session/encoding.go` with function `EncodeProjectDirname(agentDirpath string) string` that replicates Claude Code's path-to-dirname encoding:
  - Replace both `/` and `.` with `-`
  - Example: `/Users/odyssey/.agenc/missions/<uuid>/agent` → `-Users-odyssey--agenc-missions-<uuid>-agent`
  - Verify by comparing against actual directories in `~/.claude/projects/`
  - Note: Claude Code may use the `realpath` of the agent directory. Test with symlinks to confirm.


Technical Details
-----------------

- **Modules to create**: `internal/session/copy.go`, `internal/session/find.go`, `internal/session/project.go`, `internal/session/encoding.go`
- **Modules to modify**: `cmd/mission_new.go`, `internal/wrapper/wrapper.go`, `internal/wrapper/socket.go`, `internal/wrapper/client.go`
- **Data models affected**: `Response` struct gains `Idle` field
- **Database changes**: None (existing `config_commit` field is already sufficient)
- **Key dependencies**: `github.com/google/uuid` for generating new session UUIDs (check if already in go.mod)


Testing Strategy
----------------

- **Unit tests**:
  - `CopyAndForkSession`: Verify JSONL sessionId replacement, file copy, subdirectory copy
  - `FindLatestSessionID`: Verify most-recent-by-modtime selection
  - `EncodeProjectDirname`: Verify path encoding matches actual Claude Code behavior
  - `QueryIdle`: Verify socket command round-trip
- **Integration tests**:
  - Clone a running mission → verify idle wait behavior
  - Clone a stopped mission → verify skip-idle-wait behavior
  - Clone a mission with no session → verify graceful skip
- **Manual E2E test**:
  1. Start a mission, have a conversation with some turns
  2. Run `agenc mission new --clone <id>`
  3. Verify the cloned mission starts with the full conversation history visible
  4. Verify the source mission is unaffected
  5. Verify both missions can continue independently


Acceptance Criteria
-------------------

- [ ] `agenc mission new --clone <id>` waits for the source mission to be idle (if running) before copying, with a visible progress message
- [ ] The cloned mission starts with the full conversation history from the source mission's latest session
- [ ] The cloned mission has a new session UUID (not sharing the source's session)
- [ ] All `sessionId` fields in the cloned JSONL point to the new session UUID
- [ ] The source mission is completely unaffected (its session, agent dir, and DB record are unchanged)
- [ ] The cloned mission's DB record uses the source mission's `config_commit`
- [ ] Cloning a stopped mission (no running wrapper) works without errors
- [ ] Cloning a mission with no conversation history works (starts with empty session)


Risks & Considerations
----------------------

- **Race condition window**: Between the idle check and the copy, the source could become non-idle (user submits a prompt). The copy window is short (rsync is fast), so this is a minor risk. The JSONL is append-only, so a partial final line is the worst case — and Claude Code handles truncated JSONL gracefully.
- **Project directory encoding**: Claude Code's exact encoding algorithm is not documented. We reverse-engineer it from observed directory names. If Claude Code changes its encoding, session copy would break. Mitigation: the `FindProjectDirpath` function uses substring matching on mission UUID, which is robust.
- **Large sessions**: Very large JSONL files (100MB+) will take time to copy and rewrite. For v1 this is acceptable. If it becomes a bottleneck, a streaming copy with on-the-fly replacement could be used.
- **Subagent sessions**: The `<session-uuid>/subagents/` directory may contain its own JSONL files with session references. For v1, copy these as-is without UUID replacement. Claude Code should handle them correctly since the main session JSONL points to the new UUID.


Related Documentation
---------------------

- `specs/captured-claude-sessions.md` — broader vision for conversation forking and per-turn state capture
- `docs/system-architecture.md` — overall system architecture
- `internal/session/session.go` — existing session name resolution logic
