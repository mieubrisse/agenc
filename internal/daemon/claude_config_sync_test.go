package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const testAgencDirpath = "/tmp/test-agenc"

func TestMergeSettingsWithAgencOverrides(t *testing.T) {
	tests := []struct {
		name        string
		inputJSON   string
		checkMerged func(t *testing.T, settings map[string]json.RawMessage)
	}{
		{
			name:      "empty settings gets agenc hooks and deny permissions",
			inputJSON: `{}`,
			checkMerged: func(t *testing.T, settings map[string]json.RawMessage) {
				hooks := parseHooksMap(t, settings)
				assertHookArrayLen(t, hooks, "Stop", 1)
				assertHookArrayLen(t, hooks, "UserPromptSubmit", 1)
				assertDenyContainsAgencEntries(t, settings)
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
				assertDenyContainsAgencEntries(t, settings)
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
			name: "non-hooks fields are preserved and deny permissions added",
			inputJSON: `{
				"permissions": {"allow": ["Read(./**)"]},
				"enabledPlugins": {"foo": true}
			}`,
			checkMerged: func(t *testing.T, settings map[string]json.RawMessage) {
				if _, ok := settings["enabledPlugins"]; !ok {
					t.Error("enabledPlugins field was lost")
				}
				hooks := parseHooksMap(t, settings)
				assertHookArrayLen(t, hooks, "Stop", 1)
				assertHookArrayLen(t, hooks, "UserPromptSubmit", 1)
				// permissions.allow should be preserved
				perms := parsePermsMap(t, settings)
				if _, ok := perms["allow"]; !ok {
					t.Error("permissions.allow was lost")
				}
				assertDenyContainsAgencEntries(t, settings)
			},
		},
		{
			name: "existing deny entries are preserved and agenc entries appended",
			inputJSON: `{
				"permissions": {"deny": ["Read(./.env)", "Bash(rm -rf:*)"]}
			}`,
			checkMerged: func(t *testing.T, settings map[string]json.RawMessage) {
				deny := parseDenyArray(t, settings)
				// Should contain the 2 original + 5 agenc entries
				expectedLen := 2 + len(agencDenyPermissionTools)
				if len(deny) != expectedLen {
					t.Errorf("expected deny array length %d, got %d", expectedLen, len(deny))
				}
				assertDenyContains(t, deny, "Read(./.env)")
				assertDenyContains(t, deny, "Bash(rm -rf:*)")
				assertDenyContainsAgencEntries(t, settings)
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
				if _, ok := settings["enabledPlugins"]; !ok {
					t.Error("enabledPlugins field was lost")
				}
				hooks := parseHooksMap(t, settings)
				assertHookArrayLen(t, hooks, "Stop", 2)
				assertHookArrayLen(t, hooks, "UserPromptSubmit", 1)
				assertHookArrayLen(t, hooks, "PreToolUse", 1)
				assertDenyContainsAgencEntries(t, settings)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := mergeSettingsWithAgencOverrides([]byte(tt.inputJSON), testAgencDirpath)
			if err != nil {
				t.Fatalf("mergeSettingsWithAgencOverrides returned error: %v", err)
			}

			var settings map[string]json.RawMessage
			if err := json.Unmarshal(result, &settings); err != nil {
				t.Fatalf("failed to parse merged output: %v", err)
			}

			tt.checkMerged(t, settings)
		})
	}
}

func TestMergeSettingsWithAgencOverrides_InvalidJSON(t *testing.T) {
	_, err := mergeSettingsWithAgencOverrides([]byte(`not json`), testAgencDirpath)
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
	if err := os.MkdirAll(filepath.Join(userDir, "skills"), 0755); err != nil {
		t.Fatal(err)
	}
	// "commands", "agents", "plugins" do NOT exist in userDir

	if err := syncSymlinks(userDir, agencDir); err != nil {
		t.Fatalf("syncSymlinks failed: %v", err)
	}

	// skills should be symlinked
	assertSymlinkTarget(t, filepath.Join(agencDir, "skills"), filepath.Join(userDir, "skills"))

	// commands, agents, plugins should NOT exist
	for _, name := range []string{"commands", "agents", "plugins"} {
		if _, err := os.Lstat(filepath.Join(agencDir, name)); !os.IsNotExist(err) {
			t.Errorf("expected %s to not exist, but it does", name)
		}
	}

	// CLAUDE.md should NOT be symlinked (it's now merged, not symlinked)
	if _, err := os.Lstat(filepath.Join(agencDir, "CLAUDE.md")); !os.IsNotExist(err) {
		t.Error("CLAUDE.md should not be symlinked")
	}

	// Now create "commands" in userDir
	if err := os.MkdirAll(filepath.Join(userDir, "commands"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := syncSymlinks(userDir, agencDir); err != nil {
		t.Fatalf("syncSymlinks (second run) failed: %v", err)
	}

	// commands should now be symlinked
	assertSymlinkTarget(t, filepath.Join(agencDir, "commands"), filepath.Join(userDir, "commands"))
}

func TestSyncSettings(t *testing.T) {
	userDir := t.TempDir()
	modsDir := t.TempDir()
	agencDir := t.TempDir()

	// Write a user settings file
	userSettings := `{"permissions": {"allow": ["Read(./**)"]}, "hooks": {"Stop": [{"hooks": [{"type": "command", "command": "echo done"}]}]}}`
	if err := os.WriteFile(filepath.Join(userDir, settingsFilename), []byte(userSettings), 0644); err != nil {
		t.Fatal(err)
	}

	// Empty mods settings
	if err := os.WriteFile(filepath.Join(modsDir, settingsFilename), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	// First sync should write the file
	if err := syncSettings(userDir, modsDir, agencDir, testAgencDirpath); err != nil {
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
	if err := syncSettings(userDir, modsDir, agencDir, testAgencDirpath); err != nil {
		t.Fatalf("syncSettings (second) failed: %v", err)
	}
	info2, _ := os.Stat(filepath.Join(agencDir, settingsFilename))
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("file was rewritten despite no changes")
	}
}

func TestSyncSettings_NoUserFile(t *testing.T) {
	userDir := t.TempDir()
	modsDir := t.TempDir()
	agencDir := t.TempDir()

	// No settings.json in userDir or modsDir — should produce agenc-only hooks
	if err := syncSettings(userDir, modsDir, agencDir, testAgencDirpath); err != nil {
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

func TestSyncSettings_WithModifications(t *testing.T) {
	userDir := t.TempDir()
	modsDir := t.TempDir()
	agencDir := t.TempDir()

	// User has permissions.allow
	userSettings := `{"permissions": {"allow": ["Read(./**)"]}}`
	if err := os.WriteFile(filepath.Join(userDir, settingsFilename), []byte(userSettings), 0644); err != nil {
		t.Fatal(err)
	}

	// Agenc-mods has permissions.deny
	modsSettings := `{"permissions": {"deny": ["Write(/etc/**)"]}}`
	if err := os.WriteFile(filepath.Join(modsDir, settingsFilename), []byte(modsSettings), 0644); err != nil {
		t.Fatal(err)
	}

	if err := syncSettings(userDir, modsDir, agencDir, testAgencDirpath); err != nil {
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

	// Check that permissions contains both allow and deny
	permRaw, ok := settings["permissions"]
	if !ok {
		t.Fatal("permissions field missing")
	}
	var perms map[string]json.RawMessage
	if err := json.Unmarshal(permRaw, &perms); err != nil {
		t.Fatalf("failed to parse permissions: %v", err)
	}
	if _, ok := perms["allow"]; !ok {
		t.Error("permissions.allow was lost")
	}
	if _, ok := perms["deny"]; !ok {
		t.Error("permissions.deny from agenc-mods was not merged in")
	}

	// Hooks should still be present
	hooks := parseHooksMap(t, settings)
	assertHookArrayLen(t, hooks, "Stop", 1)
	assertHookArrayLen(t, hooks, "UserPromptSubmit", 1)
}

func TestDeepMergeJSON(t *testing.T) {
	tests := []struct {
		name     string
		base     string
		overlay  string
		checkFn  func(t *testing.T, result map[string]json.RawMessage)
	}{
		{
			name:    "disjoint keys merge",
			base:    `{"a": 1}`,
			overlay: `{"b": 2}`,
			checkFn: func(t *testing.T, result map[string]json.RawMessage) {
				assertJSONKey(t, result, "a", "1")
				assertJSONKey(t, result, "b", "2")
			},
		},
		{
			name:    "nested objects merge recursively",
			base:    `{"obj": {"a": 1, "b": 2}}`,
			overlay: `{"obj": {"b": 3, "c": 4}}`,
			checkFn: func(t *testing.T, result map[string]json.RawMessage) {
				var obj map[string]json.RawMessage
				if err := json.Unmarshal(result["obj"], &obj); err != nil {
					t.Fatal(err)
				}
				assertJSONKey(t, obj, "a", "1")
				assertJSONKey(t, obj, "b", "3") // overlay wins
				assertJSONKey(t, obj, "c", "4")
			},
		},
		{
			name:    "arrays concatenate",
			base:    `{"arr": [1, 2]}`,
			overlay: `{"arr": [3, 4]}`,
			checkFn: func(t *testing.T, result map[string]json.RawMessage) {
				var arr []json.RawMessage
				if err := json.Unmarshal(result["arr"], &arr); err != nil {
					t.Fatal(err)
				}
				if len(arr) != 4 {
					t.Errorf("expected array length 4, got %d", len(arr))
				}
			},
		},
		{
			name:    "scalar overlay wins",
			base:    `{"key": "base"}`,
			overlay: `{"key": "overlay"}`,
			checkFn: func(t *testing.T, result map[string]json.RawMessage) {
				assertJSONKey(t, result, "key", `"overlay"`)
			},
		},
		{
			name:    "type mismatch object vs scalar overlay wins",
			base:    `{"key": {"nested": true}}`,
			overlay: `{"key": "scalar"}`,
			checkFn: func(t *testing.T, result map[string]json.RawMessage) {
				assertJSONKey(t, result, "key", `"scalar"`)
			},
		},
		{
			name:    "empty base",
			base:    `{}`,
			overlay: `{"a": 1}`,
			checkFn: func(t *testing.T, result map[string]json.RawMessage) {
				assertJSONKey(t, result, "a", "1")
			},
		},
		{
			name:    "empty overlay",
			base:    `{"a": 1}`,
			overlay: `{}`,
			checkFn: func(t *testing.T, result map[string]json.RawMessage) {
				assertJSONKey(t, result, "a", "1")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var base map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tt.base), &base); err != nil {
				t.Fatal(err)
			}
			var overlay map[string]json.RawMessage
			if err := json.Unmarshal([]byte(tt.overlay), &overlay); err != nil {
				t.Fatal(err)
			}

			result, err := deepMergeJSON(base, overlay)
			if err != nil {
				t.Fatalf("deepMergeJSON returned error: %v", err)
			}

			tt.checkFn(t, result)
		})
	}
}

func TestSyncClaudeMd(t *testing.T) {
	t.Run("both files exist", func(t *testing.T) {
		userDir := t.TempDir()
		modsDir := t.TempDir()
		agencDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(userDir, "CLAUDE.md"), []byte("User content"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(modsDir, "CLAUDE.md"), []byte("Agenc content"), 0644); err != nil {
			t.Fatal(err)
		}

		if err := syncClaudeMd(userDir, modsDir, agencDir); err != nil {
			t.Fatal(err)
		}

		data, err := os.ReadFile(filepath.Join(agencDir, "CLAUDE.md"))
		if err != nil {
			t.Fatal(err)
		}

		expected := "User content\n\nAgenc content\n"
		if string(data) != expected {
			t.Errorf("expected %q, got %q", expected, string(data))
		}
	})

	t.Run("only user file exists", func(t *testing.T) {
		userDir := t.TempDir()
		modsDir := t.TempDir()
		agencDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(userDir, "CLAUDE.md"), []byte("User only"), 0644); err != nil {
			t.Fatal(err)
		}

		if err := syncClaudeMd(userDir, modsDir, agencDir); err != nil {
			t.Fatal(err)
		}

		data, err := os.ReadFile(filepath.Join(agencDir, "CLAUDE.md"))
		if err != nil {
			t.Fatal(err)
		}

		expected := "User only\n"
		if string(data) != expected {
			t.Errorf("expected %q, got %q", expected, string(data))
		}
	})

	t.Run("only agenc-mods file exists", func(t *testing.T) {
		userDir := t.TempDir()
		modsDir := t.TempDir()
		agencDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(modsDir, "CLAUDE.md"), []byte("Mods only"), 0644); err != nil {
			t.Fatal(err)
		}

		if err := syncClaudeMd(userDir, modsDir, agencDir); err != nil {
			t.Fatal(err)
		}

		data, err := os.ReadFile(filepath.Join(agencDir, "CLAUDE.md"))
		if err != nil {
			t.Fatal(err)
		}

		expected := "Mods only\n"
		if string(data) != expected {
			t.Errorf("expected %q, got %q", expected, string(data))
		}
	})

	t.Run("neither exists removes target", func(t *testing.T) {
		userDir := t.TempDir()
		modsDir := t.TempDir()
		agencDir := t.TempDir()

		// Pre-create target to verify it gets removed
		destFilepath := filepath.Join(agencDir, "CLAUDE.md")
		if err := os.WriteFile(destFilepath, []byte("stale"), 0644); err != nil {
			t.Fatal(err)
		}

		if err := syncClaudeMd(userDir, modsDir, agencDir); err != nil {
			t.Fatal(err)
		}

		if _, err := os.Stat(destFilepath); !os.IsNotExist(err) {
			t.Error("CLAUDE.md should have been removed when both sources are empty")
		}
	})

	t.Run("stale symlink replaced with regular file", func(t *testing.T) {
		userDir := t.TempDir()
		modsDir := t.TempDir()
		agencDir := t.TempDir()

		userFilepath := filepath.Join(userDir, "CLAUDE.md")
		if err := os.WriteFile(userFilepath, []byte("User content"), 0644); err != nil {
			t.Fatal(err)
		}

		// Simulate old version: destination is a symlink to user's file
		destFilepath := filepath.Join(agencDir, "CLAUDE.md")
		if err := os.Symlink(userFilepath, destFilepath); err != nil {
			t.Fatal(err)
		}

		if err := syncClaudeMd(userDir, modsDir, agencDir); err != nil {
			t.Fatal(err)
		}

		// Destination should now be a regular file, not a symlink
		info, err := os.Lstat(destFilepath)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			t.Error("destination should be a regular file, not a symlink")
		}

		// User's original file should be untouched
		userData, err := os.ReadFile(userFilepath)
		if err != nil {
			t.Fatal(err)
		}
		if string(userData) != "User content" {
			t.Errorf("user's source file was corrupted: got %q", string(userData))
		}
	})

	t.Run("mtime preserved when unchanged", func(t *testing.T) {
		userDir := t.TempDir()
		modsDir := t.TempDir()
		agencDir := t.TempDir()

		if err := os.WriteFile(filepath.Join(userDir, "CLAUDE.md"), []byte("Content"), 0644); err != nil {
			t.Fatal(err)
		}

		// First sync
		if err := syncClaudeMd(userDir, modsDir, agencDir); err != nil {
			t.Fatal(err)
		}

		destFilepath := filepath.Join(agencDir, "CLAUDE.md")
		info1, err := os.Stat(destFilepath)
		if err != nil {
			t.Fatal(err)
		}

		// Second sync — same content
		if err := syncClaudeMd(userDir, modsDir, agencDir); err != nil {
			t.Fatal(err)
		}

		info2, err := os.Stat(destFilepath)
		if err != nil {
			t.Fatal(err)
		}

		if !info1.ModTime().Equal(info2.ModTime()) {
			t.Error("file was rewritten despite no changes")
		}
	})
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

func assertJSONKey(t *testing.T, m map[string]json.RawMessage, key string, expectedRaw string) {
	t.Helper()
	raw, ok := m[key]
	if !ok {
		t.Fatalf("key '%s' not found in map", key)
	}
	if string(raw) != expectedRaw {
		t.Errorf("key '%s': expected %s, got %s", key, expectedRaw, string(raw))
	}
}

func parsePermsMap(t *testing.T, settings map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	permsRaw, ok := settings["permissions"]
	if !ok {
		t.Fatal("merged settings missing 'permissions' key")
	}
	var perms map[string]json.RawMessage
	if err := json.Unmarshal(permsRaw, &perms); err != nil {
		t.Fatalf("failed to parse permissions map: %v", err)
	}
	return perms
}

func parseDenyArray(t *testing.T, settings map[string]json.RawMessage) []string {
	t.Helper()
	perms := parsePermsMap(t, settings)
	denyRaw, ok := perms["deny"]
	if !ok {
		t.Fatal("permissions missing 'deny' key")
	}
	var deny []string
	if err := json.Unmarshal(denyRaw, &deny); err != nil {
		t.Fatalf("failed to parse deny array: %v", err)
	}
	return deny
}

func assertDenyContains(t *testing.T, deny []string, entry string) {
	t.Helper()
	for _, d := range deny {
		if d == entry {
			return
		}
	}
	t.Errorf("deny array does not contain expected entry %q", entry)
}

func assertDenyContainsAgencEntries(t *testing.T, settings map[string]json.RawMessage) {
	t.Helper()
	deny := parseDenyArray(t, settings)
	for _, expected := range buildRepoLibraryDenyEntries(testAgencDirpath) {
		assertDenyContains(t, deny, expected)
	}
}
