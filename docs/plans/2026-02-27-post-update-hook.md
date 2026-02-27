# Post-Update Hook Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a per-repo `postUpdateHook` config field and centralize all repo force-pull operations into a single worker goroutine that runs the hook after each successful update.

**Architecture:** A new `repoUpdateRequest` channel on Server feeds a single worker goroutine that owns all `ForceUpdateRepo` calls. Both the cron ticker and push handler enqueue requests instead of calling git directly. After each update where HEAD changes, the worker reads config and runs the hook via `sh -c`. 30-minute hard timeout with WARN logs after 5 minutes.

**Tech Stack:** Go, Cobra CLI, `os/exec`, `sh -c`

**Design doc:** `docs/plans/2026-02-27-post-update-hook-design.md`

---

### Task 1: Add PostUpdateHook field to RepoConfig

**Files:**
- Modify: `internal/config/agenc_config.go:302-310` (RepoConfig struct)
- Test: `internal/config/agenc_config_test.go`

**Step 1: Write the failing test**

Add to `internal/config/agenc_config_test.go`:

```go
func TestPostUpdateHook_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &AgencConfig{
		RepoConfigs: map[string]RepoConfig{
			"github.com/owner/repo1": {PostUpdateHook: "make setup"},
			"github.com/owner/repo2": {},
		},
	}

	if err := WriteAgencConfig(tmpDir, cfg, nil); err != nil {
		t.Fatalf("WriteAgencConfig failed: %v", err)
	}

	got, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	rc1 := got.RepoConfigs["github.com/owner/repo1"]
	if rc1.PostUpdateHook != "make setup" {
		t.Errorf("expected repo1 postUpdateHook 'make setup', got '%s'", rc1.PostUpdateHook)
	}

	rc2 := got.RepoConfigs["github.com/owner/repo2"]
	if rc2.PostUpdateHook != "" {
		t.Errorf("expected empty postUpdateHook for repo2, got '%s'", rc2.PostUpdateHook)
	}
}

func TestPostUpdateHook_FromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	yamlContent := `
repoConfig:
  github.com/owner/repo:
    alwaysSynced: true
    postUpdateHook: "npm install && npm run build"
`
	configFilepath := filepath.Join(configDirpath, ConfigFilename)
	if err := os.WriteFile(configFilepath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	rc := cfg.RepoConfigs["github.com/owner/repo"]
	if !rc.AlwaysSynced {
		t.Error("expected alwaysSynced=true")
	}
	if rc.PostUpdateHook != "npm install && npm run build" {
		t.Errorf("expected postUpdateHook 'npm install && npm run build', got '%s'", rc.PostUpdateHook)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestPostUpdateHook -v`
Expected: FAIL — `PostUpdateHook` field doesn't exist on `RepoConfig`

**Step 3: Write minimal implementation**

In `internal/config/agenc_config.go`, add the field to `RepoConfig` (around line 309):

```go
type RepoConfig struct {
	AlwaysSynced      bool               `yaml:"alwaysSynced,omitempty"`
	WindowTitle       string             `yaml:"windowTitle,omitempty"`
	TrustedMcpServers *TrustedMcpServers `yaml:"trustedMcpServers,omitempty"`
	DefaultModel      string             `yaml:"defaultModel,omitempty"`
	PostUpdateHook    string             `yaml:"postUpdateHook,omitempty"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestPostUpdateHook -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/agenc_config.go internal/config/agenc_config_test.go
git commit -m "Add PostUpdateHook field to RepoConfig"
```

---

### Task 2: Add --post-update-hook CLI flag

**Files:**
- Modify: `cmd/command_str_consts.go:114-118` (flag constants)
- Modify: `cmd/config_repo_config_set.go` (flag registration + handling)

**Step 1: Add the flag constant**

In `cmd/command_str_consts.go`, add after line 118 (the `repoConfigDefaultModelFlagName` line):

```go
repoConfigPostUpdateHookFlagName = "post-update-hook"
```

**Step 2: Register the flag**

In `cmd/config_repo_config_set.go` `init()` function, add after the `defaultModel` flag (around line 35):

```go
configRepoConfigSetCmd.Flags().String(repoConfigPostUpdateHookFlagName, "", `shell command to run after repo updates (e.g., "make setup"); empty to clear`)
```

**Step 3: Update the "at least one flag" check**

In `runConfigRepoConfigSet`, add the new flag to the Changed check and error message. Around line 48-52:

```go
postUpdateHookChanged := cmd.Flags().Changed(repoConfigPostUpdateHookFlagName)

if !alwaysSyncedChanged && !windowTitleChanged && !trustedChanged && !defaultModelChanged && !postUpdateHookChanged {
	return stacktrace.NewError("at least one of --%s, --%s, --%s, --%s, or --%s must be provided",
		repoConfigAlwaysSyncedFlagName, repoConfigWindowTitleFlagName, repoConfigTrustedMcpServersFlagName, repoConfigDefaultModelFlagName, repoConfigPostUpdateHookFlagName)
}
```

**Step 4: Handle the flag value**

After the `defaultModelChanged` block (after line 108), add:

```go
if postUpdateHookChanged {
	hook, err := cmd.Flags().GetString(repoConfigPostUpdateHookFlagName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigPostUpdateHookFlagName)
	}
	rc.PostUpdateHook = hook
}
```

**Step 5: Also update the command's Long help text**

Add an example line to the Long help string showing the new flag:

```
  agenc config repoConfig set github.com/owner/repo --post-update-hook="make setup"
```

**Step 6: Run all cmd tests**

Run: `go test ./cmd/ -v`
Expected: PASS (existing tests shouldn't break)

**Step 7: Commit**

```bash
git add cmd/command_str_consts.go cmd/config_repo_config_set.go
git commit -m "Add --post-update-hook CLI flag to config repoConfig set"
```

---

### Task 3: Add GetHEAD helper function

**Files:**
- Modify: `internal/mission/repo.go`
- Test: `internal/mission/repo_test.go`

**Step 1: Write the failing test**

Add to `internal/mission/repo_test.go`:

```go
func TestGetHEAD(t *testing.T) {
	// Create a temp git repo with a commit
	tmpDir := t.TempDir()
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s: %v", args, output, err)
		}
	}

	runGit("init")
	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "Test")

	// Write a file and commit
	if err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "file.txt")
	runGit("commit", "-m", "initial commit")

	head, err := GetHEAD(tmpDir)
	if err != nil {
		t.Fatalf("GetHEAD failed: %v", err)
	}

	if len(head) != 40 {
		t.Errorf("expected 40-char SHA, got %d chars: %s", len(head), head)
	}

	// Second call should return same value (no changes)
	head2, err := GetHEAD(tmpDir)
	if err != nil {
		t.Fatalf("second GetHEAD failed: %v", err)
	}
	if head != head2 {
		t.Errorf("expected same HEAD, got %s then %s", head, head2)
	}
}

func TestGetHEAD_InvalidRepo(t *testing.T) {
	tmpDir := t.TempDir()
	_, err := GetHEAD(tmpDir)
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/mission/ -run TestGetHEAD -v`
Expected: FAIL — `GetHEAD` doesn't exist

**Step 3: Write minimal implementation**

Add to `internal/mission/repo.go`:

```go
// GetHEAD returns the current HEAD commit SHA for a repository.
// Returns an empty string and an error if the repo has no commits or is invalid.
func GetHEAD(repoDirpath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitOperationTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = repoDirpath
	output, err := cmd.Output()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to get HEAD for '%s'", repoDirpath)
	}
	return strings.TrimSpace(string(output)), nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/mission/ -run TestGetHEAD -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/mission/repo.go internal/mission/repo_test.go
git commit -m "Add GetHEAD helper to mission package"
```

---

### Task 4: Create the repo update worker and channel

This is the core architectural change. The worker goroutine owns all repo force-pull operations.

**Files:**
- Create: `internal/server/repo_update_worker.go`
- Create: `internal/server/repo_update_worker_test.go`
- Modify: `internal/server/server.go:20-31` (add channel + field to Server struct)

**Step 1: Write the failing test for the worker**

Create `internal/server/repo_update_worker_test.go`:

```go
package server

import (
	"bytes"
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/config"
)

// createTestGitRepo creates a temp git repo with one commit and a remote "origin".
// Returns the repo dirpath.
func createTestGitRepo(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	runGit := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = tmpDir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %s: %v", args, output, err)
		}
	}

	runGit("init")
	runGit("config", "user.email", "test@test.com")
	runGit("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(tmpDir, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit("add", "file.txt")
	runGit("commit", "-m", "initial commit")

	return tmpDir
}

func TestRunPostUpdateHook_Success(t *testing.T) {
	tmpDir := t.TempDir()
	markerFilepath := filepath.Join(tmpDir, "hook-ran")

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	ctx := context.Background()
	hookCmd := "touch " + markerFilepath
	runPostUpdateHook(ctx, logger, "test/repo", tmpDir, hookCmd)

	// Verify the hook ran
	if _, err := os.Stat(markerFilepath); os.IsNotExist(err) {
		t.Error("expected hook to create marker file")
	}

	// Verify success log
	logOutput := buf.String()
	if !strings.Contains(logOutput, "succeeded") {
		t.Errorf("expected success log, got: %s", logOutput)
	}
}

func TestRunPostUpdateHook_Failure(t *testing.T) {
	tmpDir := t.TempDir()

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	ctx := context.Background()
	hookCmd := "exit 1"
	runPostUpdateHook(ctx, logger, "test/repo", tmpDir, hookCmd)

	// Verify failure log (should not panic or return error)
	logOutput := buf.String()
	if !strings.Contains(logOutput, "failed") {
		t.Errorf("expected failure log, got: %s", logOutput)
	}
}

func TestRunPostUpdateHook_WorkingDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	markerFilepath := filepath.Join(tmpDir, "pwd-output")

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	ctx := context.Background()
	// Write pwd to a file — should be tmpDir
	hookCmd := "pwd > " + markerFilepath
	runPostUpdateHook(ctx, logger, "test/repo", tmpDir, hookCmd)

	data, err := os.ReadFile(markerFilepath)
	if err != nil {
		t.Fatalf("failed to read pwd output: %v", err)
	}
	got := strings.TrimSpace(string(data))
	if got != tmpDir {
		t.Errorf("expected working dir %s, got %s", tmpDir, got)
	}
}

func TestRunPostUpdateHook_Timeout(t *testing.T) {
	tmpDir := t.TempDir()

	var buf bytes.Buffer
	logger := log.New(&buf, "", 0)

	// Use a short-lived context to simulate timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	hookCmd := "sleep 10"
	runPostUpdateHook(ctx, logger, "test/repo", tmpDir, hookCmd)

	logOutput := buf.String()
	if !strings.Contains(logOutput, "failed") {
		t.Errorf("expected failure log for timeout, got: %s", logOutput)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestRunPostUpdateHook -v`
Expected: FAIL — `runPostUpdateHook` doesn't exist

**Step 3: Implement the worker**

Create `internal/server/repo_update_worker.go`:

```go
package server

import (
	"bytes"
	"context"
	"log"
	"os/exec"
	"time"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

const (
	// postUpdateHookTimeout is the hard timeout for hook execution.
	postUpdateHookTimeout = 30 * time.Minute

	// postUpdateHookWarnInterval is how often to log a warning when a hook
	// runs longer than postUpdateHookWarnThreshold.
	postUpdateHookWarnThreshold = 5 * time.Minute
	postUpdateHookWarnInterval  = 5 * time.Minute

	// repoUpdateChannelSize is the buffer size for the update request channel.
	repoUpdateChannelSize = 64
)

// repoUpdateRequest represents a request to force-update a repo library clone.
type repoUpdateRequest struct {
	repoName             string
	refreshDefaultBranch bool
	forceRunHook         bool
}

// runRepoUpdateWorker reads update requests from the channel and processes
// them sequentially. Each request force-pulls the repo and runs the
// postUpdateHook if HEAD changed (or forceRunHook is set).
func (s *Server) runRepoUpdateWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.repoUpdateCh:
			s.processRepoUpdate(ctx, req)
		}
	}
}

// processRepoUpdate handles a single repo update request.
func (s *Server) processRepoUpdate(ctx context.Context, req repoUpdateRequest) {
	repoDirpath := config.GetRepoDirpath(s.agencDirpath, req.repoName)

	// Periodically refresh origin/HEAD so we track the remote's default branch
	if req.refreshDefaultBranch {
		setHeadCmd := exec.CommandContext(ctx, "git", "remote", "set-head", "origin", "--auto")
		setHeadCmd.Dir = repoDirpath
		if output, err := setHeadCmd.CombinedOutput(); err != nil {
			s.logger.Printf("Repo update: git remote set-head failed for '%s': %v\n%s", req.repoName, err, string(output))
		}
	}

	// Capture HEAD before update
	headBefore, _ := mission.GetHEAD(repoDirpath)

	if err := mission.ForceUpdateRepo(repoDirpath); err != nil {
		s.logger.Printf("Repo update: failed to update '%s': %v", req.repoName, err)
		return
	}

	// Capture HEAD after update
	headAfter, _ := mission.GetHEAD(repoDirpath)

	// Run hook if HEAD changed or forceRunHook is set
	headChanged := headBefore != headAfter && headAfter != ""
	if headChanged || req.forceRunHook {
		cfg, _, err := config.ReadAgencConfig(s.agencDirpath)
		if err != nil {
			s.logger.Printf("Repo update: failed to read config for hook: %v", err)
			return
		}
		rc, ok := cfg.GetRepoConfig(req.repoName)
		if ok && rc.PostUpdateHook != "" {
			if headChanged {
				s.logger.Printf("Repo update: HEAD changed for '%s' (%s -> %s), running postUpdateHook",
					req.repoName, abbreviateSHA(headBefore), abbreviateSHA(headAfter))
			} else {
				s.logger.Printf("Repo update: running postUpdateHook for '%s' (first clone)", req.repoName)
			}
			hookCtx, hookCancel := context.WithTimeout(ctx, postUpdateHookTimeout)
			defer hookCancel()
			runPostUpdateHook(hookCtx, s.logger, req.repoName, repoDirpath, rc.PostUpdateHook)
		}
	}
}

// runPostUpdateHook executes a shell command in the repo directory. It logs
// success or failure but never returns an error — hook failures are non-fatal.
func runPostUpdateHook(ctx context.Context, logger *log.Logger, repoName string, repoDirpath string, hookCmd string) {
	cmd := exec.CommandContext(ctx, "sh", "-c", hookCmd)
	cmd.Dir = repoDirpath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start a goroutine to emit warnings if the hook runs too long
	done := make(chan struct{})
	go func() {
		timer := time.NewTimer(postUpdateHookWarnThreshold)
		defer timer.Stop()
		for {
			select {
			case <-done:
				return
			case <-timer.C:
				logger.Printf("Repo update: WARN postUpdateHook for '%s' has been running for >%v, still waiting...",
					repoName, postUpdateHookWarnThreshold)
				timer.Reset(postUpdateHookWarnInterval)
			}
		}
	}()

	err := cmd.Run()
	close(done)

	if err != nil {
		logger.Printf("Repo update: postUpdateHook failed for '%s': %v\nstderr: %s",
			repoName, err, strings.TrimSpace(stderr.String()))
	} else {
		logger.Printf("Repo update: postUpdateHook succeeded for '%s'", repoName)
	}
}

// abbreviateSHA returns the first 8 characters of a git SHA, or the full
// string if shorter.
func abbreviateSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
```

Note: You will need to add `"strings"` to the import block.

**Step 4: Add the channel and field to Server struct**

In `internal/server/server.go`, add to the Server struct (around line 31):

```go
// Repo update worker
repoUpdateCh chan repoUpdateRequest
```

In `NewServer`, initialize the channel:

```go
repoUpdateCh: make(chan repoUpdateRequest, repoUpdateChannelSize),
```

**Step 5: Run tests to verify they pass**

Run: `go test ./internal/server/ -run TestRunPostUpdateHook -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/server/repo_update_worker.go internal/server/repo_update_worker_test.go internal/server/server.go
git commit -m "Add centralized repo update worker with postUpdateHook support"
```

---

### Task 5: Start the worker goroutine and refactor callers

**Files:**
- Modify: `internal/server/server.go:92-108` (start worker in Run)
- Modify: `internal/server/template_updater.go` (refactor to enqueue)
- Modify: `internal/server/repos.go` (refactor push handler)

**Step 1: Start the worker goroutine in Server.Run()**

In `internal/server/server.go`, add a new `wg.Add(1)` block alongside the existing background loops (around line 108, after the repo update loop block):

```go
wg.Add(1)
go func() {
	defer wg.Done()
	s.runRepoUpdateWorker(ctx)
}()
```

**Step 2: Refactor the repo update loop to enqueue**

In `internal/server/template_updater.go`, change `runRepoUpdateCycle` so that instead of calling `s.updateRepo(ctx, repoName, refreshDefaultBranch)` directly, it enqueues:

Replace line 99:
```go
s.updateRepo(ctx, repoName, refreshDefaultBranch)
```

With:
```go
select {
case s.repoUpdateCh <- repoUpdateRequest{
	repoName:             repoName,
	refreshDefaultBranch: refreshDefaultBranch,
}:
default:
	s.logger.Printf("Repo update: channel full, skipping '%s'", repoName)
}
```

Similarly, in `ensureRepoCloned`, after a successful clone (after the log message at line 126-127), enqueue a forceRunHook request:

```go
select {
case s.repoUpdateCh <- repoUpdateRequest{
	repoName:     repoName,
	forceRunHook: true,
}:
default:
	s.logger.Printf("Repo update: channel full, skipping first-clone hook for '%s'", repoName)
}
```

**Step 3: Delete the old `updateRepo` method**

Remove the `updateRepo` method entirely (lines 130-145 of `template_updater.go`). All its logic is now in `processRepoUpdate` in the worker.

**Step 4: Refactor the push handler to fire-and-forget**

In `internal/server/repos.go`, replace the synchronous `ForceUpdateRepo` call with an enqueue:

```go
func (s *Server) handlePushEvent(w http.ResponseWriter, r *http.Request) {
	repoName := strings.TrimPrefix(r.URL.Path, "/repos/")
	repoName = strings.TrimSuffix(repoName, "/push-event")

	if repoName == "" {
		writeError(w, http.StatusBadRequest, "repo name is required")
		return
	}

	// Verify the repo library directory exists
	repoDirpath := config.GetRepoDirpath(s.agencDirpath, repoName)
	if _, err := os.Stat(repoDirpath); os.IsNotExist(err) {
		writeError(w, http.StatusNotFound, "repo not found: "+repoName)
		return
	}

	select {
	case s.repoUpdateCh <- repoUpdateRequest{repoName: repoName}:
		s.logger.Printf("Push event: enqueued update for '%s'", repoName)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	default:
		s.logger.Printf("Push event: channel full, could not enqueue '%s'", repoName)
		writeError(w, http.StatusServiceUnavailable, "update queue full")
	}
}
```

Note: You'll need to add `"os"` to the import block in `repos.go`. You can also remove the `"github.com/odyssey/agenc/internal/mission"` import since `ForceUpdateRepo` is no longer called directly.

**Step 5: Run all server and cmd tests**

Run: `go test ./internal/server/ -v`
Run: `go test ./cmd/ -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/server/server.go internal/server/template_updater.go internal/server/repos.go
git commit -m "Refactor repo updates to use centralized worker goroutine"
```

---

### Task 6: Update architecture doc

**Files:**
- Modify: `docs/system-architecture.md:106-113`

**Step 1: Update the "Repo update loop" section**

Replace the description of background loop #1 with the updated architecture. The section should now describe:

1. **Repo update loop** (`internal/server/template_updater.go`)
   - Runs every 60 seconds
   - Collects repos to sync (alwaysSynced + active mission repos)
   - Enqueues update requests to the repo update worker channel
   - Refreshes `origin/HEAD` flag set every 10 cycles

Add a new section after the six loops for the worker:

**7. Repo update worker** (`internal/server/repo_update_worker.go`)
- Processes update requests from the channel (fed by repo update loop and push event handler)
- For each request: captures HEAD, runs `ForceUpdateRepo`, compares HEAD
- If HEAD changed (or first clone), reads `postUpdateHook` from config and runs it via `sh -c`
- Hook timeout: 30 minutes hard limit, WARN logs after 5 minutes
- Hook failures are logged but non-fatal

Also update the push event endpoint description to note it returns 202 and enqueues asynchronously.

**Step 2: Commit**

```bash
git add docs/system-architecture.md
git commit -m "Update architecture doc with repo update worker and postUpdateHook"
```

---

### Task 7: Configure postUpdateHook for mieubrisse/agenc

This is the real-world activation step.

**Step 1: Set the hook via CLI**

Run: `./agenc config repoConfig set github.com/mieubrisse/agenc --post-update-hook="make setup"`

Expected: `Updated repo config for 'github.com/mieubrisse/agenc'`

**Step 2: Verify config**

Run: `./agenc config get` (or read the config file directly)

Expected: The `github.com/mieubrisse/agenc` entry should show `postUpdateHook: "make setup"`

**Step 3: Commit the config change (if config is in a git-tracked location)**

The daemon's config auto-commit loop will handle this, but confirm manually if needed.

---

### Task 8: Update the bead and final cleanup

**Step 1: Close the bead**

Run: `bd close agent-egn --reason="Implemented: PostUpdateHook field, centralized update worker, CLI flag, arch doc update"`

**Step 2: Run full test suite**

Run: `make check`
Expected: All tests pass (wrapper integration tests may fail due to pre-existing sandbox issues — this is known and unrelated)

**Step 3: Sync and push**

```bash
bd sync
git add .beads/
git commit -m "Update beads: close agent-egn postUpdateHook feature"
git push
```
