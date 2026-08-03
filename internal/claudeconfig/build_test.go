package claudeconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestComputeProjectDirpath(t *testing.T) {
	result, err := ComputeProjectDirpath("/Users/odyssey/.agenc/missions/abc-123/agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, ".claude", "projects", "-Users-odyssey--agenc-missions-abc-123-agent")
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

func TestWriteAgencHookScripts(t *testing.T) {
	tmpDir := t.TempDir()
	claudeConfigDirpath := filepath.Join(tmpDir, "claude-config")
	if err := os.MkdirAll(claudeConfigDirpath, 0755); err != nil {
		t.Fatalf("failed to create claude-config dir: %v", err)
	}

	if err := WriteAgencHookScripts(claudeConfigDirpath); err != nil {
		t.Fatalf("WriteAgencHookScripts returned error: %v", err)
	}

	scriptFilepath := filepath.Join(claudeConfigDirpath, AgencHooksDirname, RepoLibraryGuardScriptName)
	info, err := os.Stat(scriptFilepath)
	if err != nil {
		t.Fatalf("expected hook script at %q, but stat failed: %v", scriptFilepath, err)
	}

	// Script must be executable so `bash <path>` works
	if info.Mode().Perm()&0100 == 0 {
		t.Errorf("expected hook script to be executable, got mode %v", info.Mode().Perm())
	}

	contents, err := os.ReadFile(scriptFilepath)
	if err != nil {
		t.Fatalf("failed to read hook script: %v", err)
	}
	// Sanity check that it is the embedded guard script, not something else
	if !strings.Contains(string(contents), "AgenC PreToolUse hook") {
		t.Errorf("hook script body does not look like the embedded guard script")
	}
}
