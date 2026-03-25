Self-Contained Test Environment — Implementation Plan
=====================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Allow agents working on the AgenC codebase to spin up an isolated pocket AgenC environment for end-to-end validation of their changes.

**Architecture:** System-global resource names (tmux sessions, launchd plists) get a deterministic hash suffix derived from AGENC_DIRPATH. A wrapper script (`_build/agenc-test`) sets the right environment variables before execing the real binary. Makefile targets create and tear down the test environment. Features that would conflict with the global installation (tmux keybinding injection) return errors when `AGENC_TEST_ENV=1` is set.

**Tech Stack:** Go, Make, bash, SHA256 hashing, launchd, tmux

**Design doc:** `docs/plans/2026-03-24-test-env-design.md`

---

Task 1: Namespace resolver
--------------------------

**Files:**
- Create: `internal/config/namespace.go`
- Create: `internal/config/namespace_test.go`

**Step 1: Write the tests**

```go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGetNamespaceSuffix(t *testing.T) {
	t.Run("returns empty string for default agenc path", func(t *testing.T) {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("failed to get home dir: %v", err)
		}
		defaultPath := filepath.Join(homeDir, ".agenc")
		suffix := GetNamespaceSuffix(defaultPath)
		if suffix != "" {
			t.Errorf("expected empty suffix for default path, got %q", suffix)
		}
	})

	t.Run("returns hash suffix for non-default path", func(t *testing.T) {
		suffix := GetNamespaceSuffix("/tmp/test-agenc-12345")
		if suffix == "" {
			t.Error("expected non-empty suffix for non-default path")
		}
		// Should start with "-"
		if suffix[0] != '-' {
			t.Errorf("expected suffix to start with '-', got %q", suffix)
		}
		// Should be "-" + 8 hex chars = 9 chars total
		if len(suffix) != 9 {
			t.Errorf("expected suffix length 9, got %d: %q", len(suffix), suffix)
		}
	})

	t.Run("is deterministic", func(t *testing.T) {
		path := "/tmp/test-agenc-deterministic"
		s1 := GetNamespaceSuffix(path)
		s2 := GetNamespaceSuffix(path)
		if s1 != s2 {
			t.Errorf("non-deterministic: %q != %q", s1, s2)
		}
	})

	t.Run("different paths produce different suffixes", func(t *testing.T) {
		s1 := GetNamespaceSuffix("/tmp/agenc-aaa")
		s2 := GetNamespaceSuffix("/tmp/agenc-bbb")
		if s1 == s2 {
			t.Errorf("different paths produced same suffix: %q", s1)
		}
	})
}

func TestGetTmuxSessionName(t *testing.T) {
	t.Run("default path returns agenc", func(t *testing.T) {
		homeDir, _ := os.UserHomeDir()
		got := GetTmuxSessionName(filepath.Join(homeDir, ".agenc"))
		if got != "agenc" {
			t.Errorf("expected 'agenc', got %q", got)
		}
	})

	t.Run("custom path returns agenc-HASH", func(t *testing.T) {
		got := GetTmuxSessionName("/tmp/test-agenc")
		if got == "agenc" {
			t.Error("expected namespaced session name, got plain 'agenc'")
		}
		if len(got) != len("agenc")+9 { // "agenc" + "-" + 8 hex chars
			t.Errorf("unexpected length: %q", got)
		}
	})
}

func TestGetPoolSessionName(t *testing.T) {
	t.Run("default path returns agenc-pool", func(t *testing.T) {
		homeDir, _ := os.UserHomeDir()
		got := GetPoolSessionName(filepath.Join(homeDir, ".agenc"))
		if got != "agenc-pool" {
			t.Errorf("expected 'agenc-pool', got %q", got)
		}
	})

	t.Run("custom path returns agenc-HASH-pool", func(t *testing.T) {
		got := GetPoolSessionName("/tmp/test-agenc")
		if got == "agenc-pool" {
			t.Error("expected namespaced pool name, got plain 'agenc-pool'")
		}
		// Should end with "-pool"
		if got[len(got)-5:] != "-pool" {
			t.Errorf("expected suffix '-pool', got %q", got)
		}
	})
}

func TestIsTestEnv(t *testing.T) {
	t.Run("returns false when unset", func(t *testing.T) {
		os.Unsetenv("AGENC_TEST_ENV")
		if IsTestEnv() {
			t.Error("expected false when AGENC_TEST_ENV is unset")
		}
	})

	t.Run("returns true when set to 1", func(t *testing.T) {
		t.Setenv("AGENC_TEST_ENV", "1")
		if !IsTestEnv() {
			t.Error("expected true when AGENC_TEST_ENV=1")
		}
	})
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/config/ -run "TestGetNamespaceSuffix|TestGetTmuxSessionName|TestGetPoolSessionName|TestIsTestEnv" -v`
Expected: FAIL — functions not defined

**Step 3: Write the implementation**

```go
package config

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
)

const (
	testEnvVar     = "AGENC_TEST_ENV"
	baseNamePrefix = "agenc"
)

// GetNamespaceSuffix returns a deterministic suffix derived from agencDirpath.
// If agencDirpath is the default (~/.agenc), returns "" (empty string).
// Otherwise, returns "-" + first 8 characters of SHA256 of the resolved path.
func GetNamespaceSuffix(agencDirpath string) string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Can't determine home dir — assume non-default to be safe
		return computeHashSuffix(agencDirpath)
	}
	defaultPath := filepath.Join(homeDir, defaultAgencDirname)
	if agencDirpath == defaultPath {
		return ""
	}
	return computeHashSuffix(agencDirpath)
}

// computeHashSuffix returns "-" + first 8 hex chars of SHA256 of the path.
func computeHashSuffix(path string) string {
	hash := sha256.Sum256([]byte(path))
	return fmt.Sprintf("-%x", hash[:4])
}

// GetTmuxSessionName returns the user-facing tmux session name.
// Default: "agenc". Namespaced: "agenc-HASH".
func GetTmuxSessionName(agencDirpath string) string {
	return baseNamePrefix + GetNamespaceSuffix(agencDirpath)
}

// GetPoolSessionName returns the pool tmux session name.
// Default: "agenc-pool". Namespaced: "agenc-HASH-pool".
func GetPoolSessionName(agencDirpath string) string {
	return baseNamePrefix + GetNamespaceSuffix(agencDirpath) + "-pool"
}

// GetCronPlistPrefix returns the prefix for cron plist filenames and labels.
// Default: "agenc-cron.". Namespaced: "agenc-HASH-cron.".
func GetCronPlistPrefix(agencDirpath string) string {
	return baseNamePrefix + GetNamespaceSuffix(agencDirpath) + "-cron."
}

// IsTestEnv returns true if AGENC_TEST_ENV is set (to any non-empty value).
func IsTestEnv() bool {
	return os.Getenv(testEnvVar) != ""
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/ -run "TestGetNamespaceSuffix|TestGetTmuxSessionName|TestGetPoolSessionName|TestIsTestEnv" -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/config/namespace.go internal/config/namespace_test.go
git commit -m "Add namespace resolver for test environment isolation"
```

---

Task 2: Dynamic tmux session names (cmd package)
-------------------------------------------------

**Files:**
- Modify: `cmd/tmux_helpers.go` (remove constant, add function)
- Modify: all callers of `tmuxSessionName` in `cmd/`

**Step 1: Find all references to the constant**

Run: `grep -rn 'tmuxSessionName' cmd/`

This will identify every file in `cmd/` that uses the constant. Each reference
needs to be updated to call a function that resolves `agencDirpath` first.

**Step 2: Update `cmd/tmux_helpers.go`**

Remove the `tmuxSessionName` constant. Replace with a function:

```go
// getTmuxSessionName resolves the agenc tmux session name for this installation.
func getTmuxSessionName() (string, error) {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to resolve agenc directory")
	}
	return config.GetTmuxSessionName(agencDirpath), nil
}
```

Add import for `"github.com/odyssey/agenc/internal/config"` if not already present.

**Step 3: Update every caller**

Each caller that currently references `tmuxSessionName` (the constant) must call
`getTmuxSessionName()` instead and handle the error. Common pattern:

Before:
```go
if tmuxSessionExists(tmuxSessionName) {
```

After:
```go
sessionName, err := getTmuxSessionName()
if err != nil {
    return err
}
if tmuxSessionExists(sessionName) {
```

Files likely affected (verify with grep):
- `cmd/tmux_attach.go` — session creation and attachment
- `cmd/tmux_inject.go` — if it references the session name (check)
- Any other `cmd/` files referencing the constant

**Step 4: Build to verify compilation**

Run: `make bin` (with `dangerouslyDisableSandbox: true`)
Expected: Compiles without errors

**Step 5: Commit**

```
git add cmd/tmux_helpers.go cmd/tmux_attach.go <other modified files>
git commit -m "Make tmux session name dynamic via namespace resolver"
```

---

Task 3: Dynamic pool session name (server package)
---------------------------------------------------

**Files:**
- Modify: `internal/server/pool.go` (remove constant, thread agencDirpath)

**Step 1: Find all references to `poolSessionName`**

Run: `grep -rn 'poolSessionName' internal/server/`

This constant is used extensively in pool.go. Every reference needs to be
replaced with a value derived from `agencDirpath`.

**Step 2: Determine how pool.go gets agencDirpath**

The Server struct likely already has agencDirpath as a field. Check:

Run: `grep -n 'agencDirpath' internal/server/server.go | head -20`

If the Server struct has it, the `(s *Server)` methods can use `s.agencDirpath`.
For package-level functions (like `poolSessionExists()`, `getLinkedPaneIDs()`,
`listPoolPaneIDs()`), they must gain an `agencDirpath` parameter or be converted
to methods on Server.

**Step 3: Remove the constant, add a method**

Remove: `const poolSessionName = "agenc-pool"`

Add a helper on the Server (or accept agencDirpath as a parameter for
package-level funcs):

```go
func (s *Server) getPoolSessionName() string {
    return config.GetPoolSessionName(s.agencDirpath)
}
```

For package-level functions that are called from outside Server methods, they
need to accept a pool session name parameter:

```go
// Before:
func poolSessionExists() bool {
    return tmuxSessionExists(poolSessionName)
}

// After:
func poolSessionExists(poolSessionName string) bool {
    return tmuxSessionExists(poolSessionName)
}
```

**Step 4: Update every reference**

Replace every `poolSessionName` usage with either `s.getPoolSessionName()` (in
Server methods) or the passed parameter (in package-level functions).

Key functions to update:
- `ensurePoolSession()` — uses poolSessionName for tmux new-session
- `createPoolWindow()` — uses poolSessionName for tmux new-window target
- `poolSessionExists()` — uses poolSessionName for tmux has-session
- `poolWindowExistsByPane()` — uses poolSessionName for isPaneInSession
- `killExtraPanesInWindow()` — uses poolSessionName for tmux list-panes
- `getLinkedPaneIDs()` — compares session names against poolSessionName
- `getLinkedPaneSessions()` — filters out poolSessionName
- `listPoolPaneIDs()` — uses poolSessionName for tmux list-panes

**Step 5: Build and run existing tests**

Run: `make bin` (with `dangerouslyDisableSandbox: true`)
Run: `go test ./internal/server/ -v`
Expected: Compiles, existing tests pass

**Step 6: Commit**

```
git add internal/server/pool.go <other modified files in internal/server/>
git commit -m "Make pool session name dynamic via namespace resolver"
```

---

Task 4: Dynamic launchd plist prefix
-------------------------------------

**Files:**
- Modify: `internal/launchd/plist.go` (change constant to accept agencDirpath)
- Modify: `internal/launchd/manager.go` (ListAgencCronJobs needs prefix param)
- Modify: `internal/server/cron_syncer.go` (pass agencDirpath to launchd funcs)
- Modify: `internal/launchd/plist_test.go` (update tests)
- Modify: `internal/launchd/manager_test.go` (update tests)
- Modify: `internal/server/cron_syncer_test.go` (update tests)

**Step 1: Update `internal/launchd/plist.go`**

The `CronPlistPrefix` constant stays as a backward-compatible default (used for
legacy cleanup). Add new functions that accept a prefix:

```go
// CronToPlistFilenameWithPrefix returns the plist filename using a custom prefix.
func CronToPlistFilenameWithPrefix(prefix string, cronID string) string {
    return fmt.Sprintf("%s%s.plist", prefix, cronID)
}

// CronToLabelWithPrefix returns the launchd label using a custom prefix.
func CronToLabelWithPrefix(prefix string, cronID string) string {
    return prefix + cronID
}
```

Update `CronToPlistFilename` and `CronToLabel` to delegate:

```go
func CronToPlistFilename(cronID string) string {
    return CronToPlistFilenameWithPrefix(CronPlistPrefix, cronID)
}

func CronToLabel(cronID string) string {
    return CronToLabelWithPrefix(CronPlistPrefix, cronID)
}
```

**Step 2: Update `internal/launchd/manager.go`**

`ListAgencCronJobs` currently hardcodes `CronPlistPrefix` and `LegacyCronPlistPrefix`.
Add a parameter for the current prefix:

```go
func (m *Manager) ListAgencCronJobsWithPrefix(prefix string) ([]string, error) {
    // ... same logic but use `prefix` instead of CronPlistPrefix
    // Keep LegacyCronPlistPrefix for legacy cleanup
}
```

Update `ListAgencCronJobs` to delegate with the default prefix.

Similarly update `GetPlistPathForLabel` if it hardcodes the prefix.

**Step 3: Update `internal/server/cron_syncer.go`**

The CronSyncer already has `agencDirpath`. Use `config.GetCronPlistPrefix(s.agencDirpath)`
to compute the prefix, then pass it to the launchd functions:

```go
func (s *CronSyncer) getCronPlistPrefix() string {
    return config.GetCronPlistPrefix(s.agencDirpath)
}
```

Update `syncCronJob`, `removeUnmatchedPlists`, and `removeOrphanedLaunchdJobs`
to use the dynamic prefix.

**Step 4: Update the launchdManager interface**

The `launchdManager` interface needs `ListAgencCronJobsWithPrefix`:

```go
type launchdManager interface {
    // ... existing methods ...
    ListAgencCronJobsWithPrefix(prefix string) ([]string, error)
}
```

**Step 5: Update tests**

Update `plist_test.go`, `manager_test.go`, and `cron_syncer_test.go` for the
new function signatures. Existing tests that use the default prefix should
continue to pass unchanged.

**Step 6: Build and test**

Run: `go test ./internal/launchd/ ./internal/server/ -v`
Expected: All tests pass

**Step 7: Commit**

```
git add internal/launchd/plist.go internal/launchd/manager.go internal/server/cron_syncer.go
git add internal/launchd/plist_test.go internal/launchd/manager_test.go internal/server/cron_syncer_test.go
git commit -m "Make cron plist prefix dynamic via namespace resolver"
```

---

Task 5: AGENC_TEST_ENV gating for tmux keybinding injection
------------------------------------------------------------

**Files:**
- Modify: `cmd/tmux_inject.go` (add test env check)

**Step 1: Add the gate at the top of `runTmuxInject`**

```go
func runTmuxInject(cmd *cobra.Command, args []string) error {
    if config.IsTestEnv() {
        return stacktrace.NewError(
            "tmux keybinding injection is disabled in test environments (AGENC_TEST_ENV=1)")
    }
    // ... rest of existing function
}
```

**Step 2: Build to verify**

Run: `make bin` (with `dangerouslyDisableSandbox: true`)
Expected: Compiles

**Step 3: Commit**

```
git add cmd/tmux_inject.go
git commit -m "Gate tmux keybinding injection on AGENC_TEST_ENV"
```

---

Task 6: Makefile targets and wrapper script
-------------------------------------------

**Files:**
- Modify: `Makefile` (add `test-env`, `test-env-clean` targets)
- Modify: `.gitignore` (add `_build/`, `_test-env/`)

**Step 1: Update `.gitignore`**

Replace the existing `/agenc` line and add new entries:

```
# Build output
/_build/
/agenc
/dist/

# Test environment
/_test-env/
```

**Step 2: Add Makefile targets**

Add the following to the end of the Makefile (before `clean:`). Note: the
`compile` target currently builds to `./agenc`. Update it to build to
`_build/agenc` instead, and update `clean` accordingly:

Update `compile`:
```makefile
compile:
	@mkdir -p _build
	@echo "Building agenc..."
	@go build -ldflags "$(LDFLAGS)" -o _build/agenc .
	@echo "✓ Build complete (_build/agenc)"
```

Update `clean`:
```makefile
clean:
	rm -rf _build
```

Add wrapper script generation to `compile` (or as a separate step):
```makefile
compile:
	@mkdir -p _build
	@echo "Building agenc..."
	@go build -ldflags "$(LDFLAGS)" -o _build/agenc .
	@# Generate test wrapper script
	@abs_test_env=$$(cd . && pwd)/_test-env; \
	abs_build=$$(cd . && pwd)/_build; \
	printf '#!/usr/bin/env bash\nset -euo pipefail\nscript_dirpath="$$(cd "$$(dirname "$${0}")" && pwd)"\nexport AGENC_DIRPATH="%s"\nexport AGENC_TEST_ENV=1\nexec "$${script_dirpath}/agenc" "$$@"\n' "$$abs_test_env" > _build/agenc-test; \
	chmod +x _build/agenc-test
	@echo "✓ Build complete (_build/agenc, _build/agenc-test)"
```

Add `test-env`:
```makefile
TEST_ENV_DIR := _test-env
REAL_AGENC_DIR := $(HOME)/.agenc

.PHONY: test-env test-env-clean

test-env:
	@if [ -d "$(TEST_ENV_DIR)" ]; then \
		echo "Test environment already exists at $(TEST_ENV_DIR)"; \
		echo "Run 'make test-env-clean' first to recreate."; \
		exit 0; \
	fi
	@echo "Creating test environment..."
	@mkdir -p $(TEST_ENV_DIR)/config/claude-modifications
	@mkdir -p $(TEST_ENV_DIR)/cache
	@mkdir -p $(TEST_ENV_DIR)/repos
	@mkdir -p $(TEST_ENV_DIR)/missions
	@mkdir -p $(TEST_ENV_DIR)/server
	@mkdir -p $(TEST_ENV_DIR)/logs/crons
	@mkdir -p $(TEST_ENV_DIR)/stash
	@mkdir -p $(TEST_ENV_DIR)/claude
	@# Initialize local-only git repo in config/
	@cd $(TEST_ENV_DIR)/config && git init --quiet
	@# Seed config files
	@touch $(TEST_ENV_DIR)/config/config.yml
	@touch $(TEST_ENV_DIR)/config/claude-modifications/CLAUDE.md
	@echo '{}' > $(TEST_ENV_DIR)/config/claude-modifications/settings.json
	@# Copy OAuth token if available
	@if [ -f "$(REAL_AGENC_DIR)/cache/oauth-token" ]; then \
		cp "$(REAL_AGENC_DIR)/cache/oauth-token" "$(TEST_ENV_DIR)/cache/oauth-token"; \
		chmod 600 "$(TEST_ENV_DIR)/cache/oauth-token"; \
		echo "  Copied OAuth token from $(REAL_AGENC_DIR)"; \
	else \
		echo "  Warning: no OAuth token found at $(REAL_AGENC_DIR)/cache/oauth-token"; \
	fi
	@echo "✓ Test environment created at $(TEST_ENV_DIR)/"
	@echo "  Use _build/agenc-test to run commands against it."
```

Add `test-env-clean`:
```makefile
test-env-clean:
	@if [ ! -d "$(TEST_ENV_DIR)" ]; then \
		echo "No test environment found."; \
		exit 0; \
	fi
	@echo "Cleaning up test environment..."
	@# Kill server process if running
	@if [ -f "$(TEST_ENV_DIR)/server/server.pid" ]; then \
		pid=$$(cat "$(TEST_ENV_DIR)/server/server.pid" 2>/dev/null); \
		if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
			echo "  Stopping server (PID $$pid)..."; \
			kill "$$pid" 2>/dev/null || true; \
			sleep 1; \
			kill -0 "$$pid" 2>/dev/null && kill -9 "$$pid" 2>/dev/null || true; \
		fi; \
	fi
	@# Compute namespace hash and kill tmux sessions
	@abs_path=$$(cd "$(TEST_ENV_DIR)" && pwd); \
	hash=$$(printf '%s' "$$abs_path" | shasum -a 256 | cut -c1-8); \
	session_name="agenc-$$hash"; \
	pool_name="agenc-$$hash-pool"; \
	tmux kill-session -t "=$$pool_name" 2>/dev/null && echo "  Killed tmux session $$pool_name" || true; \
	tmux kill-session -t "=$$session_name" 2>/dev/null && echo "  Killed tmux session $$session_name" || true; \
	# Clean up launchd plists \
	plist_prefix="agenc-$$hash-cron."; \
	plist_dir="$$HOME/Library/LaunchAgents"; \
	if [ -d "$$plist_dir" ]; then \
		for plist in "$$plist_dir/$$plist_prefix"*.plist; do \
			[ -f "$$plist" ] || continue; \
			label=$$(basename "$$plist" .plist); \
			launchctl remove "$$label" 2>/dev/null || true; \
			rm -f "$$plist"; \
			echo "  Removed launchd plist $$label"; \
		done; \
	fi
	@rm -rf "$(TEST_ENV_DIR)"
	@echo "✓ Test environment cleaned up."
```

**Step 3: Update the `.PHONY` line**

```makefile
.PHONY: bin build check clean compile docs genprime setup test test-env test-env-clean
```

**Step 4: Verify**

Run: `make test-env` (with `dangerouslyDisableSandbox: true`)
Expected: Creates `_test-env/` directory structure

Run: `make test-env-clean` (with `dangerouslyDisableSandbox: true`)
Expected: Removes `_test-env/`

**Step 5: Commit**

```
git add Makefile .gitignore
git commit -m "Add test-env Makefile targets and move binary to _build/"
```

---

Task 7: Update CLAUDE.md and settings
--------------------------------------

**Files:**
- Modify: `CLAUDE.md` (document test binary, update `./agenc` references)
- Modify: `.claude/settings.json` (update `Bash(./agenc:*)` to `Bash(./_build/agenc:*)`)

**Step 1: Update CLAUDE.md**

In the "Running the Binary" section, update the instructions:

- Change `./agenc` references to `./_build/agenc`
- Add a new section about the test environment:

```markdown
Test Environment
----------------

When testing changes end-to-end, use the test environment:

1. `make build` — compiles `_build/agenc` (production binary) and
   `_build/agenc-test` (wrapper that targets `_test-env/`)
2. `make test-env` — creates the isolated `_test-env/` directory with its own
   database, server, repos, and config
3. Use `_build/agenc-test` for all test commands — it automatically sets
   `AGENC_DIRPATH` and `AGENC_TEST_ENV=1`
4. `make test-env-clean` — tears down the test environment, killing its server,
   tmux sessions, and launchd plists

**Important:** `_build/agenc-test` is hardwired to the `_test-env/` directory
in this repo. It is NOT a general-purpose binary. Use `_build/agenc` for
non-test usage.

**Important:** Never use `_build/agenc` directly against `_test-env/` by
manually setting AGENC_DIRPATH. Always use `_build/agenc-test` — it ensures
both AGENC_DIRPATH and AGENC_TEST_ENV are set correctly.
```

**Step 2: Update `.claude/settings.json`**

Change `Bash(./agenc:*)` to `Bash(./_build/agenc:*)` and add
`Bash(./_build/agenc-test:*)`.

**Step 3: Commit**

```
git add CLAUDE.md .claude/settings.json
git commit -m "Document test environment and update binary paths in CLAUDE.md"
```

---

Task 8: Update existing `./agenc` references in the codebase
-------------------------------------------------------------

**Files:**
- Modify: `Makefile` (already done in Task 6)
- Verify: no other files reference the old `./agenc` binary path

**Step 1: Search for remaining references**

Run: `grep -rn '\./agenc' --include='*.go' --include='*.md' --include='*.json' --include='Makefile' | grep -v '_build/' | grep -v '_test-env/' | grep -v '.git/'`

Any results that reference `./agenc` as the binary path need to be updated to
`./_build/agenc`. References to the `agenc` CLI name in help text, log messages,
etc. do NOT need to change — only filesystem paths.

**Step 2: Fix any remaining references**

Update each file found in Step 1.

**Step 3: Build and run full check**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Expected: Full build passes (format, vet, lint, tests, compile)

**Step 4: Commit**

```
git add <modified files>
git commit -m "Update remaining binary path references from ./agenc to ./_build/agenc"
```

---

Task 9: End-to-end validation
------------------------------

This is a manual validation task, not code changes.

**Step 1: Full build**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Expected: Passes all checks, produces `_build/agenc` and `_build/agenc-test`

**Step 2: Create test environment**

Run: `make test-env` (with `dangerouslyDisableSandbox: true`)
Expected: Creates `_test-env/` with full directory structure, copies OAuth token

**Step 3: Verify the wrapper script**

Run: `_build/agenc-test version`
Expected: Prints version without error

**Step 4: Start the test server**

Run: `_build/agenc-test server start`
Expected: Server starts, PID file written to `_test-env/server/server.pid`

**Step 5: Create a headless mission**

Run: `_build/agenc-test mission new --headless --blank --prompt "echo hello"`
Expected: Mission created in `_test-env/missions/`

**Step 6: Verify tmux sessions are namespaced**

Run: `tmux list-sessions`
Expected: See `agenc-HASH-pool` (NOT `agenc-pool`). The real `agenc-pool` should
be unaffected.

**Step 7: Verify keybinding injection is blocked**

Run: `_build/agenc-test tmux inject`
Expected: Error: "tmux keybinding injection is disabled in test environments"

**Step 8: Clean up**

Run: `make test-env-clean` (with `dangerouslyDisableSandbox: true`)
Expected: Server killed, tmux sessions killed, `_test-env/` removed

**Step 9: Verify cleanup**

Run: `tmux list-sessions`
Expected: No `agenc-HASH-pool` session. Real `agenc-pool` still exists.

Run: `ls _test-env/`
Expected: "No such file or directory"

**Step 10: Final commit**

If any fixes were needed during validation:
```
git add <files>
git commit -m "Fix issues found during test environment e2e validation"
```

Push everything:
```
git push
```
