Mission "Last Active" Tracking
===============================

**Created**: 2026-02-12  **Type**: feature  **Status**: Open

---

Description
-----------

Track when a user last interacted with each mission's Claude session, so the mission switcher and `mission ls` can sort by true recency of use rather than wrapper liveness. Currently, `last_heartbeat` updates every 30 seconds while the wrapper runs — it tells you the wrapper is alive, not that the user is engaged. Two missions both running but one untouched for days will have nearly identical heartbeats.

A new `last_active` database column will record when the user last submitted a prompt (via the existing `UserPromptSubmit` hook). This provides a stable, meaningful "last touched" timestamp that persists after the wrapper stops and accurately reflects user engagement.


Context
-------

**User request:** Sort missions in the switcher by when they were last used. Ideally capture both prompt submissions and permission acceptance, though there is no Claude Code hook for "permission accepted."

**Design exploration:** Five approaches were evaluated:

1. **New `last_active` column, UserPromptSubmit only** — Score 8/10. Selected.
2. **New `last_active` column, UserPromptSubmit + Notification hook** — Score 7/10. Rejected: permission prompt *appearing* is not the same as the user *accepting* it; the semantic mismatch would produce misleading timestamps.
3. **Repurpose `last_heartbeat`** — Score 4/10. Rejected: breaks daemon repo sync and orphan detection.
4. **Heuristic Stop-timing inference** — Score 5/10. Rejected: brittle magic numbers, false positives.
5. **Tmux focus polling** — Score 3/10. Rejected: tmux-specific, adds polling overhead, focus ≠ interaction.


Design Decision
---------------

**Option A: New `last_active` column, updated on UserPromptSubmit** (Quality score: 8/10)

Rationale:
- Clean separation of concerns: `last_heartbeat` = wrapper liveness, `last_active` = user engagement
- Minimal changes — one new column, one new DB method, a few lines in the wrapper's existing `handleClaudeUpdate`
- Uses the existing `UserPromptSubmit` hook (no new hooks needed)
- Sorting and display infrastructure already exists and just needs to point at the new column
- The `mission send claude-update` CLI command already routes events to the wrapper — no new socket protocol needed

What this does **not** capture:
- Permission acceptance (no hook exists)
- User reading Claude's output without submitting a new prompt

These gaps are acceptable. The primary signal — "user submitted a prompt" — is the strongest indicator of active engagement. If Claude Code adds a hook for permission acceptance in the future, it can be trivially added as another trigger for `last_active`.


User Stories
------------

### Mission switcher sorted by recency

**As a** user with many missions, **I want** the mission switcher to show recently-used missions first, **so that** I can quickly switch to the mission I was just working on.

**Test Steps:**

1. **Setup**: Create 3 missions (A, B, C). Submit prompts to A, then B, then C (in that order, with brief pauses between).
2. **Action**: Run `agenc mission ls`.
3. **Assert**: Missions appear in order C, B, A (most recently prompted first). The "LAST ACTIVE" column shows timestamps reflecting when each mission's last prompt was submitted, not when the wrapper last sent a heartbeat.

### Stopped missions retain their last-active timestamp

**As a** user who stops and later resumes missions, **I want** stopped missions to retain their last-active timestamp, **so that** I can see when I last used them even if the wrapper is no longer running.

**Test Steps:**

1. **Setup**: Create mission A, submit a prompt, note the time. Stop mission A. Wait 2 minutes.
2. **Action**: Run `agenc mission ls`.
3. **Assert**: Mission A shows "STOPPED" status but the "LAST ACTIVE" column reflects when the prompt was submitted, not "—" or the wrapper's last heartbeat.

### New missions without interaction show fallback

**As a** user who just created a mission but hasn't submitted a prompt yet, **I want** the mission to still appear in the list with a reasonable sort position.

**Test Steps:**

1. **Setup**: Create a new mission. Do not submit any prompts.
2. **Action**: Run `agenc mission ls`.
3. **Assert**: The mission appears in the list. The "LAST ACTIVE" column falls back to the `last_heartbeat` value (or "—" if no heartbeat either). Sorting places it after missions with a `last_active` timestamp.


Implementation Plan
-------------------

### Phase 1: Database schema and CRUD

- [ ] Add `addLastActiveColumnSQL` constant in `internal/database/database.go`
- [ ] Add `LastActive *time.Time` field to the `Mission` struct
- [ ] Add `migrateAddLastActive` function (idempotent, follows existing pattern)
- [ ] Call `migrateAddLastActive` from `Open()`
- [ ] Add `UpdateLastActive(id string) error` method to `DB`
- [ ] Update `scanMissions` and `scanMission` to include `last_active` in SELECT and scan it into `Mission.LastActive`
- [ ] Update all SELECT queries that enumerate mission columns (in `ListMissions`, `GetMission`, `GetMostRecentMissionForCron`, `GetMissionByTmuxPane`) to include `last_active`
- [ ] Update `ListMissions` ORDER BY to: `last_active IS NULL, last_active DESC, last_heartbeat IS NULL, last_heartbeat DESC, created_at DESC`

### Phase 2: Wrapper integration

- [ ] In `handleClaudeUpdate` in `internal/wrapper/wrapper.go`, add a `db.UpdateLastActive(w.missionID)` call when `Event == "UserPromptSubmit"`

### Phase 3: Display layer

- [ ] Update `formatLastActive` in `cmd/mission_ls.go` to accept the `Mission` struct (or both `LastActive` and `LastHeartbeat`) and prefer `LastActive` when non-nil, falling back to `LastHeartbeat`
- [ ] Update all call sites of `formatLastActive`: `runMissionLs` and `buildMissionPickerEntries`

### Phase 4: Architecture documentation

- [ ] Update `docs/system-architecture.md` Database Schema table: add `last_active` row
- [ ] Update the "Heartbeat system" section to mention that `last_active` tracks user engagement separately from wrapper liveness
- [ ] Update the "Idle detection via socket" section to note that `UserPromptSubmit` also updates `last_active`


Technical Details
-----------------

**Modules to modify:**

| File | Changes |
|------|---------|
| `internal/database/database.go` | New column SQL, migration, `UpdateLastActive` method, `Mission` struct field, updated scans and queries |
| `internal/wrapper/wrapper.go` | One line in `handleClaudeUpdate` for `UserPromptSubmit` case |
| `cmd/mission_ls.go` | Updated `formatLastActive` to prefer `LastActive` over `LastHeartbeat` |
| `cmd/mission_helpers.go` | Updated `buildMissionPickerEntries` to pass new field to `formatLastActive` |
| `docs/system-architecture.md` | Schema table, heartbeat section, idle detection section |

**Key changes:**

- New database column: `last_active TEXT` (nullable, RFC3339 format) — same storage pattern as `last_heartbeat`
- New DB method: `UpdateLastActive(id string) error` — mirrors `UpdateHeartbeat` but for user interaction
- Sorting change: `ORDER BY last_active IS NULL, last_active DESC, last_heartbeat IS NULL, last_heartbeat DESC, created_at DESC` — missions with recent user activity sort first, then missions with recent wrapper activity, then by creation date
- Display: `formatLastActive` becomes a fallback chain: `LastActive` → `LastHeartbeat` → `"--"`

**No new hooks needed.** The existing `UserPromptSubmit` hook already reaches the wrapper via the unix socket. We just add one DB write in the handler.

**No backfill needed.** Existing missions will have `last_active = NULL`. They'll sort by `last_heartbeat` (then `created_at`), which is the current behavior. The first time a user submits a prompt after the upgrade, `last_active` gets populated.


Testing Strategy
----------------

- **Unit tests**: Test `migrateAddLastActive` idempotency (run twice, no error). Test `UpdateLastActive` writes the correct timestamp. Test `ListMissions` sorting with various combinations of `last_active` and `last_heartbeat` (both nil, one nil, both present).
- **Integration tests**: Verify the full flow: `UserPromptSubmit` event → wrapper calls `UpdateLastActive` → `ListMissions` returns missions in correct order.
- **Manual validation**: Create multiple missions, interact with them in different orders, verify `agenc mission ls` sorts by last interaction and the tmux switcher follows the same order.


Acceptance Criteria
-------------------

- [ ] `agenc mission ls` sorts missions by most recent user interaction first
- [ ] The "LAST ACTIVE" column shows when the user last submitted a prompt, not when the wrapper last sent a heartbeat
- [ ] Stopped missions retain their last-active timestamp
- [ ] New missions with no prompt submissions fall back to heartbeat (then creation date) for sorting
- [ ] The mission switcher (fzf picker) uses the same sort order
- [ ] `docs/system-architecture.md` is updated to document the new column and its semantics
- [ ] No changes to `last_heartbeat` behavior — daemon repo sync continues to work as before


Risks & Considerations
----------------------

- **Permission acceptance not captured.** Claude Code does not expose a hook for when the user accepts a permission prompt. If this is added in the future, it would be a one-line addition to the `handleClaudeUpdate` switch to also update `last_active` on that event.
- **Very long Claude responses.** If the user submits a prompt and Claude runs for 30+ minutes without stopping, `last_active` still reflects the prompt submission time, which is correct — that's when the user last actively engaged.
- **Multiple rapid prompts.** Each `UserPromptSubmit` overwrites `last_active`. This is fine — we want the most recent interaction timestamp, not a history.
- **Database write frequency.** `UpdateLastActive` runs once per user prompt submission, which is far less frequent than the 30-second heartbeat. No performance concern.
