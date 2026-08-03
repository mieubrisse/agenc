package claudeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestBuildAgencHookEntries_IncludesPreToolUseGuard(t *testing.T) {
	claudeConfigDirpath := "/tmp/test-mission/claude-config"
	entries := BuildAgencHookEntries(claudeConfigDirpath)

	preToolUseRaw, ok := entries["PreToolUse"]
	if !ok {
		t.Fatal("expected PreToolUse entry in BuildAgencHookEntries result")
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(preToolUseRaw, &arr); err != nil {
		t.Fatalf("failed to parse PreToolUse entry: %v", err)
	}
	if len(arr) != 1 {
		t.Fatalf("expected exactly 1 PreToolUse hook group, got %d", len(arr))
	}

	matcher, _ := arr[0]["matcher"].(string)
	if matcher != "Write|Edit|NotebookEdit" {
		t.Errorf("expected matcher 'Write|Edit|NotebookEdit', got %q", matcher)
	}

	hooks, _ := arr[0]["hooks"].([]interface{})
	if len(hooks) != 1 {
		t.Fatalf("expected 1 hook command, got %d", len(hooks))
	}
	hookMap, _ := hooks[0].(map[string]interface{})
	command, _ := hookMap["command"].(string)
	expectedScript := filepath.Join(claudeConfigDirpath, AgencHooksDirname, RepoLibraryGuardScriptName)
	if !strings.Contains(command, expectedScript) {
		t.Errorf("expected hook command to reference %q, got %q", expectedScript, command)
	}
}

// TestSessionStartHookEntry_WiresPrimeInjection verifies that the SessionStart
// hook entry fires `agenc prime` on every fresh Claude spawn. This is what
// replaced the old hardcoded Layer 1 CLAUDE.md prepend (see bead agenc-88kh).
func TestSessionStartHookEntry_WiresPrimeInjection(t *testing.T) {
	entries := BuildAgencHookEntries("/tmp/test-mission/claude-config")
	raw, ok := entries["SessionStart"]
	if !ok {
		t.Fatal("expected SessionStart entry in BuildAgencHookEntries result")
	}
	if !strings.Contains(string(raw), `"agenc prime"`) {
		t.Errorf("expected SessionStart command to be \"agenc prime\", got: %s", string(raw))
	}
}
