package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeSettingsWithAgencHooks(t *testing.T) {
	tests := []struct {
		name          string
		inputJSON     string
		checkMerged   func(t *testing.T, settings map[string]json.RawMessage)
	}{
		{
			name:      "empty settings gets agenc hooks",
			inputJSON: `{}`,
			checkMerged: func(t *testing.T, settings map[string]json.RawMessage) {
				hooks := parseHooksMap(t, settings)
				assertHookArrayLen(t, hooks, "Stop", 1)
				assertHookArrayLen(t, hooks, "UserPromptSubmit", 1)
			},
		},
		{
			name: "existing Stop hooks are preserved and agenc hook appended",
			inputJSON: `{
				"hooks": {
					"Stop": [
						{"hooks": [{"type": "command", "command": "echo done"}]}
					]
				}
			}`,
			checkMerged: func(t *testing.T, settings map[string]json.RawMessage) {
				hooks := parseHooksMap(t, settings)
				assertHookArrayLen(t, hooks, "Stop", 2)
				assertHookArrayLen(t, hooks, "UserPromptSubmit", 1)
			},
		},
		{
			name: "other hook types are preserved untouched",
			inputJSON: `{
				"hooks": {
					"PreToolUse": [
						{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo pre"}]}
					]
				}
			}`,
			checkMerged: func(t *testing.T, settings map[string]json.RawMessage) {
				hooks := parseHooksMap(t, settings)
				assertHookArrayLen(t, hooks, "PreToolUse", 1)
				assertHookArrayLen(t, hooks, "Stop", 1)
				assertHookArrayLen(t, hooks, "UserPromptSubmit", 1)
			},
		},
		{
			name: "non-hooks fields are preserved",
			inputJSON: `{
				"permissions": {"allow": ["Read(./**)"]},
				"enabledPlugins": {"foo": true}
			}`,
			checkMerged: func(t *testing.T, settings map[string]json.RawMessage) {
				if _, ok := settings["permissions"]; !ok {
					t.Error("permissions field was lost")
				}
				if _, ok := settings["enabledPlugins"]; !ok {
					t.Error("enabledPlugins field was lost")
				}
				hooks := parseHooksMap(t, settings)
				assertHookArrayLen(t, hooks, "Stop", 1)
				assertHookArrayLen(t, hooks, "UserPromptSubmit", 1)
			},
		},
		{
			name: "full settings with existing hooks and other fields",
			inputJSON: `{
				"permissions": {"allow": ["Read(./**)"]},
				"hooks": {
					"Stop": [
						{"hooks": [{"type": "command", "command": "printf '\\a' > /dev/tty"}]}
					],
					"PreToolUse": [
						{"matcher": "Bash", "hooks": [{"type": "command", "command": "echo check"}]}
					]
				},
				"enabledPlugins": {"gopls-lsp": true}
			}`,
			checkMerged: func(t *testing.T, settings map[string]json.RawMessage) {
				if _, ok := settings["permissions"]; !ok {
					t.Error("permissions field was lost")
				}
				if _, ok := settings["enabledPlugins"]; !ok {
					t.Error("enabledPlugins field was lost")
				}
				hooks := parseHooksMap(t, settings)
				assertHookArrayLen(t, hooks, "Stop", 2)
				assertHookArrayLen(t, hooks, "UserPromptSubmit", 1)
				assertHookArrayLen(t, hooks, "PreToolUse", 1)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mergeSettingsWithAgencHooks([]byte(tt.inputJSON))
			if err != nil {
				t.Fatalf("mergeSettingsWithAgencHooks returned error: %v", err)
			}

			var settings map[string]json.RawMessage
			if err := json.Unmarshal(result, &settings); err != nil {
				t.Fatalf("failed to parse merged output: %v", err)
			}

			tt.checkMerged(t, settings)
		})
	}
}

func TestMergeSettingsWithAgencHooks_InvalidJSON(t *testing.T) {
	_, err := mergeSettingsWithAgencHooks([]byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestEnsureSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	srcFilepath := filepath.Join(tmpDir, "source.txt")
	if err := os.WriteFile(srcFilepath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	dstFilepath := filepath.Join(tmpDir, "link.txt")

	// Create symlink where none exists
	if err := ensureSymlink(dstFilepath, srcFilepath); err != nil {
		t.Fatalf("ensureSymlink (create) failed: %v", err)
	}
	assertSymlinkTarget(t, dstFilepath, srcFilepath)

	// Calling again is a no-op
	if err := ensureSymlink(dstFilepath, srcFilepath); err != nil {
		t.Fatalf("ensureSymlink (idempotent) failed: %v", err)
	}
	assertSymlinkTarget(t, dstFilepath, srcFilepath)

	// Change target
	newSrcFilepath := filepath.Join(tmpDir, "other.txt")
	if err := os.WriteFile(newSrcFilepath, []byte("world"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureSymlink(dstFilepath, newSrcFilepath); err != nil {
		t.Fatalf("ensureSymlink (retarget) failed: %v", err)
	}
	assertSymlinkTarget(t, dstFilepath, newSrcFilepath)

	// Replace a regular file with a symlink
	regularFilepath := filepath.Join(tmpDir, "regular.txt")
	if err := os.WriteFile(regularFilepath, []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ensureSymlink(regularFilepath, srcFilepath); err != nil {
		t.Fatalf("ensureSymlink (replace regular file) failed: %v", err)
	}
	assertSymlinkTarget(t, regularFilepath, srcFilepath)
}

func TestRemoveSymlinkIfPresent(t *testing.T) {
	tmpDir := t.TempDir()

	// Removing a nonexistent path is a no-op
	if err := removeSymlinkIfPresent(filepath.Join(tmpDir, "nonexistent")); err != nil {
		t.Fatalf("removeSymlinkIfPresent (nonexistent) failed: %v", err)
	}

	// Create a symlink and remove it
	srcFilepath := filepath.Join(tmpDir, "source.txt")
	if err := os.WriteFile(srcFilepath, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	linkFilepath := filepath.Join(tmpDir, "link.txt")
	if err := os.Symlink(srcFilepath, linkFilepath); err != nil {
		t.Fatal(err)
	}
	if err := removeSymlinkIfPresent(linkFilepath); err != nil {
		t.Fatalf("removeSymlinkIfPresent (symlink) failed: %v", err)
	}
	if _, err := os.Lstat(linkFilepath); !os.IsNotExist(err) {
		t.Error("symlink should have been removed")
	}

	// A regular file is NOT removed
	regularFilepath := filepath.Join(tmpDir, "regular.txt")
	if err := os.WriteFile(regularFilepath, []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := removeSymlinkIfPresent(regularFilepath); err != nil {
		t.Fatalf("removeSymlinkIfPresent (regular file) failed: %v", err)
	}
	if _, err := os.Stat(regularFilepath); err != nil {
		t.Error("regular file should NOT have been removed")
	}
}

func TestSyncSymlinks(t *testing.T) {
	userDir := t.TempDir()
	agencDir := t.TempDir()

	// Create some source items in userDir
	if err := os.WriteFile(filepath.Join(userDir, "CLAUDE.md"), []byte("# Claude"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(userDir, "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	// "commands", "agents", "plugins" do NOT exist in userDir

	if err := syncSymlinks(userDir, agencDir); err != nil {
		t.Fatalf("syncSymlinks failed: %v", err)
	}

	// CLAUDE.md and skills should be symlinked
	assertSymlinkTarget(t, filepath.Join(agencDir, "CLAUDE.md"), filepath.Join(userDir, "CLAUDE.md"))
	assertSymlinkTarget(t, filepath.Join(agencDir, "skills"), filepath.Join(userDir, "skills"))

	// commands, agents, plugins should NOT exist
	for _, name := range []string{"commands", "agents", "plugins"} {
		if _, err := os.Lstat(filepath.Join(agencDir, name)); !os.IsNotExist(err) {
			t.Errorf("expected %s to not exist, but it does", name)
		}
	}

	// Now create "commands" in userDir and remove "CLAUDE.md"
	if err := os.MkdirAll(filepath.Join(userDir, "commands"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(userDir, "CLAUDE.md")); err != nil {
		t.Fatal(err)
	}

	if err := syncSymlinks(userDir, agencDir); err != nil {
		t.Fatalf("syncSymlinks (second run) failed: %v", err)
	}

	// CLAUDE.md symlink should be removed, commands should be symlinked
	if _, err := os.Lstat(filepath.Join(agencDir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("CLAUDE.md symlink should have been removed")
	}
	assertSymlinkTarget(t, filepath.Join(agencDir, "commands"), filepath.Join(userDir, "commands"))
}

func TestSyncSettings(t *testing.T) {
	userDir := t.TempDir()
	agencDir := t.TempDir()

	// Write a user settings file
	userSettings := `{"permissions": {"allow": ["Read(./**)"]}, "hooks": {"Stop": [{"hooks": [{"type": "command", "command": "echo done"}]}]}}`
	if err := os.WriteFile(filepath.Join(userDir, settingsFilename), []byte(userSettings), 0644); err != nil {
		t.Fatal(err)
	}

	// First sync should write the file
	if err := syncSettings(userDir, agencDir); err != nil {
		t.Fatalf("syncSettings failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(agencDir, settingsFilename))
	if err != nil {
		t.Fatalf("failed to read agenc settings: %v", err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("failed to parse agenc settings: %v", err)
	}

	// Check hooks
	hooks := parseHooksMap(t, settings)
	assertHookArrayLen(t, hooks, "Stop", 2)
	assertHookArrayLen(t, hooks, "UserPromptSubmit", 1)

	// Check permissions preserved
	if _, ok := settings["permissions"]; !ok {
		t.Error("permissions field was lost")
	}

	// Second sync with same input should not rewrite (mtime preserved)
	info1, _ := os.Stat(filepath.Join(agencDir, settingsFilename))
	if err := syncSettings(userDir, agencDir); err != nil {
		t.Fatalf("syncSettings (second) failed: %v", err)
	}
	info2, _ := os.Stat(filepath.Join(agencDir, settingsFilename))
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("file was rewritten despite no changes")
	}
}

func TestSyncSettings_NoUserFile(t *testing.T) {
	userDir := t.TempDir()
	agencDir := t.TempDir()

	// No settings.json in userDir -- should produce agenc-only hooks
	if err := syncSettings(userDir, agencDir); err != nil {
		t.Fatalf("syncSettings failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(agencDir, settingsFilename))
	if err != nil {
		t.Fatalf("failed to read agenc settings: %v", err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("failed to parse agenc settings: %v", err)
	}

	hooks := parseHooksMap(t, settings)
	assertHookArrayLen(t, hooks, "Stop", 1)
	assertHookArrayLen(t, hooks, "UserPromptSubmit", 1)
}

// --- test helpers ---

func parseHooksMap(t *testing.T, settings map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	hooksRaw, ok := settings["hooks"]
	if !ok {
		t.Fatal("merged settings missing 'hooks' key")
	}
	var hooks map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
		t.Fatalf("failed to parse hooks map: %v", err)
	}
	return hooks
}

func assertHookArrayLen(t *testing.T, hooks map[string]json.RawMessage, hookName string, expectedLen int) {
	t.Helper()
	raw, ok := hooks[hookName]
	if !ok {
		t.Fatalf("hooks missing '%s' key", hookName)
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		t.Fatalf("failed to parse '%s' array: %v", hookName, err)
	}
	if len(arr) != expectedLen {
		t.Errorf("expected %s array length %d, got %d", hookName, expectedLen, len(arr))
	}
}

func assertSymlinkTarget(t *testing.T, linkPath string, expectedTarget string) {
	t.Helper()
	target, err := os.Readlink(linkPath)
	if err != nil {
		t.Fatalf("failed to readlink '%s': %v", linkPath, err)
	}
	if target != expectedTarget {
		t.Errorf("symlink '%s' points to '%s', expected '%s'", linkPath, target, expectedTarget)
	}
}
