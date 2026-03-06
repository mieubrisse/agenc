Stale Repo JIT Pull Implementation Plan
========================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Before copying a repo into a new mission, force-pull the library clone if it hasn't been fetched in over 24 hours.

**Architecture:** Add an `IsRepoStale` function that checks `.git/FETCH_HEAD` mtime. Call it in `handleCreateMission` before `CreateMissionDir` — if stale, run `ForceUpdateRepo` synchronously.

**Tech Stack:** Go, git (FETCH_HEAD mtime)

---

Task 1: Add `IsRepoStale` function
-----------------------------------

**Files:**
- Modify: `internal/mission/repo.go`
- Test: `internal/mission/repo_test.go`

**Step 1: Write the failing test**

In `internal/mission/repo_test.go`, add:

```go
func TestIsRepoStale(t *testing.T) {
	// Setup: create a temp dir with .git/FETCH_HEAD
	tmpDir := t.TempDir()
	gitDir := filepath.Join(tmpDir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}
	fetchHeadFilepath := filepath.Join(gitDir, "FETCH_HEAD")

	t.Run("missing FETCH_HEAD returns true", func(t *testing.T) {
		if !IsRepoStale(tmpDir, 24*time.Hour) {
			t.Error("expected stale when FETCH_HEAD is missing")
		}
	})

	t.Run("old FETCH_HEAD returns true", func(t *testing.T) {
		if err := os.WriteFile(fetchHeadFilepath, []byte("abc123"), 0644); err != nil {
			t.Fatal(err)
		}
		oldTime := time.Now().Add(-48 * time.Hour)
		if err := os.Chtimes(fetchHeadFilepath, oldTime, oldTime); err != nil {
			t.Fatal(err)
		}
		if !IsRepoStale(tmpDir, 24*time.Hour) {
			t.Error("expected stale when FETCH_HEAD is 48h old")
		}
	})

	t.Run("recent FETCH_HEAD returns false", func(t *testing.T) {
		if err := os.WriteFile(fetchHeadFilepath, []byte("abc123"), 0644); err != nil {
			t.Fatal(err)
		}
		// File was just written, mtime is now — should not be stale
		if IsRepoStale(tmpDir, 24*time.Hour) {
			t.Error("expected not stale when FETCH_HEAD was just written")
		}
	})
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/mission/ -run TestIsRepoStale -v`
Expected: FAIL — `IsRepoStale` not defined

**Step 3: Write minimal implementation**

In `internal/mission/repo.go`, add:

```go
// IsRepoStale reports whether the repo's last fetch is older than maxAge.
// Checks the mtime of .git/FETCH_HEAD, which git updates on every fetch.
// Returns true (stale) if the file is missing or on any error — erring on
// the side of freshness.
func IsRepoStale(repoDirpath string, maxAge time.Duration) bool {
	fetchHeadFilepath := filepath.Join(repoDirpath, ".git", "FETCH_HEAD")
	info, err := os.Stat(fetchHeadFilepath)
	if err != nil {
		return true
	}
	return time.Since(info.ModTime()) > maxAge
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/mission/ -run TestIsRepoStale -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/mission/repo.go internal/mission/repo_test.go
git commit -m "Add IsRepoStale function to check repo freshness via FETCH_HEAD mtime"
```

---

Task 2: Call `IsRepoStale` before mission creation
---------------------------------------------------

**Files:**
- Modify: `internal/server/missions.go` (around line 294-296)

**Step 1: Add the staleness check**

In `handleCreateMission`, after the block that sets `gitCloneDirpath` (line ~296) and before `CreateMissionDir` (line ~320), add:

```go
	// Force-pull the library clone if it hasn't been fetched recently.
	// This prevents missions from starting with a stale copy of the repo.
	if gitCloneDirpath != "" && mission.IsRepoStale(gitCloneDirpath, 24*time.Hour) {
		s.logger.Printf("Mission create: force-pulling stale repo '%s' before copy", gitRepoName)
		if err := mission.ForceUpdateRepo(gitCloneDirpath); err != nil {
			s.logger.Printf("Mission create: failed to pull stale repo '%s': %v (proceeding with stale copy)", gitRepoName, err)
		}
	}
```

This goes right before the `// Create mission directory structure` comment.

**Step 2: Build to verify compilation**

Run: `make check`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/server/missions.go
git commit -m "Force-pull stale repos before copying into new missions"
```
