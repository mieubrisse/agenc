Remaining Codebase Cleanup Work
================================

**Status as of:** 2026-02-14
**Progress:** 15/28 beads complete (53.6%)
**Epic:** agenc-351

Overview
--------

The technical debt cleanup initiative has completed its foundation phase with all quick wins finished. This document details the remaining 13 beads organized by effort, priority, and dependencies.

Current State
-------------

### Completed Work (15 beads)

**Testing Infrastructure:**
- ✅ agenc-324: Fixed failing tests in agenc_config_test.go
- ✅ agenc-333: Wrapper integration tests (9 tests, 49 subtests)
- ✅ agenc-334: Cron scheduler tests (9 test functions)
- ✅ agenc-335: Session name resolution tests (11 test functions)
- ✅ agenc-345: Fixed race condition in cron scheduler

**Code Quality & CI:**
- ✅ agenc-323: Ran gofmt on entire codebase
- ✅ agenc-327: Added build-time checks (gofmt + go vet in Makefile)
- ✅ agenc-328: Linter configuration + CI pipeline (golangci-lint, race detector, coverage)
- ✅ agenc-332: Documented baseline metrics

**Database & Performance:**
- ✅ agenc-329: Added 3 database indices for query performance
- ✅ agenc-348: Reduced heartbeat frequency from 30s to 60s (50% write reduction)

**Code Cleanup:**
- ✅ agenc-326: Removed ~400 lines of deprecated Keychain code

**Duplicates Closed:**
- ✅ agenc-330, 331: Already covered by agenc-328 CI work
- ✅ agenc-341: Duplicate of agenc-333

### Foundation Established

The completed work provides:
- Comprehensive test coverage for critical components
- Automated quality gates (formatting, static analysis, race detection)
- Performance optimizations (indices, reduced write frequency)
- Clean, maintainable code (dead code removed, formatted)
- Baseline metrics for tracking improvement

Remaining Work
--------------

### Quick/Small Tasks (1 bead, 1-2 hours)

**agenc-325** [P2] - Add constants for magic numbers
- **Effort:** 1-2 hours
- **Files:** internal/wrapper/wrapper.go, internal/daemon/mission_summarizer.go
- **Changes:**
  - Replace heartbeatInterval = 30s with named constant
  - Replace gitWatchDebounce = 5s with named constant
  - Replace summarizerInterval = 2min with named constant
  - Replace summarizerPromptThreshold = 10 with named constant
  - Replace summarizerMaxMessages = 15 with named constant
- **Impact:** Improved code maintainability, easier to adjust values
- **Dependencies:** None
- **Reference:** P3-3 in specs/codebase-cleanup.md

---

### Medium Tasks (6 beads, 25-40 hours)

**agenc-336** [P0] - Add context and timeout to git operations ⭐ READY
- **Effort:** 4-6 hours
- **Priority:** P0
- **Files:**
  - internal/mission/repo.go: All git operations (fetch, reset, symbolic-ref, rev-parse)
  - internal/claudeconfig/shadow.go: Git clone/fetch operations
  - internal/daemon/config_auto_commit.go: Git operations
- **Changes:**
  - Replace 67 exec.Command calls with exec.CommandContext
  - Define const gitOperationTimeout = 30 * time.Second
  - Prevents indefinite hangs on network failures
- **Impact:** Production stability improvement
- **Dependencies:** agenc-328 (complete)
- **Blocks:** agenc-337
- **Reference:** P0-7 in specs/codebase-cleanup.md

**agenc-338** [P1] - Enforce restrictive file permissions ⭐ READY
- **Effort:** 2-3 hours
- **Priority:** P1
- **Files:**
  - internal/config/config.go: WriteOAuthToken should use mode 0600
  - internal/wrapper/socket.go: Set socket permissions after creation
- **Changes:**
  - OAuth tokens: mode 0600 (read/write owner only)
  - Wrapper sockets: mode 0600
  - Add permission check to 'agenc doctor' command
- **Impact:** Security hardening - prevents OAuth token leakage
- **Dependencies:** None
- **Reference:** P1-10 in specs/codebase-cleanup.md

**agenc-339** [P1] - Add comprehensive configuration validation ⭐ READY
- **Effort:** 3-4 hours
- **Priority:** P1
- **File:** internal/config/agenc_config.go
- **Changes:**
  - Git repo URLs: Full URL parsing, not just regex
  - Path inputs: Validate no '../' traversal patterns
  - Timeout durations: Parse and validate (positive, reasonable max)
  - Numeric bounds: Max concurrent >= 1, priorities 0-4
  - User prompts: Sanitize control characters
- **Impact:** Prevents cryptic runtime failures
- **Dependencies:** None
- **Reference:** P1-9 in specs/codebase-cleanup.md

**agenc-346** [P1] - Standardize database API error semantics ⭐ READY
- **Effort:** 4-6 hours
- **Priority:** P1
- **File:** internal/database/database.go
- **Current inconsistency:**
  - GetMission(id) returns (nil, error) when not found
  - GetMissionByTmuxPane(paneID) returns (nil, nil) when not found
  - GetMostRecentMissionForCron(cronID) returns error when not found
- **Solution:** Standardize to (nil, nil) for not-found, error only for actual failures
- **Alternative:** Return (*Mission, bool, error) where bool indicates found
- **Impact:** API consistency, easier to use correctly
- **Dependencies:** None
- **Reference:** P1-8 in specs/codebase-cleanup.md

**agenc-347** [P1] - Implement configuration caching ⭐ READY
- **Effort:** 4-6 hours
- **Priority:** P1
- **File:** internal/claudeconfig/merge.go
- **Current:** Heavy JSON marshaling on every mission creation (87 occurrences)
- **Solution:**
  - Cache merged configs keyed by shadow repo commit hash
  - Invalidate cache when shadow repo updates
  - Store in memory: map[string]interface{}
- **Impact:** Reduces CPU overhead on mission creation
- **Dependencies:** None
- **Reference:** P1-5 in specs/codebase-cleanup.md

**agenc-337** [P1] - Add context parameter to database methods ⚠️ BLOCKED
- **Effort:** 8-12 hours
- **Priority:** P1
- **File:** internal/database/database.go
- **Changes:**
  - Add ctx context.Context as first parameter to all database methods
  - Update all callers across cmd/ and internal/ packages
  - Use db.ExecContext and db.QueryContext instead of Exec/Query
- **Impact:** Enables cancellation of long-running queries (breaking API change)
- **Dependencies:** agenc-340, agenc-336
- **Blocks:** agenc-350
- **Reference:** P0-7 in specs/codebase-cleanup.md

---

### Large Refactors (6 beads, 2-3 months)

**agenc-340** [P0] - Split database.go into focused files ⭐ READY
- **Effort:** 2-3 weeks
- **Priority:** P0
- **Current:** 871 lines in single file
- **Target:** 4 focused files, database.go <200 lines
- **Strategy:** 4-phase approach
  - Phase 1 (Week 1): Create new files without deleting originals
    - internal/database/missions.go: Mission CRUD operations
    - internal/database/migrations.go: All schema migration functions
    - internal/database/queries.go: Complex query builders
    - internal/database/scanners.go: Row scanning helpers
  - Phase 2 (Week 2): Update imports, verify tests pass
  - Phase 3 (Week 3): Delete duplicates from database.go
  - Phase 4 (Week 4): Add missing tests for extracted code
- **Impact:** Major maintainability improvement
- **Risk:** Medium - central component
- **Dependencies:** agenc-324 (complete), agenc-328 (complete)
- **Blocks:** agenc-337, agenc-350
- **Reference:** P0-1 in specs/codebase-cleanup.md

**agenc-342** [P0] - Refactor wrapper.go phases 2-5 ⭐ READY
- **Effort:** 4 weeks
- **Priority:** P0
- **Current:** 771 lines in single file
- **Target:** 4 focused files, wrapper.go <300 lines
- **Strategy:** 4-phase approach (Phase 1 complete via agenc-333)
  - Phase 2 (Week 8): Extract state machine → internal/wrapper/state.go
  - Phase 3 (Week 9): Extract heartbeat → internal/wrapper/heartbeat.go
  - Phase 4 (Week 10): Extract git watching → internal/wrapper/git_watcher.go
  - Phase 5 (Week 11): Cleanup, document state transitions
- **Impact:** Major maintainability improvement
- **Risk:** HIGH - core runtime component, verify at each step
- **Dependencies:** agenc-333 (complete)
- **Rollback:** Each phase independently revertible
- **Reference:** P0-2 in specs/codebase-cleanup.md

**agenc-343** [P1] - Split large configuration files ⭐ READY
- **Effort:** 1 week
- **Priority:** P1
- **Current:**
  - agenc_config.go: 741 lines
  - config.go: 493 lines
- **Target:** No file >400 lines
- **Strategy:**
  - Split agenc_config.go into:
    - agenc_config.go: Core struct definitions and getters
    - config_validation.go: All validation functions
    - palette_commands.go: Palette command resolution
    - cron_config.go: Cron configuration logic
  - Split config.go into:
    - config.go: Path helpers only
    - oauth_setup.go: OAuth token interactive setup
    - dir_structure.go: Directory creation and seeding
- **Impact:** Improved code organization
- **Dependencies:** None
- **Reference:** P1-1 in specs/codebase-cleanup.md

**agenc-344** [P2] - Organize cmd/ directory into subdirectories ⭐ READY
- **Effort:** 1 week
- **Priority:** P2
- **Current:** 78 command files in flat cmd/ directory
- **Target:** ~40 files in logical subdirectories
- **Strategy:**
  - Create subdirectories:
    - cmd/mission/: All mission-related commands
    - cmd/config/: Configuration commands
    - cmd/cron/: Cron management commands
    - cmd/tmux/: Tmux integration commands
    - cmd/daemon/: Daemon management commands
  - Update import paths atomically
- **Impact:** Improved navigation (deprioritized from P0 - not critical)
- **Dependencies:** None
- **Reference:** P2 (formerly P0-3) in specs/codebase-cleanup.md

**agenc-349** [P2] - Optimize JSONL scanner buffer allocation ⭐ READY
- **Effort:** 2-3 hours
- **Priority:** P2
- **File:** internal/session/conversation.go:50
- **Current:** 1MB buffer allocated per JSONL read, no reuse
- **Solution:**
  ```go
  var scannerBufPool = sync.Pool{
      New: func() interface{} {
          return make([]byte, 1024*1024)
      },
  }
  ```
  - Use pool.Get() before scanning
  - Use pool.Put() after scanning
- **Impact:** Reduced allocations, better memory efficiency
- **Dependencies:** None
- **Reference:** P2-4 in specs/codebase-cleanup.md

**agenc-350** [P1] - Define MissionStore interface ⚠️ BLOCKED
- **Effort:** 6-8 hours
- **Priority:** P1
- **File:** internal/database/interface.go (new)
- **Purpose:** Enable mocking and testability
- **Solution:**
  ```go
  type MissionStore interface {
      CreateMission(ctx context.Context, params *CreateMissionParams) (*Mission, error)
      GetMission(ctx context.Context, id string) (*Mission, error)
      ListMissions(ctx context.Context, params ListMissionsParams) ([]*Mission, error)
      ArchiveMission(ctx context.Context, id string) error
      DeleteMission(ctx context.Context, id string) error
      UpdateHeartbeat(ctx context.Context, id string) error
      // ... other CRUD operations
  }
  ```
- **Impact:** Better testability, dependency injection
- **Dependencies:** agenc-340, agenc-337
- **When:** After concrete types stabilized (don't premature abstract)
- **Reference:** P1-4 in specs/codebase-cleanup.md

Critical Path
-------------

To maximize unblocking and minimize dependencies:

```
1. agenc-336 (git context, 4-6 hrs)
   └─> Unblocks agenc-337

2. agenc-340 (database split, 2-3 weeks)
   └─> Unblocks agenc-337, agenc-350

3. agenc-337 (database context, 8-12 hrs)
   └─> Unblocks agenc-350

4. agenc-350 (interface, 6-8 hrs)
   └─> Cleanup complete
```

**Parallel work** (can be done anytime):
- agenc-325, 338, 339, 346, 347 (medium tasks)
- agenc-342, 343, 344, 349 (other refactors)

Effort Summary
--------------

### By Duration

**Immediate (< 8 hours):** 7 beads ≈ 25-40 hours
- agenc-325: 1-2 hours
- agenc-336: 4-6 hours
- agenc-338: 2-3 hours
- agenc-339: 3-4 hours
- agenc-346: 4-6 hours
- agenc-347: 4-6 hours
- agenc-349: 2-3 hours

**Medium (8-12 hours):** 1 bead
- agenc-337: 8-12 hours

**Long (1-4 weeks):** 5 beads ≈ 2-3 months
- agenc-340: 2-3 weeks
- agenc-342: 4 weeks
- agenc-343: 1 week
- agenc-344: 1 week
- agenc-350: 6-8 hours

### By Priority

**P0 (Critical):** 3 beads
- agenc-336: Add git context/timeout
- agenc-340: Split database.go
- agenc-342: Refactor wrapper.go

**P1 (Important):** 6 beads
- agenc-337: Database context params
- agenc-338: File permissions
- agenc-339: Config validation
- agenc-343: Split config files
- agenc-346: Error semantics
- agenc-347: Config caching
- agenc-350: MissionStore interface

**P2 (Nice to have):** 4 beads
- agenc-325: Magic number constants
- agenc-344: Organize cmd/
- agenc-349: Buffer pooling

Strategic Options
-----------------

### Option A: Quick Completion (Recommended)
**Goal:** Get to 75% without multi-week commitment

**Approach:**
1. Complete 6 medium tasks (agenc-336, 338, 339, 346, 347, 337)
2. Complete small task (agenc-325)
3. Skip large refactors for now

**Result:** 22/28 beads (78.6%) in ~40-50 hours
- Strong, stable foundation
- No multi-week commitment
- Can revisit big refactors later

**Benefits:**
- ✅ Immediate value
- ✅ All critical issues addressed
- ✅ Minimal risk
- ✅ Foundation for future work

---

### Option B: Maximum Impact
**Goal:** Tackle the highest-impact blocker first

**Approach:**
1. agenc-340 (database split, 2-3 weeks)
2. Then medium tasks
3. Then remaining refactors

**Result:** Unblocks 2 critical beads, enables interface extraction
- Highest technical impact
- Enables better architecture (agenc-350)
- Long commitment

**Benefits:**
- ✅ Unblocks agenc-337, agenc-350
- ✅ Major maintainability win
- ✅ Enables better testing patterns
- ⚠️ 2-3 week commitment

---

### Option C: Parallel Progress
**Goal:** Mix quick wins with long work

**Approach:**
1. Start agenc-340 in background
2. Do medium tasks in parallel sessions
3. Complete both tracks

**Result:** Maximum progress if multiple sessions/agents available

**Benefits:**
- ✅ Progress on all fronts
- ✅ No wasted time
- ⚠️ Requires coordination

Metrics
-------

### Current Coverage (from baseline)
- **Overall:** ~38.1% test coverage
- **Strong:** tableprinter (100%), session (93.6%), history (92.6%)
- **Weak:** daemon (14.6%), mission (17.0%), wrapper (28.3%)
- **Zero:** main and cmd packages

### Complexity
- **gocyclo:** Not installed (tool needed for future baselines)
- **Functions >15 complexity:** Unknown (install gocyclo to measure)

### Race Conditions
- **Status:** No races detected after agenc-345 fix
- **CI:** Race detector runs on every PR

### Linter
- **Config:** Complete (.golangci.yml with 6 linters)
- **CI:** Runs on every PR via GitHub Actions
- **Local:** golangci-lint not installed locally

Dependencies Needing Updates
-----------------------------

From baseline metrics (agenc-332):

**Critical:**
- github.com/mieubrisse/stacktrace: v0.0.0-date → v0.1.0 (official release)
- github.com/stretchr/testify: v1.10.0 → v1.11.1

**Minor:**
- Multiple golang.org/x/* packages have updates available

Next Steps
----------

**Immediate:**
1. Review this document
2. Choose strategic option (A, B, or C)
3. Begin execution

**For Option A (Quick Completion):**
```bash
# Start with agenc-336 (git context, 4-6 hrs)
bd update agenc-336 --status=in_progress

# Then agenc-338, 339, 346, 347 in any order
# Finally agenc-337 (depends on 336, 340)
```

**For Option B (Maximum Impact):**
```bash
# Start with agenc-340 (database split, 2-3 weeks)
bd update agenc-340 --status=in_progress

# Follow 4-phase strategy from bead description
```

**For Option C (Parallel):**
```bash
# Multiple agents work on different beads simultaneously
# Coordinate via bd list --status=in_progress
```

References
----------

- **Master Epic:** agenc-351
- **Detailed Spec:** specs/codebase-cleanup.md
- **Architecture Doc:** docs/system-architecture.md
- **Baseline Metrics:** docs/metrics-baseline.md
- **Bead System:** .beads/issues.jsonl

---

**Document Version:** 1.0
**Last Updated:** 2026-02-14
**Author:** Technical Debt Cleanup Master Coordinator
