package repo

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindReposOnDisk_Empty(t *testing.T) {
	reposDirpath := t.TempDir()

	repoNames, err := FindReposOnDisk(reposDirpath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repoNames) != 0 {
		t.Errorf("expected empty slice, got %v", repoNames)
	}
}

func TestFindReposOnDisk_FindsRepos(t *testing.T) {
	reposDirpath := t.TempDir()

	// Create host/owner/repo directory structures (intentionally unordered)
	repoPaths := []string{
		filepath.Join("github.com", "zeta", "backend"),
		filepath.Join("github.com", "alpha", "frontend"),
		filepath.Join("gitlab.com", "alpha", "tooling"),
		filepath.Join("github.com", "alpha", "api"),
	}
	for _, repoRelPath := range repoPaths {
		dirpath := filepath.Join(reposDirpath, repoRelPath)
		if err := os.MkdirAll(dirpath, 0755); err != nil {
			t.Fatalf("failed to create repo dir %s: %v", dirpath, err)
		}
	}

	// Also create a regular file at the host level to verify it's ignored
	dummyFilepath := filepath.Join(reposDirpath, "README.md")
	if err := os.WriteFile(dummyFilepath, []byte("ignored"), 0644); err != nil {
		t.Fatalf("failed to write dummy file: %v", err)
	}

	repoNames, err := FindReposOnDisk(reposDirpath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{
		"github.com/alpha/api",
		"github.com/alpha/frontend",
		"github.com/zeta/backend",
		"gitlab.com/alpha/tooling",
	}

	if len(repoNames) != len(expected) {
		t.Fatalf("expected %d repos, got %d: %v", len(expected), len(repoNames), repoNames)
	}
	for i, name := range repoNames {
		if name != expected[i] {
			t.Errorf("repo[%d]: expected %q, got %q", i, expected[i], name)
		}
	}
}

func TestFindReposOnDisk_MissingDir(t *testing.T) {
	nonexistentDirpath := filepath.Join(t.TempDir(), "does-not-exist")

	repoNames, err := FindReposOnDisk(nonexistentDirpath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repoNames) != 0 {
		t.Errorf("expected empty slice, got %v", repoNames)
	}
}
