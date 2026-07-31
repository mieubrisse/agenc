package claudeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

// buildOperationalSettingsForTest calls BuildOperationalSettings with standard
// test paths and returns the parsed top-level settings map. Fails the test if
// the call errors or the output is not valid JSON.
func buildOperationalSettingsForTest(t *testing.T) map[string]json.RawMessage {
	t.Helper()

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	agencDirpath := filepath.Join(homeDir, ".agenc")
	agentDirpath := filepath.Join(homeDir, ".agenc", "missions", "test-uuid", "agent")
	hookScriptsBaseDirpath := filepath.Join(homeDir, ".agenc", "missions", "test-uuid")

	output, err := BuildOperationalSettings(agencDirpath, agentDirpath, hookScriptsBaseDirpath)
	if err != nil {
		t.Fatalf("BuildOperationalSettings returned unexpected error: %v", err)
	}

	// Output must end with a trailing newline
	if len(output) == 0 || output[len(output)-1] != '\n' {
		t.Error("output does not end with trailing newline")
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(output, &settings); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}

	return settings
}

func TestBuildOperationalSettings_ValidJSON(t *testing.T) {
	buildOperationalSettingsForTest(t)
}

func TestBuildOperationalSettings_HookKeys(t *testing.T) {
	settings := buildOperationalSettingsForTest(t)

	hooksRaw, ok := settings["hooks"]
	if !ok {
		t.Fatal("missing 'hooks' key in output")
	}

	var hooks map[string]json.RawMessage
	if err := json.Unmarshal(hooksRaw, &hooks); err != nil {
		t.Fatalf("failed to parse hooks: %v", err)
	}

	expectedHookKeys := []string{
		"SessionStart",
		"Stop",
		"UserPromptSubmit",
		"Notification",
		"PostToolUse",
		"PostToolUseFailure",
		"PreToolUse",
	}
	for _, key := range expectedHookKeys {
		if _, present := hooks[key]; !present {
			t.Errorf("missing expected hook key: %q", key)
		}
	}
}

func TestBuildOperationalSettings_SessionStartCommandIsAgencPrime(t *testing.T) {
	settings := buildOperationalSettingsForTest(t)

	var hooks map[string]json.RawMessage
	if err := json.Unmarshal(settings["hooks"], &hooks); err != nil {
		t.Fatalf("failed to parse hooks: %v", err)
	}

	sessionStartRaw, ok := hooks["SessionStart"]
	if !ok {
		t.Fatal("missing SessionStart hook")
	}
	if !strings.Contains(string(sessionStartRaw), `"agenc prime"`) {
		t.Errorf("expected SessionStart command to be \"agenc prime\", got: %s", string(sessionStartRaw))
	}
}

func TestBuildOperationalSettings_PreToolUseReferencesGuardScript(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	hookScriptsBaseDirpath := filepath.Join(homeDir, ".agenc", "missions", "test-uuid")
	settings := buildOperationalSettingsForTest(t)

	var hooks map[string]json.RawMessage
	if err := json.Unmarshal(settings["hooks"], &hooks); err != nil {
		t.Fatalf("failed to parse hooks: %v", err)
	}

	preToolUseRaw, ok := hooks["PreToolUse"]
	if !ok {
		t.Fatal("missing PreToolUse hook")
	}
	expectedScriptPath := filepath.Join(hookScriptsBaseDirpath, AgencHooksDirname, RepoLibraryGuardScriptName)
	if !strings.Contains(string(preToolUseRaw), expectedScriptPath) {
		t.Errorf("expected PreToolUse command to reference %q, got: %s", expectedScriptPath, string(preToolUseRaw))
	}
}

func TestBuildOperationalSettings_AllowIncludesAgentDir(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	agentDirpath := filepath.Join(homeDir, ".agenc", "missions", "test-uuid", "agent")
	settings := buildOperationalSettingsForTest(t)

	var perms map[string]json.RawMessage
	if err := json.Unmarshal(settings["permissions"], &perms); err != nil {
		t.Fatalf("failed to parse permissions: %v", err)
	}

	var allowEntries []string
	if err := json.Unmarshal(perms["allow"], &allowEntries); err != nil {
		t.Fatalf("failed to parse allow entries: %v", err)
	}

	for _, entry := range allowEntries {
		if strings.Contains(entry, agentDirpath) {
			return
		}
	}
	t.Errorf("expected at least one allow entry referencing agentDirpath %q", agentDirpath)
}

func TestBuildOperationalSettings_DenyIncludesRepoLibrary(t *testing.T) {
	settings := buildOperationalSettingsForTest(t)

	var perms map[string]json.RawMessage
	if err := json.Unmarshal(settings["permissions"], &perms); err != nil {
		t.Fatalf("failed to parse permissions: %v", err)
	}

	var denyEntries []string
	if err := json.Unmarshal(perms["deny"], &denyEntries); err != nil {
		t.Fatalf("failed to parse deny entries: %v", err)
	}

	for _, entry := range denyEntries {
		if strings.Contains(entry, "repos") {
			return
		}
	}
	t.Error("expected at least one deny entry referencing the repo library")
}

func TestBuildOperationalSettings_DenyExcludesClaudeConfigItems(t *testing.T) {
	settings := buildOperationalSettingsForTest(t)

	var perms map[string]json.RawMessage
	if err := json.Unmarshal(settings["permissions"], &perms); err != nil {
		t.Fatalf("failed to parse permissions: %v", err)
	}

	var denyEntries []string
	if err := json.Unmarshal(perms["deny"], &denyEntries); err != nil {
		t.Fatalf("failed to parse deny entries: %v", err)
	}

	for _, entry := range denyEntries {
		if strings.Contains(entry, "CLAUDE.md") {
			t.Errorf("unexpected claude-config deny entry for CLAUDE.md: %q", entry)
		}
		if strings.Contains(entry, "settings.json") {
			t.Errorf("unexpected claude-config deny entry for settings.json: %q", entry)
		}
	}
}

func TestBuildOperationalSettings_SandboxIncludesServerSocket(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	agencDirpath := filepath.Join(homeDir, ".agenc")
	settings := buildOperationalSettingsForTest(t)

	var sandbox map[string]json.RawMessage
	if err := json.Unmarshal(settings["sandbox"], &sandbox); err != nil {
		t.Fatalf("failed to parse sandbox: %v", err)
	}

	var network map[string]json.RawMessage
	if err := json.Unmarshal(sandbox["network"], &network); err != nil {
		t.Fatalf("failed to parse sandbox.network: %v", err)
	}

	var sockets []string
	if err := json.Unmarshal(network["allowUnixSockets"], &sockets); err != nil {
		t.Fatalf("failed to parse allowUnixSockets: %v", err)
	}

	expectedSocket := config.GetServerSocketFilepath(agencDirpath)
	for _, s := range sockets {
		if s == expectedSocket {
			return
		}
	}
	t.Errorf("expected server socket %q in allowUnixSockets, got: %v", expectedSocket, sockets)
}

func TestBuildOperationalSettings_NoUserSettingsKeys(t *testing.T) {
	settings := buildOperationalSettingsForTest(t)

	allowedKeys := map[string]bool{
		"hooks":       true,
		"permissions": true,
		"sandbox":     true,
	}
	for key := range settings {
		if !allowedKeys[key] {
			t.Errorf("unexpected top-level key %q — BuildOperationalSettings must not include user-settings keys", key)
		}
	}
}
