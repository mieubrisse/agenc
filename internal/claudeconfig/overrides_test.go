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

	// Should generate deny entries for all tools × all path variants
	expectedToolCount := len(AgencDenyPermissionTools)
	expectedPathVariants := 3 // absolute, tilde, ${HOME}
	expectedTotal := expectedToolCount * expectedPathVariants

	if len(entries) != expectedTotal {
		t.Errorf("expected %d entries (tools=%d × variants=%d), got %d",
			expectedTotal, expectedToolCount, expectedPathVariants, len(entries))
	}

	// Verify all three path variants are present for at least one tool
	foundAbsolute := false
	foundTilde := false
	foundHomeEnv := false

	for _, entry := range entries {
		if strings.Contains(entry, "Edit("+claudeConfigDirpath) {
			foundAbsolute = true
		}
		if strings.Contains(entry, "Edit(~/.agenc/missions/test-uuid/claude-config") {
			foundTilde = true
		}
		if strings.Contains(entry, "Edit(${HOME}/.agenc/missions/test-uuid/claude-config") {
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
