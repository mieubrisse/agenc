package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

// TestCheckNoClaudeModificationsOverlay_Absent verifies that the check passes
// cleanly when the retired claude-modifications directory does not exist.
func TestCheckNoClaudeModificationsOverlay_Absent(t *testing.T) {
	agencDirpath := t.TempDir()
	t.Setenv("AGENC_DIRPATH", agencDirpath)

	result := checkNoClaudeModificationsOverlay()
	if !result.passed {
		t.Errorf("expected check to pass when overlay directory absent, got: %s", result.message)
	}
}

// TestCheckNoClaudeModificationsOverlay_Present verifies that the check warns
// when the retired claude-modifications directory exists in the config dir.
func TestCheckNoClaudeModificationsOverlay_Present(t *testing.T) {
	agencDirpath := t.TempDir()
	t.Setenv("AGENC_DIRPATH", agencDirpath)

	// Create the retired overlay directory
	overlayDirpath := filepath.Join(config.GetConfigDirpath(agencDirpath), claudeModificationsDirname)
	if err := os.MkdirAll(overlayDirpath, 0755); err != nil {
		t.Fatalf("failed to create overlay directory: %v", err)
	}

	result := checkNoClaudeModificationsOverlay()
	if result.passed {
		t.Error("expected check to fail when overlay directory exists, but it passed")
	}
	if result.message == "" {
		t.Error("expected a non-empty warning message, got empty string")
	}
}
