package claudeconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildClaudeConfigDenyEntries(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	claudeConfigDirpath := filepath.Join(homeDir, ".agenc", "missions", "test-uuid", "claude-config")
	entries := BuildClaudeConfigDenyEntries(claudeConfigDirpath)

	// Count expected item suffixes: files get 1 suffix, directories get 2 (/* and /**)
	expectedItemSuffixes := 0
	for _, item := range claudeConfigProtectedItems {
		if isFileName(item) {
			expectedItemSuffixes++
		} else {
			expectedItemSuffixes += 2 // /* and /**
		}
	}

	expectedToolCount := len(AgencDenyPermissionTools)
	expectedPathVariants := 2 // //absolute, tilde
	expectedTotal := expectedToolCount * expectedPathVariants * expectedItemSuffixes

	if len(entries) != expectedTotal {
		t.Errorf("expected %d entries (tools=%d × variants=%d × suffixes=%d), got %d",
			expectedTotal, expectedToolCount, expectedPathVariants, expectedItemSuffixes, len(entries))
	}

	// Verify both path variants are present for at least one tool+item combo
	foundAbsolute := false
	foundTilde := false

	for _, entry := range entries {
		if strings.Contains(entry, "Edit(/"+claudeConfigDirpath+"/settings.json)") {
			foundAbsolute = true
		}
		if strings.Contains(entry, "Edit(~/.agenc/missions/test-uuid/claude-config/settings.json)") {
			foundTilde = true
		}
	}

	if !foundAbsolute {
		t.Error("missing //absolute path variant")
	}
	if !foundTilde {
		t.Error("missing tilde path variant")
	}

	// Verify files get exact match and directories get both /* and /** globs
	foundFileExact := false
	foundDirRecursiveGlob := false
	foundDirSingleGlob := false
	for _, entry := range entries {
		if strings.Contains(entry, "/CLAUDE.md)") {
			foundFileExact = true
		}
		if strings.Contains(entry, "/skills/**)") {
			foundDirRecursiveGlob = true
		}
		if strings.Contains(entry, "/skills/*)") {
			foundDirSingleGlob = true
		}
	}

	if !foundFileExact {
		t.Error("file items should use exact path match (no glob)")
	}
	if !foundDirRecursiveGlob {
		t.Error("directory items should use /** glob suffix")
	}
	if !foundDirSingleGlob {
		t.Error("directory items should use /* glob suffix")
	}

	// Verify symlinked dirs like shell-snapshots are NOT denied
	for _, entry := range entries {
		if strings.Contains(entry, "shell-snapshots") {
			t.Error("shell-snapshots should not be denied — it's a symlinked runtime directory")
		}
		if strings.Contains(entry, "plugins") {
			t.Error("plugins should not be denied — it's a symlinked runtime directory")
		}
	}
}

func TestIsFileName(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"CLAUDE.md", true},
		{"settings.json", true},
		{".claude.json", true},
		{"skills", false},
		{"hooks", false},
		{"commands", false},
		{"agents", false},
	}

	for _, tt := range tests {
		got := isFileName(tt.name)
		if got != tt.expected {
			t.Errorf("isFileName(%q) = %v, want %v", tt.name, got, tt.expected)
		}
	}
}

func TestBuildPathVariants(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	testPath := filepath.Join(homeDir, ".agenc", "missions", "abc123", "claude-config")
	variants := buildPathVariants(testPath)

	if len(variants) != 2 {
		t.Fatalf("expected 2 path variants, got %d: %v", len(variants), variants)
	}

	// Check absolute path uses // prefix (gitignore syntax for filesystem-absolute).
	// Since testPath already starts with /, prepending one more / gives //.
	expectedAbsolute := "/" + testPath
	if variants[0] != expectedAbsolute {
		t.Errorf("expected absolute path %q, got %q", expectedAbsolute, variants[0])
	}

	// Check tilde path
	expectedTilde := filepath.Join("~", ".agenc", "missions", "abc123", "claude-config")
	if variants[1] != expectedTilde {
		t.Errorf("expected tilde path %q, got %q", expectedTilde, variants[1])
	}
}

func TestBuildPathVariantsNonHomePath(t *testing.T) {
	// Paths outside home directory should only produce the // absolute variant
	testPath := "/tmp/some/path"
	variants := buildPathVariants(testPath)

	if len(variants) != 1 {
		t.Fatalf("expected 1 path variant for non-home path, got %d: %v", len(variants), variants)
	}

	expectedAbsolute := "//tmp/some/path"
	if variants[0] != expectedAbsolute {
		t.Errorf("expected %q, got %q", expectedAbsolute, variants[0])
	}
}
