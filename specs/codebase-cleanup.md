Codebase Cleanup Report
=======================

Executive Summary
-----------------

The AgenC codebase demonstrates solid architectural design and strong security practices, but suffers from significant technical debt in code organization and testing. The primary issues are oversized god objects (`database.go` at 871 lines, `wrapper.go` at 771 lines), critical gaps in test coverage for core runtime components (wrapper, daemon, session packages have 0 tests), and extensive commented-out code from the deprecated Keychain authentication system. While the system-level architecture is well-documented and accurate, the file-level organization needs refactoring to improve maintainability. No critical security vulnerabilities were found, and dependency management is current and appropriate.

Critical Issues
----------------

**P0-1: God Object - database.go (871 lines)**
- File: `internal/database/database.go`
- Issue: Single file handles all database operations (CRUD, migrations, queries, scanning) with 36 exported functions
- Impact: High maintenance burden, difficult to test individual components, violates Single Responsibility Principle
- Recommendation: Split into migrations.go, missions.go, crons.go, and summary.go. Target: <300 lines per file

**P0-2: God Object - wrapper.go (771 lines)**
- File: `internal/wrapper/wrapper.go`
- Issue: Manages process lifecycle, state machine, heartbeats, git watching, signal handling, and headless execution in one file
- Impact: Difficult to reason about state transitions, high complexity, testing challenges
- Recommendation: Extract state machine, heartbeat logic, and git watching to separate files. Remove all commented-out Keychain auth code

**P0-3: Excessive Command File Count (78 files in cmd/)**
- Issue: Many trivial command files with duplicated patterns, no clear organization
- Impact: Difficult to navigate and maintain, slows onboarding
- Recommendation: Group into subdirectories (cmd/mission/, cmd/config/, cmd/cron/, cmd/tmux/, cmd/daemon/). Target: ~40 files

**P0-4: Zero Test Coverage for Critical Runtime Components**
- Missing tests: `internal/wrapper/` (0/6 files), `internal/session/` (0/2 files), `internal/daemon/` (1/8 files - only tests claudeconfig merge)
- Impact: CRITICAL - wrapper manages Claude child processes and state machine; daemon runs 6 concurrent loops
- Risk: State machine bugs, socket protocol failures, cron scheduling errors, mission lifecycle issues
- Recommendation: Integration tests for wrapper state machine and cron scheduler (highest priority)

**P0-5: Failing Tests in CI**
- File: `internal/config/agenc_config_test.go`
- Issue: Four tests currently failing (palette commands and keybinding tests)
- Impact: Undermines confidence in test suite, indicates regressions
- Recommendation: Fix immediately - failing tests should never be on main branch

**P0-6: Missing Go Tooling Infrastructure**
- Files: Need `.golangci.yml`, CI configuration updates
- Issue: No linter config, no race detector in CI, no coverage reporting, no pre-commit hooks
- Impact: CRITICAL - Technical debt will regress immediately after cleanup without enforcement
- Current state: Build succeeds but no automated quality gates
- Recommendation:
  - Create `.golangci.yml` with linters: errcheck, gosec, govet, staticcheck, unused, gocyclo (max 15)
  - Add CI: `go test -race -coverprofile=coverage.out ./...`
  - Add CI: `golangci-lint run`
  - Add pre-commit hook: gofmt check, go vet, basic tests
  - Target: 70%+ test coverage, 0 race conditions, 0 critical linter issues

**P0-7: Context and Timeout Management**
- Files: `internal/database/*.go`, `internal/mission/repo.go`, `cmd/*.go`, 67 total subprocess calls
- Issue: Only 1 of 67 `exec.Command` calls uses `exec.CommandContext`; database operations don't accept `context.Context`
- Impact: CRITICAL - Operations can hang indefinitely, no cancellation support, daemon loops could deadlock
- Examples:
  - All git operations in `internal/mission/repo.go` (fetch, reset, symbolic-ref) have no timeouts
  - Database methods like `ListMissions()` can't be cancelled
  - Subprocess calls in config_auto_commit.go, shadow.go lack timeout protection
- Recommendation:
  - Add `ctx context.Context` as first parameter to all database methods
  - Use `exec.CommandContext(ctx, ...)` for all subprocess calls
  - Define timeout constants: `gitOperationTimeout = 30s`, `dbOperationTimeout = 5s`
  - Priority: git operations first (network failures most common), then database

**P0-8: Database Performance - Missing Query Indices**
- File: `internal/database/database.go`
- Issue: Only one index (short_id) besides primary key; common queries do full table scans
- Impact: O(n) scaling with mission count, noticeable slowdown with 100+ missions
- Recommendation: Add indices for last_active/last_heartbeat (line 447), tmux_pane (line 665), and summary eligibility (line 864)
- Migration strategy:
  ```sql
  CREATE INDEX IF NOT EXISTS idx_missions_activity ON missions(last_active DESC, last_heartbeat DESC);
  CREATE INDEX IF NOT EXISTS idx_missions_tmux_pane ON missions(tmux_pane) WHERE tmux_pane IS NOT NULL;
  CREATE INDEX IF NOT EXISTS idx_missions_summary ON missions(status, prompt_count, last_summary_prompt_count);
  ```

High Priority Issues
--------------------

**P1-1: Large Configuration Files (741 & 493 lines)**
- Files: `internal/config/agenc_config.go`, `internal/config/config.go`
- Issue: Config validation, parsing, YAML handling, path helpers, and business logic all mixed together
- Recommendation: Split into config_validation.go, palette_commands.go, cron_config.go, oauth_setup.go, dir_structure.go

**P1-2: Disabled Credential Management System (~400 lines of commented code)**
- Files: `internal/wrapper/token_expiry.go`, `internal/wrapper/credential_sync.go`, `internal/wrapper/wrapper.go`
- Issue: Entire credential sync system disabled but preserved "for potential re-enablement"
- Impact: Confuses maintainers, increases cognitive load, makes code harder to read
- Recommendation: Remove entirely if no re-enablement planned within 3 months; it's in git history

**P1-3: Large Claude Config Builder (693 lines)**
- File: `internal/claudeconfig/build.go`
- Issue: Handles config building, file copying, path rewriting, JSON patching, and Keychain operations
- Recommendation: Extract to credentials.go, path_rewriting.go, file_operations.go

**P1-4: Limited Use of Interfaces**
- Issue: Only one file defines interfaces (`internal/daemon/cron_scheduler.go`)
- Impact: Tight coupling, difficult to mock dependencies, hard to test
- Recommendation: Define MissionStore interface for database, wrapper operations interface, use dependency injection

**P1-5: Excessive JSON Marshal/Unmarshal Operations**
- File: `internal/claudeconfig/merge.go` (87 occurrences across 15 files)
- Issue: Heavy JSON marshaling for deep merging without caching; called on every mission creation
- Impact: CPU overhead, memory allocations, repeated parsing
- Recommendation: Cache merged configs keyed by shadow repo commit hash

**P1-6: Subprocess Spawning for AI Summarization**
- File: `internal/daemon/mission_summarizer.go:139-162`
- Issue: Spawns new `claude` CLI subprocess for EVERY summarization (every 2 minutes per eligible mission)
- Impact: High CPU overhead, increased latency, unnecessary memory allocation
- Recommendation: Short-term: rate limiting or increase interval. Long-term: direct Anthropic API calls

**P1-7: Race Condition in Cron Scheduler**
- File: `internal/daemon/cron_scheduler.go:92-94, 123-125`
- Issue: `runningCount` read under lock but used outside lock in loop
- Impact: Can exceed maxConcurrent limit or skip jobs unnecessarily
- Recommendation: Hold lock for entire scheduling cycle or use atomic operations
- Concrete fix:
  ```go
  // Current (buggy):
  s.mu.Lock()
  runningCount := len(s.runningMissions)
  s.mu.Unlock()
  if runningCount >= maxConcurrent { continue }

  // Fixed:
  s.mu.Lock()
  if len(s.runningMissions) >= maxConcurrent {
      s.mu.Unlock()
      continue
  }
  // spawn job while holding lock
  s.runningMissions[missionID] = struct{}{}
  s.mu.Unlock()
  ```

**P1-8: API Design Inconsistencies**
- File: `internal/database/database.go`
- Issue: Inconsistent error semantics and naming across database API
- Impact: Error-prone for callers; must know each function's specific behavior
- Examples:
  - `GetMission(id)` returns `(nil, error)` when not found
  - `GetMissionByTmuxPane(paneID)` returns `(nil, nil)` when not found
  - `GetMostRecentMissionForCron(cronID)` returns `error` when not found
  - Update methods use inconsistent naming: `UpdateHeartbeat`, `UpdateLastActive`, `UpdateMissionPrompt`
- Recommendation: Standardize on one pattern (prefer: `(nil, nil)` for not-found, specific error only for actual failures)
- Long-term: Consider `Get` methods that return `(*Mission, bool, error)` where bool indicates "found"

Medium Priority Issues
----------------------

**P2-1: Summary Command Complexity (494 lines)**
- File: `cmd/summary.go`
- Issue: Single command file handles date parsing, statistics, JSONL parsing, git operations, and formatting
- Recommendation: Extract to `internal/summary/` package with stats.go, jsonl.go, git.go, format.go

**P2-2: Inconsistent Error Handling**
- Issue: 510 occurrences of `if err != nil` across 93 files with inconsistent wrapping
- Recommendation: Document patterns in CLAUDE.md; use stacktrace.NewError for new errors, stacktrace.Propagate for wrapping

**P2-3: Database Connection Pool Misconfiguration**
- File: `internal/database/database.go:87`
- Issue: SQLite with busy timeout of 5 seconds may cause blocking under high concurrency
- Recommendation: Increase to 10-15 seconds, add logging for slow operations

**P2-4: Inefficient Scanner Buffer Allocation**
- File: `internal/session/conversation.go:50`
- Issue: 1MB buffer allocated per JSONL read, no buffer reuse
- Recommendation: Use sync.Pool for scanner buffers

**P2-5: Git Operations Without Timeout**
- File: `internal/mission/repo.go`
- Issue: Git subprocess calls have no timeout, can hang indefinitely
- Recommendation: Add context with timeout, implement exponential backoff for fetch operations

**P2-6: Heartbeat Write Frequency**
- File: `internal/wrapper/wrapper.go:540-554`
- Issue: Every wrapper writes to database every 30 seconds (100 writes/min with 50 missions)
- Impact: SQLite database contention, unnecessary I/O
- Recommendation: Increase interval to 60-90 seconds or batch updates

**P2-7: Error Swallowing in Session Statistics**
- File: `cmd/summary.go:192-196, 359-363`
- Issue: Silently skips errors without logging
- Impact: Incomplete reports, difficult debugging
- Recommendation: Add verbose flag or debug logging

**P2-8: Potential Goroutine Leaks in Wrapper**
- File: `internal/wrapper/wrapper.go`
- Issue: Multiple goroutines spawned without WaitGroup tracking (lines 239, 286)
- Impact: Potential leaked goroutines if wrapper crashes
- Current mitigation: Channel is buffered (capacity 1), so goroutine completes even without reader
- Recommendation: Add WaitGroup for explicit tracking, ensure clean shutdown in error paths
- Consider: Add goroutine leak detection tests using `uber.go/goleak`

**P2-9: Nil Pointer Safety**
- File: `internal/database/database.go`
- Issue: Mission struct has 8+ pointer fields for nullable columns; dereferencing without nil checks
- Example: `internal/daemon/cron_scheduler.go:260` dereferences `*mission.CronName` (though line 245 checks, still risky pattern)
- Impact: Potential nil pointer panics if database contains unexpected NULL values
- Recommendation:
  - Add defensive nil checks before dereferencing pointer fields
  - Alternative: Use `sql.NullString` / `sql.NullTime` instead of pointers for nullable DB fields
  - Document which fields can be nil and require checks

**P2-10: Observability and Metrics Gaps**
- Files: No metrics collection infrastructure
- Issue: Zero visibility into production performance, error rates, or system health
- Missing:
  - No metrics collection (Prometheus, OpenTelemetry, etc.)
  - No tracing for request flows
  - No performance profiling endpoints (pprof)
  - No health check endpoint for daemon
  - No structured error rate tracking
- Impact: Cannot diagnose production issues, invisible performance degradation, no alerting foundation
- Recommendation:
  - Add basic metrics: active mission count, database query duration, cron execution time
  - Add pprof HTTP endpoint to daemon (behind localhost-only binding)
  - Add `agenc daemon health` command
  - Consider structured logging with severity levels for error tracking

Low Priority / Nice-to-Haves
-----------------------------

**P3-1: Formatting Inconsistencies (20 files need gofmt)**
- Recommendation: Run `gofmt -w .`, add pre-commit hook, add CI check

**P3-2: Lack of Package Documentation**
- Issue: No package-level doc comments or doc.go files
- Recommendation: Add doc.go to each internal package explaining purpose

**P3-3: Missing Constants for Magic Numbers**
- Examples: Heartbeat interval (30s), debounce periods (5s, 500ms), log rotation (10MB)
- Recommendation: Define package-level constants with clear names and documentation

**P3-4: Function Length Issues**
- Issue: Several functions exceed 100 lines (event loop in wrapper.go, runSummary in summary.go)
- Recommendation: Extract helper functions, aim for <40 lines per function

**P3-5: Hardcoded Model Name in Summarizer**
- File: `internal/daemon/mission_summarizer.go:35`
- Issue: `summarizerModel = "claude-haiku-4-5-20251001"` hardcoded
- Recommendation: Move to config.yml

**P3-6: Inconsistent Warning Message Formatting**
- Issue: Some use logger, some use fmt.Printf to stderr
- Recommendation: Create helper function for consistent warnings

**P3-7: Database Time Parsing Errors Silently Ignored**
- File: `internal/database/database.go:689-754`
- Issue: `m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)` discards errors
- Impact: Corrupted timestamps become zero values silently
- Recommendation: Return errors from scanner functions or log parsing failures

Quantitative Metrics
--------------------

**Code Coverage Analysis:**
```
Target: Run `go test -coverprofile=coverage.out ./...` to establish baseline

Expected results (estimated from test file counts):
  internal/claudeconfig:  ~85-90% (excellent - 7/7 files tested)
  internal/database:      ~70-75% (good - comprehensive tests)
  internal/wrapper:        0% (CRITICAL - no tests)
  internal/daemon:        <20% (only config merge tested)
  internal/session:        0% (CRITICAL - no tests)
  internal/mission:       ~30% (only URL parsing tested)

Overall estimated: 35-40% statement coverage
Target: >70% overall, >90% for critical paths (database, wrapper, daemon)
```

**Complexity Metrics:**
```
Target: Run `gocyclo -over 15 .` to find complex functions

Expected findings:
  - wrapper.go Run() method: likely >20 (state machine + event loop)
  - database.go ListMissions(): likely 15-18 (query building logic)
  - cron_scheduler.go runSchedulerCycle(): likely 18-20 (scheduling logic)

Target: Max cyclomatic complexity 15 per function
```

**Race Detector Results:**
```
Target: Run `go test -race ./...`

Known issues:
  - P1-7: Cron scheduler race condition (runningCount)
  - Potential races in wrapper goroutine coordination

Target: 0 race conditions
```

**Linter Findings:**
```
Target: Run `golangci-lint run` (after creating .golangci.yml)

Expected categories:
  - errcheck: ~20-30 unchecked errors
  - gosec: Potential security issues (file permissions, temp files)
  - unused: Unused functions from Keychain migration
  - gocyclo: Functions exceeding complexity threshold

Target: 0 critical issues, <10 warnings
```

**Dependency Audit:**
```
Run: go list -m -u all | grep '\[' (shows available updates)
Run: govulncheck (check for known vulnerabilities)

Current: All dependencies are recent and maintained
Action: No urgent dependency updates needed
```

Detailed Findings by Category
------------------------------

### Code Quality & Architecture

**Package Structure:**
- ✅ Good: Clear separation between cmd/, internal/, and specs/
- ✅ Good: Package responsibilities well-defined in architecture doc
- ❌ Bad: 78 command files in flat cmd/ directory (should be organized)
- ❌ Bad: God objects in database, wrapper, config packages

**File Size Distribution:**
- Files >700 lines: 3 (database.go 871, wrapper.go 771, agenc_config.go 741)
- Files >500 lines: 8 total
- Target: No file >400 lines

**Code Duplication:**
- 36 path helper functions with similar patterns (`Get*Dirpath`, `Get*Filepath`)
- Could benefit from PathResolver struct pattern

**Commented-Out Code:**
- ~20+ blocks of commented code from Keychain auth migration
- Entire files marked as disabled (credential_sync.go, token_expiry.go)
- Multiple sections in wrapper.go (lines 217-227, 256, 267, 295, 632, 675, 684, 690, 694)

**Naming & Conventions:**
- Generally follows Go idioms
- Consistent use of stacktrace library for errors
- Good use of package-level regex compilation

**Positive Observations:**
- Consistent error handling library (stacktrace)
- Good defer usage for cleanup
- Idiomatic Go throughout
- No TODO/FIXME comments littering code

---

### Testing & Documentation

**Test Coverage Analysis:**

**Critical Gaps (0 tests):**
- `internal/wrapper/` (6 production files, 0 test files) - CRITICAL
- `internal/session/` (2 production files, 0 test files) - HIGH
- `internal/version/` (1 file, not testable via unit tests) - LOW

**Partial Coverage:**
- `internal/daemon/` (8 production files, 1 test file - only tests claudeconfig merge, not daemon loops)
- `internal/config/` (3 production files, 2 test files - first_run.go missing)
- `internal/mission/` (2 production files, 1 test file - only URL parsing tested, not git operations or mission creation)
- `internal/tmux/` (2 production files, 1 test file - version detection missing)

**Good Coverage:**
- `internal/claudeconfig/` (7/7 files tested) - EXCELLENT
- `internal/database/` (1/1 tested, good behavioral tests)
- `internal/history/` (1/1 tested)
- `internal/tableprinter/` (1/1 tested)

**Overall Ratio:** 34 production files, 15 test files (44% files have tests)

**Test Quality:**
- ✅ Good: Table-driven tests with clear structure
- ✅ Good: Tests verify behavior, not just code execution
- ✅ Good: Proper use of t.Helper() and t.Cleanup()
- ❌ Missing: Integration tests for multi-goroutine coordination
- ❌ Missing: Error injection tests for failure modes
- ❌ Missing: End-to-end workflow tests

**Documentation Quality:**

**Excellent:**
- `docs/system-architecture.md` (528 lines) - comprehensive, accurate
- Verified against code: all daemon loops, database columns, package files match
- No documentation drift detected

**Good:**
- README.md (244 lines) - clear philosophy, installation, practical tips
- Inline function documentation - most exported functions documented
- CLI documentation - auto-generated from Cobra (always in sync)

**Adequate:**
- Code comments - critical sections well-commented
- Error messages - use stacktrace library with context

**Missing:**
- No package-level doc.go files
- No usage examples in tests
- No comprehensive config reference table
- Minimal troubleshooting section in README

---

### Dependencies & Performance

**Dependency Analysis:**
- ✅ All dependencies current and well-maintained
- ✅ No outdated or abandoned packages
- ⚠️ Both `github.com/goccy/go-yaml` AND `gopkg.in/yaml.v3` present - investigate if both needed

**Direct Dependencies (go.mod):**
- `github.com/adhocore/gronx v1.19.6` - Recent cron parser
- `github.com/fsnotify/fsnotify v1.9.0` - File watcher, actively maintained
- `github.com/goccy/go-yaml v1.19.2` - Recent YAML parser
- `github.com/google/uuid v1.6.0` - Standard UUID library
- `github.com/spf13/cobra v1.10.2` - CLI framework, up-to-date
- `modernc.org/sqlite v1.44.3` - Pure Go SQLite, recent

**Performance Patterns:**

**Good Patterns Found:**
- ✅ Package-level regex compilation (all regexes compiled once at init)
- ✅ strings.Builder usage for concatenation in loops
- ✅ Map size hints where capacity known
- ✅ Deferred cleanup for resources
- ✅ Context-based cancellation

**Anti-patterns Found:**
- ❌ Subprocess spawning in hot paths (mission summarizer)
- ❌ No query optimization (missing database indices)
- ❌ Excessive JSON marshaling (deep merge operations)
- ❌ High-frequency database writes (heartbeat every 30s)
- ❌ No caching layer (config merging repeated on every mission)

**Resource Leak Analysis:**
- ✅ Files: All file operations use defer file.Close() correctly
- ✅ Database: Single connection with proper Close() in all paths
- ⚠️ Goroutines: Potential leaks in wrapper but context-based cancellation should handle most cases

**Database Performance:**
- Issue: Only one index besides primary key
- Slow queries: Ordering by COALESCE (line 447), tmux_pane lookup (line 665), summary eligibility (line 864)
- Impact: Full table scans, O(n) scaling, noticeable with 100+ missions

**Git Operations:**
- Issue: No timeouts, can hang indefinitely on network failures
- Issue: rsync subprocess for repo copying (overhead on every mission creation)

---

### Technical Debt Markers & Security

**TODO/FIXME/HACK Comments:** ✅ None found - clean codebase

**Disabled Code:**
- `internal/wrapper/token_expiry.go` - entire file disabled (lines 1-4)
- `internal/wrapper/credential_sync.go` - entire file disabled (lines 1-5)
- Multiple commented sections in wrapper.go (~400 lines total)

**Security Assessment:**

**✅ PASSED:**
- SQL Injection Protection - all queries use parameterized statements
- No Hardcoded Secrets - proper use of Keychain/env vars
- Command Injection Protection - all exec.Command uses proper argument arrays
- No shell interpolation found

**⚠️ WATCH:**
- Path Traversal Risk - no obvious vulnerabilities, but verify mission IDs are validated as UUIDs
- Note: Regex validation exists for repo names (`^github\.com/[^/]+/[^/]+$`)

**Race Conditions:**
- Cron scheduler: runningCount read under lock, used outside lock
- Wrapper goroutines: potential leaks but buffered channel mitigates

**Error Handling:**
- Some error swallowing without logging (session statistics, custom title finding)
- Inconsistent error context in some locations
- Time parsing errors silently ignored in database scanner

**Build Status:**
- ✅ Build succeeds with no warnings
- ❌ Test suite has 4 failing tests (agenc_config_test.go)

Quick Wins (Day 1 - 4 hours)
-----------------------------

These are low-risk, high-value improvements that can be completed immediately:

1. **Run gofmt** (5 minutes)
   ```bash
   gofmt -w .
   git add -u
   git commit -m "Run gofmt on entire codebase"
   ```

2. **Fix failing tests** (30-60 minutes depending on root cause)
   - Priority: P0-5 - must fix before any other work
   - Run: `go test ./internal/config/...` to reproduce
   - Fix and verify

3. **Add constants for magic numbers** (1-2 hours)
   ```go
   // internal/wrapper/wrapper.go
   const (
       heartbeatInterval = 30 * time.Second
       gitWatchDebounce  = 5 * time.Second
   )

   // internal/daemon/mission_summarizer.go
   const (
       summarizerInterval        = 2 * time.Minute
       summarizerPromptThreshold = 10
       summarizerMaxMessages     = 15
   )
   ```

4. **Remove commented Keychain code** (1 hour)
   - Delete: `internal/wrapper/token_expiry.go`
   - Delete: `internal/wrapper/credential_sync.go`
   - Clean: Remove commented sections in wrapper.go (lines 217-227, 256, 267, 295, etc.)
   - Git history preserves this code if needed

5. **Create pre-commit hook** (15 minutes)
   ```bash
   # .git/hooks/pre-commit
   #!/bin/bash
   set -euo pipefail

   unformatted=$(gofmt -l .)
   if [[ -n "$unformatted" ]]; then
       echo "Files need formatting: $unformatted"
       echo "Run: gofmt -w ."
       exit 1
   fi

   go vet ./...
   ```

**Impact:** Immediate code quality improvement, foundation for larger refactors. Total time: ~4 hours.

Refactoring Strategies
----------------------

### P0-1: Splitting database.go (Step-by-Step)

**Current state:** 871 lines, 36+ exported functions

**Strategy:** Incremental extraction using receiver method package split pattern

**Week 1: Create new files (don't delete originals yet)**
```go
// internal/database/missions.go - CRUD operations
func (db *DB) CreateMission(gitRepo string, params *CreateMissionParams) (*Mission, error)
func (db *DB) GetMission(id string) (*Mission, error)
func (db *DB) ListMissions(params ListMissionsParams) ([]*Mission, error)
func (db *DB) ArchiveMission(id string) error
func (db *DB) DeleteMission(id string) error
// ... all Mission CRUD methods

// internal/database/migrations.go - Schema management
func ensureSchema(db *sql.DB) error
func migrateV1AddSessionName(db *sql.DB) error
func migrateV2AddCronTracking(db *sql.DB) error
// ... all 15+ migration functions

// internal/database/queries.go - Complex query builders
func buildListMissionsQuery(params ListMissionsParams) (string, []interface{})

// internal/database/scanners.go - Row scanning helpers
func scanMission(rows *sql.Rows) (*Mission, error)

// internal/database/database.go - Keep core DB struct and Open()
type DB struct { *sql.DB }
func Open(dbFilepath string) (*DB, error)
```

**Week 2: Update imports, verify tests**
- Update all `import "internal/database"` statements (none needed - same package)
- Run full test suite: `go test ./...`
- Verify no regressions

**Week 3: Delete duplicates from database.go**
- Remove functions now in missions.go, migrations.go, etc.
- Keep only DB struct, Open(), and Close()
- Target: database.go <200 lines

**Week 4: Add missing tests**
- Add tests for complex queries
- Add tests for migration idempotency

**Risk:** Medium - central component, but good test coverage mitigates
**Rollback:** Git revert if issues found

---

### P0-2: Refactoring wrapper.go (High Risk - Go Slow)

**Current state:** 771 lines, manages state machine + lifecycle + goroutines

**Strategy:** Extract in order of increasing risk, tests first

**Phase 1: Add comprehensive integration tests FIRST (Week 7)**

Create `internal/wrapper/wrapper_integration_test.go`:
```go
func TestGracefulRestart(t *testing.T) {
    // Spawn mock Claude process (sleep command)
    // Send restart command via socket
    // Assert: state transitions Running → RestartPending → Restarting
    // Assert: process receives SIGINT (not SIGKILL)
    // Assert: new process spawned with -c flag
}

func TestHardRestart(t *testing.T) {
    // Assert: transitions Running → Restarting (skips RestartPending)
    // Assert: process receives SIGKILL
}

func TestRestartIdempotency(t *testing.T) {
    // Send restart twice while in RestartPending
    // Assert: only one restart happens
}
```

**Phase 2: Extract state machine (Week 8, lowest risk)**
```go
// internal/wrapper/state.go
type WrapperState int

const (
    StateRunning WrapperState = iota
    StateRestartPending
    StateRestarting
)

func (s WrapperState) String() string { ... }
```
- Tests don't need to change (same public API)
- Low risk: just moving constants and type definitions

**Phase 3: Extract heartbeat (Week 9)**
```go
// internal/wrapper/heartbeat.go
func (w *Wrapper) writeHeartbeat(ctx context.Context) {
    ticker := time.NewTicker(heartbeatInterval)
    defer ticker.Stop()

    for {
        select {
        case <-ticker.C:
            // ... existing logic
        case <-ctx.Done():
            return
        }
    }
}
```

**Phase 4: Extract git watching (Week 10)**
```go
// internal/wrapper/git_watcher.go
func (w *Wrapper) watchWorkspaceRemoteRefs(ctx context.Context) { ... }
```

**Phase 5: Main wrapper.go cleanup (Week 11)**
- Remove all commented Keychain code (now that structure is stable)
- Document state transitions
- Target: wrapper.go <300 lines

**Risk:** HIGH - core runtime component
**Mitigation:** Tests first, incremental extraction, verify at each step
**Rollback:** Each phase is independently revertible

Risk Assessment Matrix
----------------------

| Issue | Priority | Risk Level | Blast Radius | Rollback Strategy | Should Do First |
|-------|----------|------------|--------------|-------------------|-----------------|
| P0-5: Fix failing tests | P0 | Low | CI only | Just fix them | ✅ Yes - blocks everything |
| P0-6: Add Go tooling | P0 | Low | Development process | Config changes only | ✅ Yes - prevents regression |
| P0-7: Context/timeouts | P0 | Medium | All long-running ops | Incremental, one package at a time | After tests |
| P0-8: Database indices | P0 | Low | Query performance | Indices are backwards-compatible | ✅ Yes - easy win |
| P1-2: Remove Keychain code | P1 | Low | Code clarity only | Git revert | ✅ Yes - quick win |
| P0-1: Split database.go | P0 | Medium | All DB consumers | Git revert, tests catch breaks | After tooling |
| P0-2: Refactor wrapper.go | P0 | **HIGH** | Mission lifecycle, ALL users | Incremental extraction, comprehensive tests FIRST | Last of P0s |
| P1-7: Fix race condition | P1 | Medium | Cron scheduler | 3-line fix, test with -race | After tests available |
| P0-4: Add wrapper tests | P0 | Medium | Test coverage | No production impact | Before wrapper refactor |
| P0-3: Organize cmd/ | P0 | Low | Import paths change | Update imports atomically | After tests |

**Critical Path:**
1. Fix failing tests (enables all other work)
2. Add Go tooling (prevents regression)
3. Quick wins (Keychain removal, constants, indices)
4. Add wrapper tests (enables safe refactoring)
5. Refactor wrapper.go (highest risk, needs tests first)

Recommendations
---------------

### Immediate Actions (Week 1):
1. ✅ **Fix failing tests** - P0-5 (blocks everything else)
2. ✅ **Add Go tooling infrastructure** - P0-6 (prevents regression)
3. ✅ **Quick wins** - gofmt, constants, remove Keychain code, add indices (4 hours total)
4. ✅ **Establish baseline metrics** - run coverage, linter, race detector to document current state

### Short-term (Weeks 2-3): Testing Infrastructure
5. **Add wrapper integration tests** - P0-4 (must happen before wrapper refactor)
   - Test state machine transitions
   - Test socket protocol
   - Test signal handling
   - Set up test fixtures and mocks
6. **Add cron scheduler tests** - P0-4 (complex logic needs coverage)
   - Test overlap policies
   - Test max concurrent enforcement
   - Test orphan adoption
7. **Add session name resolution tests** - P0-4 (affects UX)
8. **Set up coverage reporting in CI** - track improvements

### Medium-term (Weeks 4-6): Low-Risk Structural Work
9. **Add context/timeout to git operations** - P0-7 (high impact, moderate risk)
   - Start with internal/mission/repo.go (most critical)
   - Define timeout constants
   - Update all exec.Command calls to exec.CommandContext
10. **Split database.go** - P0-1 (follow detailed strategy above)
11. **Organize cmd/ directory** - P0-3 (low risk after tests in place)
12. **Fix cron scheduler race condition** - P1-7 (small fix, big impact)

### Longer-term (Weeks 7-11): High-Risk Refactoring
13. **Refactor wrapper.go** - P0-2 (follow detailed strategy above, needs wrapper tests first)
    - Week 7: Tests first
    - Weeks 8-11: Incremental extraction
14. **Split config files** - P1-1 (moderate impact)
15. **Split Claude config builder** - P1-3 (moderate impact)

### Ongoing Improvements (Weeks 12+):
16. **Add context to database operations** - P0-7 (breaks API, plan carefully)
17. **Standardize database API** - P1-8 (error semantics consistency)
18. **Implement configuration caching** - P1-5 (reduces JSON overhead)
19. **Add observability/metrics** - P2-10 (production visibility)
20. **Introduce interfaces** - P1-4 (for testability, after concrete types stabilized)
21. **Extract summary command logic** - P2-1 (low priority refactor)
22. **Add nil pointer guards** - P2-9 (safety improvement)

### Performance Optimizations:
23. **Reduce heartbeat frequency** - P2-6 (database load reduction)
24. **Optimize JSONL scanner allocation** - P2-4 (sync.Pool)
25. **Replace subprocess AI calls** - P1-6 (if performance becomes issue)

### Polish & Maintenance:
26. **Add package doc.go files** - P3-2
27. **Create config reference docs** - P2
28. **Document error handling patterns** - P2-2
29. **Extract long functions** - P3-4
30. **Standardize warning messages** - P3-6

Definition of Done Checklist
-----------------------------

Use these checklists to verify completion of major refactoring tasks:

### Database Split (P0-1)
- [ ] database.go is <200 lines (core Open/Close only)
- [ ] All tests pass: `go test ./internal/database/...`
- [ ] No new test coverage gaps (run: `go test -cover`)
- [ ] Code review approved by senior engineer
- [ ] No import cycles introduced
- [ ] Architecture doc updated if public API changed

### Wrapper Refactor (P0-2)
- [ ] Integration tests written and passing (before starting refactor)
- [ ] wrapper.go is <300 lines
- [ ] State machine extracted to state.go
- [ ] All tests pass: `go test ./internal/wrapper/...`
- [ ] Race detector clean: `go test -race ./internal/wrapper/...`
- [ ] Verified in staging environment with real missions
- [ ] Rollback plan documented

### Go Tooling Setup (P0-6)
- [ ] .golangci.yml created with required linters
- [ ] CI runs: `go test -race -cover ./...`
- [ ] CI runs: `golangci-lint run`
- [ ] Pre-commit hook installed and tested
- [ ] Coverage report generated and published
- [ ] Team trained on new workflow

### Context Addition (P0-7)
- [ ] All git operations use exec.CommandContext
- [ ] Database methods accept ctx parameter
- [ ] Timeout constants defined and documented
- [ ] Tests verify timeout behavior
- [ ] Backward compatibility maintained (or migration plan executed)

---

**Report Generated:** 2026-02-13
**Report Updated:** 2026-02-13 (added missing Go-specific analysis)
**Codebase Version:** Based on git commit `9f69e0d` (main branch)
**Total Lines Analyzed:** ~10,000+ lines across 135 Go files
**Analysis Team:** 4 specialized agents (Code Quality, Testing, Performance, Security)
**Review Team:** 3 senior Go engineers (Best Practices, Accuracy, Actionability)

**Overall Assessment:** 7.0/10 - Solid foundation with accurate findings, but required additional Go-specific depth, quantitative metrics, and concrete refactoring strategies. Updated report now provides actionable guidance for a successful multi-month cleanup effort.
