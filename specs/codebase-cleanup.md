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

**P0-6: Database Performance - Missing Query Indices**
- File: `internal/database/database.go`
- Issue: Only one index (short_id) besides primary key; common queries do full table scans
- Impact: O(n) scaling with mission count, noticeable slowdown with 100+ missions
- Recommendation: Add indices for last_active/last_heartbeat (line 447), tmux_pane (line 665), and summary eligibility (line 864)

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

**P1-7: Race Condition Risk in Cron Scheduler**
- File: `internal/daemon/cron_scheduler.go:92-94, 123-125`
- Issue: `runningCount` read under lock but used outside lock in loop
- Impact: Can exceed maxConcurrent limit or skip jobs unnecessarily
- Recommendation: Hold lock for entire scheduling cycle or use atomic operations

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
- Issue: Multiple goroutines spawned without WaitGroup tracking
- Impact: Potential leaked goroutines if wrapper crashes
- Recommendation: Add WaitGroup, ensure clean shutdown in error paths

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

Recommendations
---------------

### Immediate Actions (Week 1):
1. **Fix failing tests** in `internal/config/agenc_config_test.go` (P0)
2. **Remove deprecated Keychain code** - low risk, immediate clarity improvement (P1)
3. **Add database indices** for common query patterns (P0)

### Short-term (Weeks 2-4):
4. **Organize cmd/ directory** into subdirectories - improves navigation, low risk (P0)
5. **Add integration tests for wrapper state machine** - highest risk area (P0)
6. **Add unit tests for cron scheduler** - highest complexity area (P0)
7. **Split database.go** - high impact, moderate risk (P0)

### Medium-term (Weeks 5-8):
8. **Refactor wrapper.go** - high impact, higher risk (core runtime) (P0)
9. **Split config files** - moderate impact (P1)
10. **Implement configuration caching** - reduces JSON marshaling overhead (P1)
11. **Add session name resolution tests** - affects UX (P0)
12. **Add mission creation integration tests** - end-to-end workflow (P1)

### Long-term (Weeks 9+):
13. **Introduce interfaces** for database, wrapper operations (P1)
14. **Extract summary command logic** to internal package (P2)
15. **Replace subprocess AI calls** with direct API if performance becomes issue (P2)
16. **Add goroutine leak detection** and tracking (P2)
17. **Implement retry logic** for transient failures (P2)
18. **Add package-level doc.go files** (P3)
19. **Create comprehensive config reference** documentation (P2)

### Performance Optimizations:
20. **Reduce heartbeat frequency** to 60-90 seconds or batch updates (P2)
21. **Add timeouts to all git operations** (P2)
22. **Optimize JSONL scanner buffer allocation** with sync.Pool (P2)
23. **Add metrics and observability** for performance monitoring (P3)

### Code Quality Improvements:
24. **Run gofmt on entire codebase** and add pre-commit hook (P3)
25. **Document error handling patterns** in CLAUDE.md (P2)
26. **Define constants for magic numbers** with explanations (P3)
27. **Extract helper functions** to reduce function length (P3)
28. **Standardize warning message formatting** (P3)

---

**Report Generated:** 2026-02-13
**Codebase Version:** Based on git commit `9f69e0d` (main branch)
**Total Lines Analyzed:** ~10,000+ lines across 135 Go files
**Analysis Team:** 4 specialized agents (Code Quality, Testing, Performance, Security)
