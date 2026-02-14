Code Quality Baseline
======================

**Date:** 2026-02-14
**Purpose:** Establish baseline metrics to track technical debt cleanup progress

Test Coverage
-------------

| Package | Coverage |
|---------|----------|
| github.com/odyssey/agenc | 0.0% |
| github.com/odyssey/agenc/cmd | 0.0% |
| github.com/odyssey/agenc/cmd/gendocs | 0.0% |
| github.com/odyssey/agenc/cmd/genskill | 0.0% |
| github.com/odyssey/agenc/internal/claudeconfig | 35.5% |
| github.com/odyssey/agenc/internal/config | 38.7% (FAILING TESTS) |
| github.com/odyssey/agenc/internal/daemon | 14.6% |
| github.com/odyssey/agenc/internal/database | 50.7% |
| github.com/odyssey/agenc/internal/history | 92.6% |
| github.com/odyssey/agenc/internal/mission | 17.0% |
| github.com/odyssey/agenc/internal/session | 93.6% |
| github.com/odyssey/agenc/internal/tableprinter | 100.0% |
| github.com/odyssey/agenc/internal/tmux | 40.7% |
| github.com/odyssey/agenc/internal/version | [no test files] |
| github.com/odyssey/agenc/internal/wrapper | 28.3% |

**Overall:** ~38.1% coverage (estimated weighted average)

**Failing Tests:**
- `internal/config` package has 3 failing tests related to palette command keybinding configuration
  - TestPaletteCommands_BuiltinDefaults
  - TestPaletteCommands_KeybindingUniqueness
  - TestPaletteTmuxKeybinding_ConflictsWithCommand

**Coverage Gaps:**
- Main package and cmd packages have 0% coverage
- Low coverage in daemon (14.6%), mission (17.0%), and wrapper (28.3%)
- version package has no test files

**Strong Coverage:**
- tableprinter: 100%
- session: 93.6%
- history: 92.6%

Cyclomatic Complexity
---------------------

Tool not installed. Install with:
```
go install github.com/fzipp/gocyclo/cmd/gocyclo@latest
```

Race Detector
-------------

**Status:** FAIL

The race detector test suite fails due to failing tests in `internal/config`, not race conditions:
- TestPaletteCommands_BuiltinDefaults
- TestPaletteCommands_KeybindingUniqueness
- TestPaletteTmuxKeybinding_ConflictsWithCommand

**No race conditions detected** in the packages that ran successfully. The failure is due to existing test failures, not race detector findings.

Linter Issues
-------------

**Status:** golangci-lint not installed locally

Linter is configured in `.golangci.yml` and runs in CI. See GitHub Actions for current linter results.

To run locally, install with:
```
brew install golangci-lint
```

Dependencies
------------

**Outdated dependencies:** 14 packages have available updates

| Package | Current | Available |
|---------|---------|-----------|
| github.com/clipperhouse/uax29/v2 | v2.2.0 | v2.6.0 |
| github.com/cpuguy83/go-md2man/v2 | v2.0.6 | v2.0.7 |
| github.com/google/go-cmp | v0.6.0 | v0.7.0 |
| github.com/google/pprof | v0.0.0-20250317173921-a4b03ec1a45e | v0.0.0-20260202012954-cb029daf43ef |
| github.com/mieubrisse/stacktrace | v0.0.0-20260130152157-50c8c98aa97d | v0.1.0 |
| github.com/rivo/uniseg | v0.2.0 | v0.4.7 |
| github.com/spf13/pflag | v1.0.9 | v1.0.10 |
| github.com/stretchr/objx | v0.5.2 | v0.5.3 |
| github.com/stretchr/testify | v1.10.0 | v1.11.1 |
| golang.org/x/exp | v0.0.0-20251023183803-a4bb9ffd2546 | v0.0.0-20260212183809-81e46e3db34a |
| golang.org/x/mod | v0.32.0 | v0.33.0 |
| golang.org/x/sync | v0.17.0 | v0.19.0 |
| golang.org/x/tools | v0.40.0 | v0.42.0 |
| gopkg.in/check.v1 | v0.0.0-20161208181325-20d25e280405 | v1.0.0-20201130134442-10cb98267c6c |
| modernc.org/ccgo/v4 | v4.30.1 | v4.30.2 |
| modernc.org/gc/v3 | v3.1.1 | v3.1.2 |
| modernc.org/libc | v1.67.6 | v1.67.7 |
| modernc.org/sqlite | v1.44.3 | v1.45.0 |

**All dependencies up to date:** NO (18 updates available)

Priority Updates
----------------

Based on this baseline, the following areas should be prioritized for improvement:

1. **Fix failing tests** in `internal/config` package (3 tests)
2. **Improve test coverage** in low-coverage packages:
   - daemon: 14.6% → target 60%+
   - mission: 17.0% → target 60%+
   - wrapper: 28.3% → target 60%+
   - cmd packages: 0% → target 40%+
3. **Update critical dependencies:**
   - github.com/mieubrisse/stacktrace to v0.1.0 (official release)
   - github.com/stretchr/testify to v1.11.1 (testing framework)
4. **Install quality tools:**
   - gocyclo for complexity analysis
   - golangci-lint for local linting
