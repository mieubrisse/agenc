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

	// Should generate deny entries for all tools × all path variants × all protected items
	expectedToolCount := len(AgencDenyPermissionTools)
	expectedPathVariants := 3 // absolute, tilde, ${HOME}
	expectedItemCount := len(claudeConfigProtectedItems)
	expectedTotal := expectedToolCount * expectedPathVariants * expectedItemCount

	if len(entries) != expectedTotal {
		t.Errorf("expected %d entries (tools=%d × variants=%d × items=%d), got %d",
			expectedTotal, expectedToolCount, expectedPathVariants, expectedItemCount, len(entries))
	}

	// Verify all three path variants are present for at least one tool+item combo
	foundAbsolute := false
	foundTilde := false
	foundHomeEnv := false

	for _, entry := range entries {
		if strings.Contains(entry, "Edit("+claudeConfigDirpath+"/settings.json)") {
			foundAbsolute = true
		}
		if strings.Contains(entry, "Edit(~/.agenc/missions/test-uuid/claude-config/settings.json)") {
			foundTilde = true
		}
		if strings.Contains(entry, "Edit(${HOME}/.agenc/missions/test-uuid/claude-config/settings.json)") {
			foundHomeEnv = true
		}
	}

	if !foundAbsolute {
		t.Error("missing absolute path variant")
	}
	if !foundTilde {
		t.Error("missing tilde path variant")
	}
	if !foundHomeEnv {
		t.Error("missing ${HOME} path variant")
	}

	// Verify files get exact match and directories get /** glob
	foundFileExact := false
	foundDirGlob := false
	for _, entry := range entries {
		if strings.Contains(entry, "/CLAUDE.md)") {
			foundFileExact = true
		}
		if strings.Contains(entry, "/skills/**)") {
			foundDirGlob = true
		}
	}

	if !foundFileExact {
		t.Error("file items should use exact path match (no glob)")
	}
	if !foundDirGlob {
		t.Error("directory items should use /** glob suffix")
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

	if len(variants) != 3 {
		t.Fatalf("expected 3 path variants, got %d: %v", len(variants), variants)
	}

	// Check absolute path
	if variants[0] != testPath {
		t.Errorf("expected absolute path %q, got %q", testPath, variants[0])
	}

	// Check tilde path
	expectedTilde := filepath.Join("~", ".agenc", "missions", "abc123", "claude-config")
	if variants[1] != expectedTilde {
		t.Errorf("expected tilde path %q, got %q", expectedTilde, variants[1])
	}

	// Check ${HOME} path
	expectedHomeEnv := filepath.Join("${HOME}", ".agenc", "missions", "abc123", "claude-config")
	if variants[2] != expectedHomeEnv {
		t.Errorf("expected ${HOME} path %q, got %q", expectedHomeEnv, variants[2])
	}
}
