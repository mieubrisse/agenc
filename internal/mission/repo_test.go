package mission

import (
	"bytes"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// discardLogger satisfies the copy functions' logger parameter in tests, where
// the fallback warning has no useful destination.
var discardLogger = log.New(io.Discard, "", 0)

func TestGetHEAD(t *testing.T) {
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

	head, err := GetHEAD(tmpDir)
	if err != nil {
		t.Fatalf("GetHEAD failed: %v", err)
	}

	if len(head) != 40 {
		t.Errorf("expected 40-char SHA, got %d chars: %s", len(head), head)
	}

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

// newOriginAndClone builds a bare "origin" repo with one commit plus a clone of
// it with origin/HEAD set, mirroring the shape of an AgenC repo-library clone.
// Returns (originDirpath, cloneDirpath).
func newOriginAndClone(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	originDirpath := filepath.Join(root, "origin.git")
	seedDirpath := filepath.Join(root, "seed")
	cloneDirpath := filepath.Join(root, "clone")

	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s failed: %s: %v", args, dir, output, err)
		}
	}

	run(root, "init", "--bare", "--initial-branch=main", originDirpath)
	run(root, "init", "--initial-branch=main", seedDirpath)
	run(seedDirpath, "config", "user.email", "test@test.com")
	run(seedDirpath, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(seedDirpath, "file.txt"), []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	run(seedDirpath, "add", "file.txt")
	run(seedDirpath, "commit", "-m", "initial commit")
	run(seedDirpath, "remote", "add", "origin", originDirpath)
	run(seedDirpath, "push", "origin", "main")

	run(root, "clone", originDirpath, cloneDirpath)
	run(cloneDirpath, "config", "user.email", "test@test.com")
	run(cloneDirpath, "config", "user.name", "Test")
	run(cloneDirpath, "remote", "set-head", "origin", "--auto")

	// Give origin a second commit so the clone can be made genuinely behind.
	if err := os.WriteFile(filepath.Join(seedDirpath, "file.txt"), []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}
	run(seedDirpath, "commit", "-am", "second commit")
	run(seedDirpath, "push", "origin", "main")

	return originDirpath, cloneDirpath
}

func touchFetchHead(t *testing.T, repoDirpath string, age time.Duration) {
	t.Helper()
	fetchHeadFilepath := filepath.Join(repoDirpath, ".git", "FETCH_HEAD")
	if err := os.WriteFile(fetchHeadFilepath, []byte("abc123"), 0644); err != nil {
		t.Fatal(err)
	}
	when := time.Now().Add(-age)
	if err := os.Chtimes(fetchHeadFilepath, when, when); err != nil {
		t.Fatal(err)
	}
}

func TestIsRepoStale(t *testing.T) {
	t.Run("missing FETCH_HEAD returns true", func(t *testing.T) {
		tmpDir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(tmpDir, ".git"), 0755); err != nil {
			t.Fatal(err)
		}
		if !IsRepoStale(tmpDir, 24*time.Hour) {
			t.Error("expected stale when FETCH_HEAD is missing")
		}
	})

	t.Run("old FETCH_HEAD returns true", func(t *testing.T) {
		_, clone := newOriginAndClone(t)
		if err := ForceUpdateRepo(clone); err != nil {
			t.Fatalf("ForceUpdateRepo failed: %v", err)
		}
		touchFetchHead(t, clone, 48*time.Hour)
		if !IsRepoStale(clone, 24*time.Hour) {
			t.Error("expected stale when FETCH_HEAD is 48h old")
		}
	})

	t.Run("recent FETCH_HEAD on a pristine clone returns false", func(t *testing.T) {
		_, clone := newOriginAndClone(t)
		if err := ForceUpdateRepo(clone); err != nil {
			t.Fatalf("ForceUpdateRepo failed: %v", err)
		}
		touchFetchHead(t, clone, 0)
		if IsRepoStale(clone, 24*time.Hour) {
			t.Error("expected not stale for a freshly-fetched pristine clone")
		}
	})

	// Regression: the incident. A fresh FETCH_HEAD said "fresh" while HEAD was
	// frozen commits behind origin, because fetch kept succeeding after reset
	// started failing. Freshness must be judged on HEAD, not on FETCH_HEAD alone.
	t.Run("fresh FETCH_HEAD but HEAD behind origin returns true", func(t *testing.T) {
		_, clone := newOriginAndClone(t)
		if err := ForceUpdateRepo(clone); err != nil {
			t.Fatalf("ForceUpdateRepo failed: %v", err)
		}
		cmd := exec.Command("git", "reset", "--hard", "HEAD~1")
		cmd.Dir = clone
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("reset failed: %s: %v", output, err)
		}
		touchFetchHead(t, clone, 0)
		if !IsRepoStale(clone, 24*time.Hour) {
			t.Error("expected stale when HEAD is behind origin despite a fresh FETCH_HEAD")
		}
	})

	// Regression: untracked debris in the library clone gets rsynced into every
	// mission, so a dirty tree must also count as stale.
	t.Run("fresh FETCH_HEAD but dirty tree returns true", func(t *testing.T) {
		_, clone := newOriginAndClone(t)
		if err := ForceUpdateRepo(clone); err != nil {
			t.Fatalf("ForceUpdateRepo failed: %v", err)
		}
		if err := os.WriteFile(filepath.Join(clone, "junk.txt"), []byte("debris"), 0644); err != nil {
			t.Fatal(err)
		}
		touchFetchHead(t, clone, 0)
		if !IsRepoStale(clone, 24*time.Hour) {
			t.Error("expected stale when the working tree has untracked debris")
		}
	})
}

// Regression: a killed git process leaves .git/index.lock behind. Every later
// reset fails with exit 128 while fetch keeps succeeding, so the clone froze
// three weeks behind origin with a dirty tree and nothing surfaced it.
func TestForceUpdateRepo_ClearsStaleIndexLockAndDebris(t *testing.T) {
	_, clone := newOriginAndClone(t)

	lockFilepath := filepath.Join(clone, ".git", "index.lock")
	if err := os.WriteFile(lockFilepath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-24 * time.Hour)
	if err := os.Chtimes(lockFilepath, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(clone, "junk.txt"), []byte("debris"), 0644); err != nil {
		t.Fatal(err)
	}

	// Positive control: without the fix this reset is what fails.
	control := exec.Command("git", "reset", "--hard", "origin/main")
	control.Dir = clone
	if _, err := control.CombinedOutput(); err == nil {
		t.Fatal("positive control failed: reset succeeded despite a stale index.lock, so this test proves nothing")
	}

	if err := ForceUpdateRepo(clone); err != nil {
		t.Fatalf("ForceUpdateRepo should clear the stale lock and succeed, got: %v", err)
	}
	if _, err := os.Stat(lockFilepath); !os.IsNotExist(err) {
		t.Error("expected the stale index.lock to be removed")
	}
	if _, err := os.Stat(filepath.Join(clone, "junk.txt")); !os.IsNotExist(err) {
		t.Error("expected untracked debris to be cleaned")
	}
	if !isRepoPristine(clone) {
		t.Error("expected the clone to be pristine after ForceUpdateRepo")
	}
}

// A lock young enough to plausibly have a live owner must be left alone.
func TestForceUpdateRepo_LeavesFreshIndexLock(t *testing.T) {
	_, clone := newOriginAndClone(t)
	lockFilepath := filepath.Join(clone, ".git", "index.lock")
	if err := os.WriteFile(lockFilepath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := ForceUpdateRepo(clone); err == nil {
		t.Error("expected ForceUpdateRepo to fail rather than stomp a live index.lock")
	}
	if _, err := os.Stat(lockFilepath); err != nil {
		t.Error("expected a fresh index.lock to be left in place")
	}
}

// CopyRepo must not reproduce a lock file into the mission copy; rsync -a does.
func TestCopyRepo_DropsIndexLock(t *testing.T) {
	_, clone := newOriginAndClone(t)
	if err := os.WriteFile(filepath.Join(clone, ".git", "index.lock"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "mission-agent")
	if err := CopyRepo(discardLogger, clone, dst); err != nil {
		t.Fatalf("CopyRepo failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Error("expected the copied repo to have no index.lock")
	}
}

func TestParseRepoReference(t *testing.T) {
	tests := []struct {
		name         string
		ref          string
		preferSSH    bool
		wantRepoName string
		wantCloneURL string
		wantErr      bool
	}{
		// SSH URLs should always return SSH clone URLs regardless of preferSSH
		{
			name:         "SSH URL git@github.com format",
			ref:          "git@github.com:owner/repo.git",
			preferSSH:    false,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "git@github.com:owner/repo.git",
		},
		{
			name:         "SSH URL without .git suffix",
			ref:          "git@github.com:owner/repo",
			preferSSH:    false,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "git@github.com:owner/repo.git",
		},
		{
			name:         "SSH URL ssh:// protocol",
			ref:          "ssh://git@github.com/owner/repo.git",
			preferSSH:    false,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "git@github.com:owner/repo.git",
		},
		{
			name:         "SSH URL ssh:// without .git suffix",
			ref:          "ssh://git@github.com/owner/repo",
			preferSSH:    false,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "git@github.com:owner/repo.git",
		},

		// HTTPS URLs should always return HTTPS clone URLs regardless of preferSSH
		{
			name:         "HTTPS URL",
			ref:          "https://github.com/owner/repo",
			preferSSH:    true,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "https://github.com/owner/repo.git",
		},
		{
			name:         "HTTPS URL with .git suffix",
			ref:          "https://github.com/owner/repo.git",
			preferSSH:    true,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "https://github.com/owner/repo.git",
		},
		{
			name:         "HTTPS URL with extra path segments",
			ref:          "https://github.com/owner/repo/tree/main/src",
			preferSSH:    true,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "https://github.com/owner/repo.git",
		},

		// Shorthand references should respect preferSSH
		{
			name:         "owner/repo shorthand with preferSSH=false",
			ref:          "owner/repo",
			preferSSH:    false,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "https://github.com/owner/repo.git",
		},
		{
			name:         "owner/repo shorthand with preferSSH=true",
			ref:          "owner/repo",
			preferSSH:    true,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "git@github.com:owner/repo.git",
		},
		{
			name:         "github.com/owner/repo with preferSSH=false",
			ref:          "github.com/owner/repo",
			preferSSH:    false,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "https://github.com/owner/repo.git",
		},
		{
			name:         "github.com/owner/repo with preferSSH=true",
			ref:          "github.com/owner/repo",
			preferSSH:    true,
			wantRepoName: "github.com/owner/repo",
			wantCloneURL: "git@github.com:owner/repo.git",
		},

		// Error cases
		{
			name:    "unsupported host",
			ref:     "gitlab.com/owner/repo",
			wantErr: true,
		},
		{
			name:    "invalid format - too many parts",
			ref:     "a/b/c/d",
			wantErr: true,
		},
		{
			name:    "invalid format - single part",
			ref:     "repo",
			wantErr: true,
		},
		{
			name:    "empty owner",
			ref:     "/repo",
			wantErr: true,
		},
		{
			name:    "empty repo",
			ref:     "owner/",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoName, cloneURL, err := ParseRepoReference(tt.ref, tt.preferSSH, "")

			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseRepoReference(%q, %v) expected error, got nil", tt.ref, tt.preferSSH)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseRepoReference(%q, %v) unexpected error: %v", tt.ref, tt.preferSSH, err)
				return
			}

			if repoName != tt.wantRepoName {
				t.Errorf("ParseRepoReference(%q, %v) repoName = %q, want %q", tt.ref, tt.preferSSH, repoName, tt.wantRepoName)
			}

			if cloneURL != tt.wantCloneURL {
				t.Errorf("ParseRepoReference(%q, %v) cloneURL = %q, want %q", tt.ref, tt.preferSSH, cloneURL, tt.wantCloneURL)
			}
		})
	}
}

func TestParseRepoReferenceWithDefaultOwner(t *testing.T) {
	tests := []struct {
		name         string
		ref          string
		preferSSH    bool
		defaultOwner string
		wantRepoName string
		wantCloneURL string
	}{
		{
			name:         "bare repo name with defaultOwner",
			ref:          "my-repo",
			preferSSH:    false,
			defaultOwner: "testuser",
			wantRepoName: "github.com/testuser/my-repo",
			wantCloneURL: "https://github.com/testuser/my-repo.git",
		},
		{
			name:         "bare repo name with defaultOwner and SSH",
			ref:          "my-repo",
			preferSSH:    true,
			defaultOwner: "testuser",
			wantRepoName: "github.com/testuser/my-repo",
			wantCloneURL: "git@github.com:testuser/my-repo.git",
		},
		{
			name:         "owner/repo ignores defaultOwner",
			ref:          "otheruser/repo",
			preferSSH:    false,
			defaultOwner: "testuser",
			wantRepoName: "github.com/otheruser/repo",
			wantCloneURL: "https://github.com/otheruser/repo.git",
		},
		{
			name:         "bare repo name without defaultOwner fails",
			ref:          "my-repo",
			preferSSH:    false,
			defaultOwner: "",
			wantRepoName: "",
			wantCloneURL: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repoName, cloneURL, err := ParseRepoReference(tt.ref, tt.preferSSH, tt.defaultOwner)

			// Last test case should error (bare name without defaultOwner)
			if tt.wantRepoName == "" {
				if err == nil {
					t.Errorf("ParseRepoReference(%q, %v, %q) expected error, got nil", tt.ref, tt.preferSSH, tt.defaultOwner)
				}
				return
			}

			if err != nil {
				t.Errorf("ParseRepoReference(%q, %v, %q) unexpected error: %v", tt.ref, tt.preferSSH, tt.defaultOwner, err)
				return
			}

			if repoName != tt.wantRepoName {
				t.Errorf("ParseRepoReference(%q, %v, %q) repoName = %q, want %q", tt.ref, tt.preferSSH, tt.defaultOwner, repoName, tt.wantRepoName)
			}

			if cloneURL != tt.wantCloneURL {
				t.Errorf("ParseRepoReference(%q, %v, %q) cloneURL = %q, want %q", tt.ref, tt.preferSSH, tt.defaultOwner, cloneURL, tt.wantCloneURL)
			}
		})
	}
}

// writeCopySourceTree builds a source tree exercising the properties a repo
// copy has to preserve: dotfiles and dot-directories (.git), nesting, a
// symlink (which must be copied as a symlink, not followed), a dangling
// symlink, a non-default file mode, and a non-current mtime.
func writeCopySourceTree(t *testing.T, srcDirpath string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(srcDirpath, ".git", "objects"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(srcDirpath, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDirpath, ".git", "config"), []byte("[core]\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDirpath, ".git", "objects", "obj"), []byte("object-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDirpath, "sub", "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(srcDirpath, "script.sh"), []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("script.sh", filepath.Join(srcDirpath, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("does-not-exist", filepath.Join(srcDirpath, "dangling")); err != nil {
		t.Fatal(err)
	}

	// A non-default mode on the source root itself, so the root-mode assertion
	// in assertTreesMatch can tell "copied" apart from "MkdirAll's default".
	if err := os.Chmod(srcDirpath, 0750); err != nil {
		t.Fatal(err)
	}

	// Mtimes well in the past, so "preserved" is distinguishable from
	// "happened to be written at the same second as the copy". Every regular
	// file gets a whole-second timestamp: rsync -a truncates mtimes to whole
	// seconds where cp preserves nanoseconds, so a sub-second fixture mtime
	// would make the mtime assertions fail on whichever platforms take the
	// rsync path.
	past := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	for _, relFilepath := range []string{
		filepath.Join(".git", "config"),
		filepath.Join(".git", "objects", "obj"),
		filepath.Join("sub", "nested.txt"),
		"script.sh",
	} {
		if err := os.Chtimes(filepath.Join(srcDirpath, relFilepath), past, past); err != nil {
			t.Fatal(err)
		}
	}
}

// assertEntryMatches requires one source entry's destination counterpart to
// have the same mode, and — by type — the same symlink target, or the same
// content and mtime.
func assertEntryMatches(t *testing.T, relPath string, srcPath string, dstPath string, srcInfo os.FileInfo) {
	t.Helper()

	dstInfo, err := os.Lstat(dstPath)
	if err != nil {
		t.Errorf("%s: missing from destination: %v", relPath, err)
		return
	}

	if srcInfo.Mode() != dstInfo.Mode() {
		t.Errorf("%s: mode = %v, want %v", relPath, dstInfo.Mode(), srcInfo.Mode())
	}

	if srcInfo.Mode()&os.ModeSymlink != 0 {
		srcTarget, srcErr := os.Readlink(srcPath)
		dstTarget, dstErr := os.Readlink(dstPath)
		if srcErr != nil || dstErr != nil {
			t.Errorf("%s: readlink failed: source %v, destination %v", relPath, srcErr, dstErr)
			return
		}
		if srcTarget != dstTarget {
			t.Errorf("%s: symlink target = %q, want %q", relPath, dstTarget, srcTarget)
		}
		return
	}

	// Directory mtimes shift as children are written into them, so content and
	// mtime are asserted for regular files only.
	if srcInfo.IsDir() {
		return
	}

	// Reading a special file would block (a FIFO's open(2) waits for a writer),
	// so anything that is not a regular file stops at the mode comparison.
	if !srcInfo.Mode().IsRegular() {
		return
	}

	srcContent, srcErr := os.ReadFile(srcPath)
	dstContent, dstErr := os.ReadFile(dstPath)
	if srcErr != nil || dstErr != nil {
		t.Errorf("%s: read failed: source %v, destination %v", relPath, srcErr, dstErr)
		return
	}
	if string(srcContent) != string(dstContent) {
		t.Errorf("%s: content = %q, want %q", relPath, dstContent, srcContent)
	}
	if !srcInfo.ModTime().Equal(dstInfo.ModTime()) {
		t.Errorf("%s: mtime = %v, want %v", relPath, dstInfo.ModTime(), srcInfo.ModTime())
	}
}

// assertTreesMatch walks srcDirpath and requires dstDirpath to hold the same
// relative paths with the same types, contents, modes, mtimes and symlink
// targets — then requires dstDirpath to hold nothing extra.
func assertTreesMatch(t *testing.T, srcDirpath string, dstDirpath string) {
	t.Helper()

	seen := map[string]bool{}
	walkErr := filepath.Walk(srcDirpath, func(srcPath string, srcInfo os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(srcDirpath, srcPath)
		if err != nil {
			return err
		}
		if relPath == "." {
			return nil
		}
		seen[relPath] = true
		assertEntryMatches(t, relPath, srcPath, filepath.Join(dstDirpath, relPath), srcInfo)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walking source tree failed: %v", walkErr)
	}

	// Positive control on the walk itself: a comparison that visited nothing
	// would pass every assertion above.
	if len(seen) == 0 {
		t.Fatal("source tree was empty, so this comparison proved nothing")
	}

	// The walk above skips the roots themselves, but the destination root's
	// mode must match the source root's whichever backend ran (rsync -a and
	// cp -p both apply it to the destination root).
	srcRootInfo, err := os.Lstat(srcDirpath)
	if err != nil {
		t.Fatalf("stat of source root failed: %v", err)
	}
	dstRootInfo, err := os.Lstat(dstDirpath)
	if err != nil {
		t.Fatalf("stat of destination root failed: %v", err)
	}
	if srcRootInfo.Mode().Perm() != dstRootInfo.Mode().Perm() {
		t.Errorf("destination root mode = %v, want %v", dstRootInfo.Mode().Perm(), srcRootInfo.Mode().Perm())
	}

	extraErr := filepath.Walk(dstDirpath, func(dstPath string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, err := filepath.Rel(dstDirpath, dstPath)
		if err != nil {
			return err
		}
		if relPath != "." && !seen[relPath] {
			t.Errorf("%s: present in destination but not in source", relPath)
		}
		return nil
	})
	if extraErr != nil {
		t.Fatalf("walking destination tree failed: %v", extraErr)
	}
}

func TestCopyRepo(t *testing.T) {
	tmpDir := t.TempDir()
	srcDirpath := filepath.Join(tmpDir, "src")
	dstDirpath := filepath.Join(tmpDir, "dst")
	writeCopySourceTree(t, srcDirpath)

	if err := CopyRepo(discardLogger, srcDirpath, dstDirpath); err != nil {
		t.Fatalf("CopyRepo failed: %v", err)
	}

	assertTreesMatch(t, srcDirpath, dstDirpath)
}

func TestCopyRepo_CreatesMissingDestination(t *testing.T) {
	tmpDir := t.TempDir()
	srcDirpath := filepath.Join(tmpDir, "src")
	dstDirpath := filepath.Join(tmpDir, "not", "yet", "there")
	writeCopySourceTree(t, srcDirpath)

	if err := CopyRepo(discardLogger, srcDirpath, dstDirpath); err != nil {
		t.Fatalf("CopyRepo failed: %v", err)
	}

	assertTreesMatch(t, srcDirpath, dstDirpath)
}

func TestCopyRepo_IndependentOfSource(t *testing.T) {
	tmpDir := t.TempDir()
	srcDirpath := filepath.Join(tmpDir, "src")
	dstDirpath := filepath.Join(tmpDir, "dst")
	if err := os.MkdirAll(srcDirpath, 0755); err != nil {
		t.Fatal(err)
	}
	srcFilepath := filepath.Join(srcDirpath, "file.txt")
	if err := os.WriteFile(srcFilepath, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CopyRepo(discardLogger, srcDirpath, dstDirpath); err != nil {
		t.Fatalf("CopyRepo failed: %v", err)
	}

	// Cloned files are copy-on-write, not shared: a write to either side must
	// not be visible on the other.
	dstFilepath := filepath.Join(dstDirpath, "file.txt")
	if err := os.WriteFile(dstFilepath, []byte("changed in destination"), 0644); err != nil {
		t.Fatal(err)
	}
	srcContent, err := os.ReadFile(srcFilepath)
	if err != nil {
		t.Fatal(err)
	}
	if string(srcContent) != "original" {
		t.Errorf("writing the destination changed the source: got %q, want %q", srcContent, "original")
	}

	if err := os.WriteFile(srcFilepath, []byte("changed in source"), 0644); err != nil {
		t.Fatal(err)
	}
	dstContent, err := os.ReadFile(dstFilepath)
	if err != nil {
		t.Fatal(err)
	}
	if string(dstContent) != "changed in destination" {
		t.Errorf("writing the source changed the destination: got %q, want %q", dstContent, "changed in destination")
	}
}

func TestCopyAgentDir(t *testing.T) {
	tmpDir := t.TempDir()
	srcDirpath := filepath.Join(tmpDir, "src")
	dstDirpath := filepath.Join(tmpDir, "dst")
	writeCopySourceTree(t, srcDirpath)

	if err := CopyAgentDir(discardLogger, srcDirpath, dstDirpath); err != nil {
		t.Fatalf("CopyAgentDir failed: %v", err)
	}

	assertTreesMatch(t, srcDirpath, dstDirpath)
}

// TestCopyRepo_MissingSourceFailsLoudly pins the failure path: when the copy
// cannot succeed at all, the returned error carries the rsync failure, and (on
// macOS) the cp failure that preceded it is logged rather than swallowed.
func TestCopyRepo_MissingSourceFailsLoudly(t *testing.T) {
	tmpDir := t.TempDir()
	srcDirpath := filepath.Join(tmpDir, "does-not-exist")
	dstDirpath := filepath.Join(tmpDir, "dst")

	var logBuffer bytes.Buffer
	logger := log.New(&logBuffer, "", 0)

	err := CopyRepo(logger, srcDirpath, dstDirpath)
	if err == nil {
		t.Fatal("expected CopyRepo of a nonexistent source to fail")
	}
	if !strings.Contains(err.Error(), "rsync") {
		t.Errorf("error should carry the rsync failure, got: %v", err)
	}
	if runtime.GOOS == "darwin" {
		if !strings.Contains(logBuffer.String(), "falling back to rsync") {
			t.Errorf("cp failure should be logged before the rsync fallback, got log: %q", logBuffer.String())
		}
	}
}

func TestCopyAgentDir_MissingSourceIsNoOp(t *testing.T) {
	tmpDir := t.TempDir()
	srcDirpath := filepath.Join(tmpDir, "does-not-exist")
	dstDirpath := filepath.Join(tmpDir, "dst")

	if err := CopyAgentDir(discardLogger, srcDirpath, dstDirpath); err != nil {
		t.Fatalf("CopyAgentDir failed: %v", err)
	}

	if _, err := os.Stat(dstDirpath); !os.IsNotExist(err) {
		t.Errorf("expected destination not to be created, got err = %v", err)
	}
}
