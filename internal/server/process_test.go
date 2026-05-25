package server

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTryAcquireServerLock_FirstCallerSucceeds(t *testing.T) {
	lockFilepath := filepath.Join(t.TempDir(), "server.lock")

	lockFile, err := tryAcquireServerLock(lockFilepath)
	if err != nil {
		t.Fatalf("expected lock to succeed, got error: %v", err)
	}
	defer lockFile.Close()

	if lockFile == nil {
		t.Fatal("expected non-nil file handle")
	}
}

func TestTryAcquireServerLock_SecondCallerFails(t *testing.T) {
	lockFilepath := filepath.Join(t.TempDir(), "server.lock")

	lockFile1, err := tryAcquireServerLock(lockFilepath)
	if err != nil {
		t.Fatalf("first lock should succeed: %v", err)
	}
	defer lockFile1.Close()

	lockFile2, err := tryAcquireServerLock(lockFilepath)
	if err != ErrServerLocked {
		t.Fatalf("expected ErrServerLocked, got: %v", err)
	}
	if lockFile2 != nil {
		lockFile2.Close()
		t.Fatal("expected nil file handle when lock is held")
	}
}

func TestTryAcquireServerLock_ReleasedLockCanBeReacquired(t *testing.T) {
	lockFilepath := filepath.Join(t.TempDir(), "server.lock")

	lockFile1, err := tryAcquireServerLock(lockFilepath)
	if err != nil {
		t.Fatalf("first lock should succeed: %v", err)
	}
	lockFile1.Close()

	lockFile2, err := tryAcquireServerLock(lockFilepath)
	if err != nil {
		t.Fatalf("second lock should succeed after release: %v", err)
	}
	defer lockFile2.Close()
}

func TestTryAcquireServerLock_CreatesParentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dir")
	lockFilepath := filepath.Join(dir, "server.lock")

	lockFile, err := tryAcquireServerLock(lockFilepath)
	if err != nil {
		t.Fatalf("expected lock to succeed, got error: %v", err)
	}
	defer lockFile.Close()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("expected parent directory to be created")
	}
}

func TestFindOrphanServerPIDs_EmptyPathReturnsNothing(t *testing.T) {
	// Defensive: if the caller could not determine the executable path, the
	// function must skip the sweep entirely. Falling back to an unscoped
	// pattern would reintroduce the cross-installation kill bug where a
	// `make e2e` teardown nukes the user's production agenc server.
	pids := findOrphanServerPIDs("", map[int]bool{})
	if len(pids) != 0 {
		t.Fatalf("expected no PIDs when executableFilepath is empty, got %v", pids)
	}
}

func TestFindOrphanServerPIDs_NoMatchForBogusPath(t *testing.T) {
	// A path that no process could possibly have launched from must yield no
	// matches. This guards against a regression that reintroduces a substring
	// fallback (e.g. pgrep -f "agenc server run") which would happily match
	// unrelated installations.
	bogusPath := filepath.Join(t.TempDir(), "definitely-not-a-real-agenc-binary")
	pids := findOrphanServerPIDs(bogusPath, map[int]bool{})
	if len(pids) != 0 {
		t.Fatalf("expected no PIDs for path that no process matches, got %v", pids)
	}
}
