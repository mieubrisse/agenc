package server

import (
	"encoding/json"
	"testing"

	"github.com/odyssey/agenc/internal/claudeconfig"
)

const testAgencDirpath = "/tmp/test-agenc"
const testAgentDirpath = "/tmp/test-agenc/missions/test-mission/agent"
const testClaudeConfigDirpath = "/tmp/test-agenc/missions/test-mission/claude-config"

func TestMergeSettingsWithAgencOverrides(t *testing.T) {
	tests := []struct {
		name        string
		inputJSON   string
		checkMerged func(t *testing.T, settings map[string]json.RawMessage)
	}{
		{
			name:      "empty settings gets agenc hooks and allow/deny permissions",
			inputJSON: `{}`,
			checkMerged: func(t *testing.T, settings map[string]json.RawMessage) {
				hooks := parseHooksMap(t, settings)
				assertHookArrayLen(t, hooks, "Stop", 1)
				assertHookArrayLen(t, hooks, "UserPromptSubmit", 1)
				assertAllowContainsAgentDirEntries(t, settings)
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
			name: "non-hooks fields are preserved and allow/deny permissions added",
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
				// existing permissions.allow should be preserved alongside agenc entries
				allow := parseAllowArray(t, settings)
				assertAllowContains(t, allow, "Read(./**)")
				assertAllowContainsAgentDirEntries(t, settings)
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
				// Should contain the 2 original + repo library entries + claude-config entries
				expectedLen := 2 + 2*len(claudeconfig.AgencDenyPermissionTools)
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
			result, err := claudeconfig.MergeSettingsWithAgencOverrides([]byte(tt.inputJSON), testAgencDirpath, testAgentDirpath, testClaudeConfigDirpath)
			if err != nil {
				t.Fatalf("claudeconfig.MergeSettingsWithAgencOverrides returned error: %v", err)
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
	_, err := claudeconfig.MergeSettingsWithAgencOverrides([]byte(`not json`), testAgencDirpath, testAgentDirpath, testClaudeConfigDirpath)
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}

func TestDeepMergeJSON(t *testing.T) {
	tests := []struct {
		name    string
		base    string
		overlay string
		checkFn func(t *testing.T, result map[string]json.RawMessage)
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

			result, err := claudeconfig.DeepMergeJSON(base, overlay)
			if err != nil {
				t.Fatalf("claudeconfig.DeepMergeJSON returned error: %v", err)
			}

			tt.checkFn(t, result)
		})
	}
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

func parseAllowArray(t *testing.T, settings map[string]json.RawMessage) []string {
	t.Helper()
	perms := parsePermsMap(t, settings)
	allowRaw, ok := perms["allow"]
	if !ok {
		t.Fatal("permissions missing 'allow' key")
	}
	var allow []string
	if err := json.Unmarshal(allowRaw, &allow); err != nil {
		t.Fatalf("failed to parse allow array: %v", err)
	}
	return allow
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

func assertAllowContains(t *testing.T, allow []string, entry string) {
	t.Helper()
	for _, a := range allow {
		if a == entry {
			return
		}
	}
	t.Errorf("allow array does not contain expected entry %q", entry)
}

func assertAllowContainsAgentDirEntries(t *testing.T, settings map[string]json.RawMessage) {
	t.Helper()
	allow := parseAllowArray(t, settings)
	for _, expected := range claudeconfig.BuildAgentDirAllowEntries(testAgentDirpath) {
		assertAllowContains(t, allow, expected)
	}
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
	for _, expected := range claudeconfig.BuildRepoLibraryDenyEntries(testAgencDirpath) {
		assertDenyContains(t, deny, expected)
	}
	for _, expected := range claudeconfig.BuildClaudeConfigDenyEntries(testClaudeConfigDirpath) {
		assertDenyContains(t, deny, expected)
	}
}
