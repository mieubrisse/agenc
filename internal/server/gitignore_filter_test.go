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

	// Add a .git/info/exclude entry — machine-local ignore.
	gitInfoDir := filepath.Join(tmpDir, ".git", "info")
	if err := os.MkdirAll(gitInfoDir, 0755); err != nil {
		t.Fatalf("failed to create .git/info dir: %v", err)
	}
	excludeContent := "*.tmp\n"
	if err := os.WriteFile(filepath.Join(gitInfoDir, "exclude"), []byte(excludeContent), 0644); err != nil {
		t.Fatalf("failed to write .git/info/exclude: %v", err)
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
		{"machine-local exclude via .git/info/exclude", filepath.Join(tmpDir, "scratch.tmp"), false, true},
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
