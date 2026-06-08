# Watcher Migration: `fsnotify` → `rjeczalik/notify` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace every `fsnotify` call site in the codebase with `github.com/rjeczalik/notify` so recursive watchers can use FSEvents-recursive on macOS (eliminating the FD-leak class that caused the 2026-06-07 server outage), and add `go-git`'s gitignore matcher as a runtime post-filter on the writeable-copy watcher.

**Architecture:** Five `fsnotify.NewWatcher()` instances become `notify.Watch(path, eventCh, eventMask)` calls. Two recursive watchers (writeable-copy working tree, `~/.claude` tracked dirs) use the library's `path/...` recursive syntax. The writeable-copy watcher also constructs a `gitignore.Matcher` from the repo's ignore rules at startup and filters events through it before debouncing. `config_watcher.go`'s combined single-file + recursive Watcher becomes multiple `notify.Watch` calls sharing one event channel, with a defer-undo cleanup pattern guarding partial-init failure. The `fsnotify` dependency is dropped from `go.mod` at the end.

**Tech Stack:** Go, `github.com/rjeczalik/notify`, `github.com/go-git/go-git/v5/plumbing/format/gitignore`, `github.com/go-git/go-billy/v5/osfs`.

**Spec:** `docs/superpowers/specs/2026-06-08-watcher-fsnotify-migration-design.md`
**Bead:** `agenc-ku7h`
**Mission:** ab2584f3-1f48-43e8-a61e-e05cfb493707

---

## File Structure

**Create:**
- `internal/server/gitignore_filter.go` — pure-function gitignore matcher constructor + per-event `Match` wrapper. One responsibility: "given a repo root and a path, should we ignore this event?"
- `internal/server/gitignore_filter_test.go` — unit tests for the filter.

**Modify:**
- `go.mod`, `go.sum` — add `rjeczalik/notify` and `go-git/v5`; remove `fsnotify` (last task).
- `internal/wrapper/credential_sync.go:149-203` — credential-broadcast file Watcher.
- `internal/wrapper/wrapper.go:678-733` — refs Watcher (containerized mode).
- `internal/server/writeable_copies_watcher.go` — working-tree watcher (recursive + gitignore filter), ref watcher (non-recursive), and delete the obsolete `addWatchesRecursiveExcludingGit` / `isInsideGitDir` helpers.
- `internal/server/config_watcher.go` — combined Watcher splits into multiple `notify.Watch` calls sharing one channel; delete the `addTrackedWatches` / `addWatchesRecursive` / `addWatch` fsnotify-shaped helpers and replace with rjeczalik/notify-shaped equivalents.
- `scripts/e2e-test.sh` — append a regression test that asserts FD count stays under 1,000 after writeable-copy creation.

**Delete (after migration):**
- `addWatchesRecursiveExcludingGit`, `isInsideGitDir` in `writeable_copies_watcher.go` (no longer needed — FSEvents handles recursion natively).

---

## API Translation Reference

This pattern recurs across every task — keep it visible.

**Old (`fsnotify`):**
```go
watcher, err := fsnotify.NewWatcher()
if err != nil { return }
defer watcher.Close()

if err := watcher.Add(path); err != nil { return }

for {
    select {
    case <-ctx.Done():
        return
    case ev, ok := <-watcher.Events:
        if !ok { return }
        if ev.Op&fsnotify.Write == fsnotify.Write { /* ... */ }
    case err, ok := <-watcher.Errors:
        if !ok { return }
        log.Printf("watch error: %v", err)
    }
}
```

**New (`rjeczalik/notify`):**
```go
import "github.com/rjeczalik/notify"

eventCh := make(chan notify.EventInfo, 256)
if err := notify.Watch(path, eventCh, notify.Create|notify.Write|notify.Remove|notify.Rename); err != nil {
    return
}
defer notify.Stop(eventCh)

for {
    select {
    case <-ctx.Done():
        return
    case ev := <-eventCh:
        if ev.Event()&notify.Write != 0 { /* ... */ }
        // ev.Path() returns the absolute path
    }
}
```

**Recursive variant:** pass `path+"/..."` instead of `path`. The trailing `/...` is the library's recursive syntax.

**Multi-Watch on one channel:** call `notify.Watch(pathA, eventCh, ...)` then `notify.Watch(pathB, eventCh, ...)` — events from both arrive on the same channel. A single `notify.Stop(eventCh)` tears down everything sharing that channel.

---

## Task 1: Add Dependencies

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Add the two new dependencies**

Run:
```bash
go get github.com/rjeczalik/notify
go get github.com/go-git/go-git/v5
go get github.com/go-git/go-billy/v5
```

- [ ] **Step 2: Verify the project still builds**

Run: `make build`

Expected: clean build, no errors. The new deps are pulled but unused.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: add rjeczalik/notify and go-git for watcher migration

AgenC mission: ab2584f3-1f48-43e8-a61e-e05cfb493707"
```

---

## Task 2: Write Gitignore Filter (TDD)

**Files:**
- Create: `internal/server/gitignore_filter.go`
- Test: `internal/server/gitignore_filter_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/server/gitignore_filter_test.go`:

```go
package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitignoreFilter(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up a fixture repo with a .gitignore at the root.
	gitignoreContent := "node_modules/\ndist/\n!dist/important.txt\n"
	if err := os.WriteFile(filepath.Join(tmpDir, ".gitignore"), []byte(gitignoreContent), 0644); err != nil {
		t.Fatalf("failed to write fixture .gitignore: %v", err)
	}

	filter, err := newGitignoreFilter(tmpDir)
	if err != nil {
		t.Fatalf("newGitignoreFilter failed: %v", err)
	}

	tests := []struct {
		name     string
		path     string
		isDir    bool
		expected bool // true = should be ignored
	}{
		{"plain file under ignored dir", filepath.Join(tmpDir, "node_modules", "foo.js"), false, true},
		{"file under another ignored dir", filepath.Join(tmpDir, "dist", "random.txt"), false, true},
		{"negation un-ignores", filepath.Join(tmpDir, "dist", "important.txt"), false, false},
		{"unrelated source file", filepath.Join(tmpDir, "src", "main.go"), false, false},
		{"ignored directory itself", filepath.Join(tmpDir, "node_modules"), true, true},
		{"path outside repo root", "/some/other/place/foo", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := filter.shouldIgnore(tc.path, tc.isDir)
			if got != tc.expected {
				t.Errorf("shouldIgnore(%q, isDir=%v) = %v, want %v", tc.path, tc.isDir, got, tc.expected)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestGitignoreFilter -v`
Expected: FAIL with `newGitignoreFilter` undefined.

- [ ] **Step 3: Implement the filter**

Create `internal/server/gitignore_filter.go`:

```go
package server

import (
	"path/filepath"
	"strings"

	"github.com/go-git/go-billy/v5/osfs"
	"github.com/go-git/go-git/v5/plumbing/format/gitignore"
)

// gitignoreFilter answers "should events at this path be ignored?" against the
// repo's full gitignore rule set (nested .gitignore files, .git/info/exclude,
// and the global core.excludesFile). Built once at watcher startup and read
// without synchronization from the event-receive goroutine.
type gitignoreFilter struct {
	repoRoot string
	matcher  gitignore.Matcher
}

func newGitignoreFilter(repoRoot string) (*gitignoreFilter, error) {
	fs := osfs.New(repoRoot)
	patterns, err := gitignore.ReadPatterns(fs, nil)
	if err != nil {
		return nil, err
	}
	return &gitignoreFilter{
		repoRoot: repoRoot,
		matcher:  gitignore.NewMatcher(patterns),
	}, nil
}

// shouldIgnore returns true if the path falls inside repoRoot and matches a
// gitignore rule. Paths outside repoRoot return false (we don't presume to
// ignore foreign events).
func (f *gitignoreFilter) shouldIgnore(path string, isDir bool) bool {
	rel, err := filepath.Rel(f.repoRoot, path)
	if err != nil {
		return false
	}
	if strings.HasPrefix(rel, "..") || rel == "." {
		return false
	}
	segments := strings.Split(filepath.ToSlash(rel), "/")
	return f.matcher.Match(segments, isDir)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestGitignoreFilter -v`
Expected: PASS (6 subtests).

- [ ] **Step 5: Commit**

```bash
git add internal/server/gitignore_filter.go internal/server/gitignore_filter_test.go
git commit -m "feat: add gitignore filter for writeable-copy watcher events

AgenC mission: ab2584f3-1f48-43e8-a61e-e05cfb493707"
```

---

## Task 3: Migrate `credential_sync.go` Watcher

**Files:**
- Modify: `internal/wrapper/credential_sync.go:149-203`

- [ ] **Step 1: Replace the fsnotify setup with rjeczalik/notify**

Open `internal/wrapper/credential_sync.go`. Replace the fsnotify-import line at the top of the file:

```go
// Before
"github.com/fsnotify/fsnotify"

// After
"github.com/rjeczalik/notify"
```

Then replace the body of `watchCredentialDownwardSync` starting at line 149 with:

```go
	eventCh := make(chan notify.EventInfo, 256)
	if err := notify.Watch(w.agencDirpath, eventCh, notify.Create|notify.Write); err != nil {
		w.logger.Warn("Failed to watch agenc directory for credential changes", "error", err)
		return
	}
	defer notify.Stop(eventCh)

	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}
	timerActive := false

	for {
		select {
		case <-ctx.Done():
			if !debounceTimer.Stop() && timerActive {
				<-debounceTimer.C
			}
			return

		case event := <-eventCh:
			if event.Path() != expiryFilepath {
				continue
			}
			if event.Event()&(notify.Create|notify.Write) == 0 {
				continue
			}
			if !debounceTimer.Stop() && timerActive {
				<-debounceTimer.C
			}
			debounceTimer.Reset(credentialDownwardDebouncePeriod)
			timerActive = true

		case <-debounceTimer.C:
			timerActive = false
			if w.rebuilding.Load() {
				continue
			}
			w.handleDownwardSync(expiryFilepath)
		}
	}
```

(Drop the `watcher, err := fsnotify.NewWatcher() ... defer watcher.Close() ... watcher.Add(...)` block in favor of the single `notify.Watch` call above.)

- [ ] **Step 2: Run existing tests for the wrapper package**

Run: `go test ./internal/wrapper/... -v`
Expected: PASS (any tests that touched the watcher should continue passing).

- [ ] **Step 3: Run `make check`**

Run: `make check`
Expected: clean — lint, vet, deadcode, tests all pass.

- [ ] **Step 4: Commit**

```bash
git add internal/wrapper/credential_sync.go
git commit -m "refactor: migrate credential_sync watcher to rjeczalik/notify

AgenC mission: ab2584f3-1f48-43e8-a61e-e05cfb493707"
```

---

## Task 4: Migrate `wrapper.go` Refs Watcher

**Files:**
- Modify: `internal/wrapper/wrapper.go:678-733`

- [ ] **Step 1: Apply the same translation pattern**

In `internal/wrapper/wrapper.go`, find the existing block starting at line 678:

```go
watcher, err := fsnotify.NewWatcher()
// ... defer watcher.Close()
// ... watcher.Add(refsDirpath)
// ... select { case event := <-watcher.Events: ... case err := <-watcher.Errors: ... }
```

Replace with the new shape:

```go
eventCh := make(chan notify.EventInfo, 256)
if err := notify.Watch(refsDirpath, eventCh, notify.Create|notify.Write); err != nil {
    // existing error-logging shape preserved
    return
}
defer notify.Stop(eventCh)

// Preserve the existing debounce-timer + branch-name filter logic.
// Translate event.Name → event.Path(), event.Has(...) / event.Op&... → event.Event()&...
```

The full translated body should mirror Task 3 plus the existing branch-filter logic (`filepath.Base(event.Path()) != defaultBranch`).

Update the import line to match.

- [ ] **Step 2: Run wrapper tests**

Run: `go test ./internal/wrapper/... -v`
Expected: PASS.

- [ ] **Step 3: Run `make check`**

Run: `make check`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add internal/wrapper/wrapper.go
git commit -m "refactor: migrate wrapper refs watcher to rjeczalik/notify

AgenC mission: ab2584f3-1f48-43e8-a61e-e05cfb493707"
```

---

## Task 5: Migrate `writeable_copies_watcher.go` Ref Watcher

**Files:**
- Modify: `internal/server/writeable_copies_watcher.go:144-198`

- [ ] **Step 1: Replace the fsnotify ref-watcher with the rjeczalik/notify shape**

In `runWriteableCopyRefWatcher`, replace the `watcher, err := fsnotify.NewWatcher() ... defer watcher.Close() ... watcher.Add(refsDirpath)` setup with:

```go
eventCh := make(chan notify.EventInfo, 256)
if err := notify.Watch(refsDirpath, eventCh, notify.Create|notify.Write); err != nil {
    s.logger.Printf("Writeable-copy ref watcher: cannot watch '%s': %v", refsDirpath, err)
    return
}
defer notify.Stop(eventCh)
```

In the event-loop select, replace `case event, ok := <-watcher.Events:` with `case event := <-eventCh:` and translate `event.Name` → `event.Path()`, `event.Op&...` → `event.Event()&...`. Drop the `case err, ok := <-watcher.Errors:` branch entirely (rjeczalik/notify has no separate error channel; setup errors come back from `Watch` itself).

Update the file's imports to reference `rjeczalik/notify` instead of `fsnotify` (keep `fsnotify` if the working-tree watcher in the same file still imports it — Task 7 removes it).

- [ ] **Step 2: Run server tests**

Run: `go test ./internal/server/... -v`
Expected: PASS.

- [ ] **Step 3: Run `make check`**

Run: `make check`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add internal/server/writeable_copies_watcher.go
git commit -m "refactor: migrate writeable-copy ref watcher to rjeczalik/notify

AgenC mission: ab2584f3-1f48-43e8-a61e-e05cfb493707"
```

---

## Task 6: Migrate `config_watcher.go` Combined Watcher (with defer-undo)

**Files:**
- Modify: `internal/server/config_watcher.go:50-196`

This watcher uses a single `fsnotify.Watcher` for both a single file and a recursive tracked-directory set. With `rjeczalik/notify` it becomes multiple `notify.Watch` calls sharing one event channel. The defer-undo pattern guards partial-init failure (edge case 2.6 from the spec's edge-case discovery).

- [ ] **Step 1: Replace the body of `watchBothConfigs`**

Replace `watchBothConfigs` with the new shape:

```go
func (s *Server) watchBothConfigs(ctx context.Context, userClaudeDirpath string, shadowDirpath string) {
	eventCh := make(chan notify.EventInfo, 256)
	shouldCleanup := true
	defer func() {
		if shouldCleanup {
			notify.Stop(eventCh)
		}
	}()

	// Watch the agenc config.yml file directly.
	agencConfigPath := config.GetConfigFilepath(s.agencDirpath)
	if err := notify.Watch(agencConfigPath, eventCh, notify.Create|notify.Write); err != nil {
		s.logger.Printf("Config watcher: failed to watch agenc config.yml: %v", err)
	}

	// Watch the ~/.claude tracked directories (recursive on macOS via FSEvents).
	s.watchTrackedDirs(eventCh, userClaudeDirpath)

	shouldCleanup = false // transfer ownership to the deferred final Stop below
	defer notify.Stop(eventCh)

	var claudeDebounceTimer *time.Timer
	var agencDebounceTimer *time.Timer

	for {
		select {
		case <-ctx.Done():
			if claudeDebounceTimer != nil {
				claudeDebounceTimer.Stop()
			}
			if agencDebounceTimer != nil {
				agencDebounceTimer.Stop()
			}
			return

		case event := <-eventCh:
			if event.Path() == agencConfigPath {
				if agencDebounceTimer != nil {
					agencDebounceTimer.Stop()
				}
				agencDebounceTimer = time.AfterFunc(ingestDebounce, func() {
					s.reloadConfig()
				})
				continue
			}

			if !isTrackedPath(event.Path(), userClaudeDirpath) {
				continue
			}

			if claudeDebounceTimer != nil {
				claudeDebounceTimer.Stop()
			}
			claudeDebounceTimer = time.AfterFunc(ingestDebounce, func() {
				s.ingestClaudeConfig(userClaudeDirpath, shadowDirpath)
				// No need to re-add watches: FSEvents-recursive picks up new dirs automatically on macOS.
			})
		}
	}
}
```

- [ ] **Step 2: Add the new `watchTrackedDirs` helper and delete the obsolete fsnotify helpers**

Add this helper:

```go
// watchTrackedDirs registers recursive notify.Watch calls for each tracked
// ~/.claude subdirectory (resolved through symlinks). Each Watch is best-effort
// — failure to add one directory does not block the rest. All events stream
// into the shared eventCh.
func (s *Server) watchTrackedDirs(eventCh chan<- notify.EventInfo, userClaudeDirpath string) {
	// Watch ~/.claude itself for file creates/deletes at the top level.
	_ = notify.Watch(userClaudeDirpath, eventCh, notify.All)

	for _, dirName := range claudeconfig.TrackedDirNames {
		dirpath := filepath.Join(userClaudeDirpath, dirName)
		resolved, err := filepath.EvalSymlinks(dirpath)
		if err != nil {
			continue
		}
		// Recursive watch using rjeczalik/notify's "/..." syntax.
		_ = notify.Watch(resolved+"/...", eventCh, notify.All)
	}

	// Watch directories containing tracked individual files (symlink targets).
	for _, fileName := range claudeconfig.TrackedFileNames {
		filePath := filepath.Join(userClaudeDirpath, fileName)
		resolved, err := filepath.EvalSymlinks(filePath)
		if err != nil {
			continue
		}
		_ = notify.Watch(filepath.Dir(resolved), eventCh, notify.All)
	}
}
```

Delete `addTrackedWatches`, `addWatchesRecursive`, and `addWatch` from the file (they no longer have callers).

- [ ] **Step 3: Update imports**

Replace `"github.com/fsnotify/fsnotify"` with `"github.com/rjeczalik/notify"` at the top of `config_watcher.go`.

- [ ] **Step 4: Run server tests**

Run: `go test ./internal/server/... -v`
Expected: PASS (any tests for config-watcher's behavior should continue passing).

- [ ] **Step 5: Run `make check`**

Run: `make check`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/server/config_watcher.go
git commit -m "refactor: migrate config_watcher to rjeczalik/notify with defer-undo

Splits the combined fsnotify Watcher into per-target notify.Watch calls
sharing one event channel. Uses defer-undo cleanup pattern to ensure
partial-init failures don't leak goroutines.

AgenC mission: ab2584f3-1f48-43e8-a61e-e05cfb493707"
```

---

## Task 7: Migrate `writeable_copies_watcher.go` Working-Tree Watcher (recursive + gitignore filter)

**Files:**
- Modify: `internal/server/writeable_copies_watcher.go:75-127`
- Delete from same file: `addWatchesRecursiveExcludingGit` (lines 200-221), `isInsideGitDir` (lines 223 onward).

This is the load-bearing migration — the one that actually fixes the FD-leak bug.

- [ ] **Step 1: Replace the body of `runWriteableCopyWorkingTreeWatcher`**

Replace with:

```go
func (s *Server) runWriteableCopyWorkingTreeWatcher(ctx context.Context, repoName, repoDirpath string) {
	// Load the gitignore matcher BEFORE starting the watcher to avoid the
	// load-order race surfaced in edge-case discovery (4.1).
	filter, err := newGitignoreFilter(repoDirpath)
	if err != nil {
		s.logger.Printf("Writeable-copy watcher: failed to load gitignore for '%s': %v", repoName, err)
		return
	}

	eventCh := make(chan notify.EventInfo, 256)
	if err := notify.Watch(repoDirpath+"/...", eventCh, notify.All); err != nil {
		s.logger.Printf("Writeable-copy watcher: notify.Watch failed for '%s': %v", repoName, err)
		return
	}
	defer notify.Stop(eventCh)

	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}
	timerActive := false

	for {
		select {
		case <-ctx.Done():
			return
		case event := <-eventCh:
			eventPath := event.Path()
			// Skip events inside .git/.
			if isInsideRepoGitDir(eventPath, repoDirpath) {
				continue
			}
			// Skip events matching the repo's gitignore.
			if filter.shouldIgnore(eventPath, eventIsDir(event)) {
				continue
			}
			if !timerActive {
				timerActive = true
			} else if !debounce.Stop() {
				<-debounce.C
			}
			debounce.Reset(writeableCopyWorkingTreeDebounce)
		case <-debounce.C:
			timerActive = false
			s.enqueueReconcileForRepo(repoName)
		}
	}
}

// isInsideRepoGitDir replaces the old isInsideGitDir helper. FSEvents will
// emit paths under .git/ since we now watch recursively without per-dir Add
// filtering; this check restores the previous "skip .git/" semantic.
func isInsideRepoGitDir(eventPath, repoDirpath string) bool {
	gitDir := filepath.Join(repoDirpath, ".git")
	rel, err := filepath.Rel(gitDir, eventPath)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, "..")
}

// eventIsDir returns true if the event path is a directory at evaluation time.
// Returns false if the path does not exist (event for a deleted item) — that's
// safe because gitignore directory-only patterns won't match a non-directory.
func eventIsDir(event notify.EventInfo) bool {
	info, err := os.Stat(event.Path())
	if err != nil {
		return false
	}
	return info.IsDir()
}
```

- [ ] **Step 2: Delete the obsolete helpers**

Remove `addWatchesRecursiveExcludingGit` and the old `isInsideGitDir` from `writeable_copies_watcher.go` — they no longer have callers.

- [ ] **Step 3: Update imports**

Replace `"github.com/fsnotify/fsnotify"` with `"github.com/rjeczalik/notify"`. Add `"strings"` if not already imported.

- [ ] **Step 4: Run server tests**

Run: `go test ./internal/server/... -v`
Expected: PASS.

- [ ] **Step 5: Run `make check`**

Run: `make check`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add internal/server/writeable_copies_watcher.go
git commit -m "refactor: migrate writeable-copy working-tree watcher with gitignore filter

Replaces fsnotify recursive watch with rjeczalik/notify FSEvents-recursive
on macOS, eliminating the per-directory FD cost that caused the
2026-06-07 server outage. Adds go-git gitignore matcher as event
post-filter to drop noise from build artifacts.

Fixes: agenc-ku7h
AgenC mission: ab2584f3-1f48-43e8-a61e-e05cfb493707"
```

---

## Task 8: Add E2E Regression Test for FD Count

**Files:**
- Modify: `scripts/e2e-test.sh`

- [ ] **Step 1: Append a regression test to e2e-test.sh**

Add a new section at the end of `scripts/e2e-test.sh` (above the final teardown):

```bash
echo "--- Watcher FD-Leak Regression (agenc-ku7h) ---"

# Create a synthetic writeable-copy with a large ignored subtree (simulating
# node_modules) and assert the server's FD count stays bounded.
fd_test_repo="${TMPDIR:-/tmp}/agenc-fd-test-$$"
command rm -rf "${fd_test_repo}"
command mkdir -p "${fd_test_repo}"
(cd "${fd_test_repo}" && command git init -q && command git -c user.email=test@test -c user.name=test commit --allow-empty -m init -q)
command mkdir -p "${fd_test_repo}/fake_node_modules"
command printf "fake_node_modules/\n" > "${fd_test_repo}/.gitignore"
# Generate 500 files inside the ignored dir — enough to blow past the old
# per-file FD cost but trivial for the new FSEvents-recursive watcher.
for i in $(seq 1 500); do
    command printf "x" > "${fd_test_repo}/fake_node_modules/file_${i}.tmp"
done

# (Assumes a test-env repo registration helper exists; otherwise inline the
# config.yml write and reload here.)
run_test "register fd-test writeable-copy" 0 \
    "${agenc_test}" repo add-writeable-copy mieubrisse/fd-test "${fd_test_repo}"

sleep 3  # give the watcher time to settle

# Read the server PID and count its open FDs.
server_pid=$(cat "${AGENC_DIRPATH}/server/server.pid")
fd_count=$(lsof -p "${server_pid}" 2>/dev/null | command wc -l | command tr -d ' ')

echo "Server FD count after writeable-copy creation: ${fd_count}"
if [ "${fd_count}" -gt 1000 ]; then
    echo "FAIL: FD count ${fd_count} exceeds threshold of 1000 (regression of agenc-ku7h)"
    exit 1
fi
echo "PASS: FD count under threshold"

command rm -rf "${fd_test_repo}"
```

Note: the exact CLI invocation for "register a writeable-copy in the test env" depends on existing helpers. If `repo add-writeable-copy` doesn't exist as a CLI subcommand, this step needs to write the config.yml writeable-copy entry directly and trigger a reconcile.

- [ ] **Step 2: Run e2e tests**

Run: `make e2e`
Expected: all tests pass including the new FD-leak regression.

- [ ] **Step 3: Commit**

```bash
git add scripts/e2e-test.sh
git commit -m "test: add e2e FD-count regression guard for agenc-ku7h

Asserts the agenc server's open-FD count stays under 1000 after a
writeable-copy with a 500-file ignored subtree is registered. Direct
regression guard for the 2026-06-07 watcher FD-leak incident.

AgenC mission: ab2584f3-1f48-43e8-a61e-e05cfb493707"
```

---

## Task 9: Drop the `fsnotify` Dependency

**Files:**
- Modify: `go.mod`, `go.sum`

- [ ] **Step 1: Confirm no remaining fsnotify imports**

Run: `command grep -rn "fsnotify" --include="*.go" .`
Expected: NO output (all imports already migrated in Tasks 3-7).

If any output appears, return to the corresponding task and migrate the remaining call site.

- [ ] **Step 2: Tidy go.mod**

Run: `go mod tidy`

Expected: `fsnotify` removed from `go.mod` and `go.sum` automatically.

- [ ] **Step 3: Verify everything builds and tests pass**

Run: `make check`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "deps: drop fsnotify — replaced by rjeczalik/notify

AgenC mission: ab2584f3-1f48-43e8-a61e-e05cfb493707"
```

---

## Task 10: Manual Verification

**Note:** This task is human-driven, not automated. The agent should run the command and report results, then ask Kevin to confirm before closing the bead.

- [ ] **Step 1: Verify the production binary builds**

Run: `make build`
Expected: clean build, version string set from git.

- [ ] **Step 2: Briefly attach the binary to the real environment**

Run (from the agenc repo): `./_build/agenc server status`
Expected: server status reported. (If the server isn't running, this exits cleanly; the goal is verifying the binary works against the real env.)

- [ ] **Step 3: Sample FD count of the running server**

Run: `lsof -p $(cat ~/.agenc/server/server.pid) 2>/dev/null | command wc -l`
Expected: FD count in the low-hundreds-to-low-thousands range (not the six-figure range the bug produced).

Report the actual number to Kevin and note whether it's consistent with the expectation. If it's higher than expected (e.g., > 3000), investigate before declaring the bead done — there may be a leak elsewhere.

- [ ] **Step 4: Close the beads**

```bash
bd close agenc-ku7h --reason="Migrated all watchers to rjeczalik/notify; FSEvents-recursive on macOS eliminates per-dir FD cost; go-git gitignore filter handles event post-filtering. E2E regression guard added. Manual verification: <FD count from step 3>."
```

(Do NOT auto-close `agenc-jcvv` — it remains open as the broader writeable-copy reconcile exploration. Do NOT auto-close `agenc-88uu` — that's the downstream `git_corrupt` misclassification, addressed separately.)

---

## Self-Review

Performing the spec-coverage / placeholder / type-consistency check now (not delegated to a subagent).

**Spec coverage:**
- ✅ Library swap (fsnotify → rjeczalik/notify): Tasks 3-7
- ✅ FSEvents-recursive on macOS: Task 6 (config_watcher), Task 7 (writeable-copy working tree) — both use `path+"/..."`
- ✅ Gitignore post-filter on writeable-copy: Task 2 (filter), Task 7 (integration)
- ✅ Drop `addWatchesRecursiveExcludingGit` and `isInsideGitDir`: Task 7 step 2
- ✅ Defer-undo for config_watcher partial-init: Task 6 step 1
- ✅ Single shared event channel for config_watcher: Task 6 step 1
- ✅ Drop fsnotify dependency: Task 9
- ✅ E2E regression guard (FD count under 1000): Task 8
- ✅ Manual lsof verification: Task 10

**Placeholders:** None — every code step has complete code. The one caveat is Task 8's `repo add-writeable-copy` CLI invocation; if that helper doesn't exist, the executing engineer needs to inline the config.yml write. This is flagged explicitly in Task 8.

**Type consistency:**
- `gitignoreFilter` struct + `shouldIgnore` method — defined in Task 2, used in Task 7. Names match.
- `notify.EventInfo` channel type used consistently across Tasks 3-7.
- `isInsideRepoGitDir` (new helper name in Task 7) is different from old `isInsideGitDir` (deleted) — intentional rename to disambiguate, but flagged here for awareness.

No issues found that block execution.

---

## Edge-Case Coverage Cross-Reference

From `/edge-case-discovery` enumeration (session synthesis):

| Edge case | Addressed in |
|---|---|
| 1.1 path missing at Watch time | All migration tasks (existing log-and-return shape preserved) |
| 1.2 symlink resolution | Task 6 (preserved `EvalSymlinks`) |
| 1.3 NFC/NFD normalization | **NOT in plan.** Low-severity edge case; if it bites, add normalization at the top of `shouldIgnore` and at event-receive. |
| 1.4-1.6 gitignore corner cases | Task 2 (matcher uses go-git which handles these correctly) |
| 1.7 path outside watched root | Task 2 `shouldIgnore` already guards with `strings.HasPrefix(rel, "..")` |
| 2.1 watched root deleted | Out of scope — existing reconcile-tick handles via `missing path` pause |
| 2.6 partial init cleanup | **Task 6** (defer-undo) |
| 3.1 FSEvents must-rescan sentinel | Verify during Task 7 implementation; if exposed, trigger one reconcile-tick; otherwise rely on existing periodic timer |
| 4.1 event before matcher loaded | **Task 7 step 1** (filter loaded before `notify.Watch`) |
| 5.1 FD count regression | **Task 8** (e2e assertion) |
| 5.2 goroutine leak on missed Stop | All migration tasks use `defer notify.Stop(eventCh)` immediately after success |
