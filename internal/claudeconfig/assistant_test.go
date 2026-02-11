package claudeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestBuildAssistantAllowEntries(t *testing.T) {
	agencDirpath := "/home/user/.agenc"
	entries := BuildAssistantAllowEntries(agencDirpath)

	expectedTools := []string{"Read", "Write", "Edit", "Glob", "Grep"}
	expectedPattern := agencDirpath + "/**"

	// One entry per tool + Bash(agenc:*)
	expectedLen := len(expectedTools) + 1
	if len(entries) != expectedLen {
		t.Fatalf("expected %d entries, got %d: %v", expectedLen, len(entries), entries)
	}

	for i, tool := range expectedTools {
		expected := tool + "(" + expectedPattern + ")"
		if entries[i] != expected {
			t.Errorf("entry %d: expected %q, got %q", i, expected, entries[i])
		}
	}

	bashEntry := entries[len(entries)-1]
	if bashEntry != "Bash(agenc:*)" {
		t.Errorf("last entry: expected %q, got %q", "Bash(agenc:*)", bashEntry)
	}
}

func TestBuildAssistantDenyEntries(t *testing.T) {
	agencDirpath := "/home/user/.agenc"
	entries := BuildAssistantDenyEntries(agencDirpath)

	expectedPattern := agencDirpath + "/" + config.MissionsDirname + "/*/" + config.AgentDirname + "/**"

	if len(entries) != 2 {
		t.Fatalf("expected 2 deny entries, got %d: %v", len(entries), entries)
	}

	expectedEntries := []string{
		"Write(" + expectedPattern + ")",
		"Edit(" + expectedPattern + ")",
	}

	for i, expected := range expectedEntries {
		if entries[i] != expected {
			t.Errorf("entry %d: expected %q, got %q", i, expected, entries[i])
		}
	}
}

func TestWriteAssistantAgentConfig(t *testing.T) {
	agencDirpath := "/home/user/.agenc"

	t.Run("writes CLAUDE.md to agent directory", func(t *testing.T) {
		agentDirpath := t.TempDir()

		if err := writeAssistantAgentConfig(agentDirpath, agencDirpath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(agentDirpath, "CLAUDE.md"))
		if err != nil {
			t.Fatalf("failed to read CLAUDE.md: %v", err)
		}

		content := string(data)
		if !strings.Contains(content, "AgenC Assistant Mission") {
			t.Error("expected assistant content in agent/CLAUDE.md")
		}
		if !strings.Contains(content, "CLI quick reference") {
			t.Error("expected CLI quick reference mention in agent/CLAUDE.md")
		}
	})

	t.Run("writes settings.json to agent/.claude directory", func(t *testing.T) {
		agentDirpath := t.TempDir()

		if err := writeAssistantAgentConfig(agentDirpath, agencDirpath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		settingsFilepath := filepath.Join(agentDirpath, config.UserClaudeDirname, "settings.json")
		data, err := os.ReadFile(settingsFilepath)
		if err != nil {
			t.Fatalf("failed to read settings.json: %v", err)
		}

		var settings map[string]json.RawMessage
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatalf("failed to parse settings: %v", err)
		}

		var perms map[string]json.RawMessage
		if err := json.Unmarshal(settings["permissions"], &perms); err != nil {
			t.Fatalf("failed to parse permissions: %v", err)
		}

		var allow []string
		if err := json.Unmarshal(perms["allow"], &allow); err != nil {
			t.Fatalf("failed to parse allow array: %v", err)
		}

		if len(allow) != len(BuildAssistantAllowEntries(agencDirpath)) {
			t.Errorf("expected %d allow entries, got %d", len(BuildAssistantAllowEntries(agencDirpath)), len(allow))
		}

		var deny []string
		if err := json.Unmarshal(perms["deny"], &deny); err != nil {
			t.Fatalf("failed to parse deny array: %v", err)
		}

		if len(deny) != len(BuildAssistantDenyEntries(agencDirpath)) {
			t.Errorf("expected %d deny entries, got %d", len(BuildAssistantDenyEntries(agencDirpath)), len(deny))
		}
	})

	t.Run("settings contains permissions and hooks", func(t *testing.T) {
		agentDirpath := t.TempDir()

		if err := writeAssistantAgentConfig(agentDirpath, agencDirpath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		settingsFilepath := filepath.Join(agentDirpath, config.UserClaudeDirname, "settings.json")
		data, err := os.ReadFile(settingsFilepath)
		if err != nil {
			t.Fatalf("failed to read settings.json: %v", err)
		}

		var settings map[string]json.RawMessage
		if err := json.Unmarshal(data, &settings); err != nil {
			t.Fatalf("failed to parse settings: %v", err)
		}

		if len(settings) != 2 {
			t.Errorf("expected settings to contain 'permissions' and 'hooks' keys, got %d keys", len(settings))
		}

		if _, ok := settings["permissions"]; !ok {
			t.Error("expected 'permissions' key in settings")
		}

		if _, ok := settings["hooks"]; !ok {
			t.Error("expected 'hooks' key in settings")
		}
	})

	t.Run("settings has SessionStart hook running agenc prime", func(t *testing.T) {
		agentDirpath := t.TempDir()

		if err := writeAssistantAgentConfig(agentDirpath, agencDirpath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		settingsFilepath := filepath.Join(agentDirpath, config.UserClaudeDirname, "settings.json")
		data, err := os.ReadFile(settingsFilepath)
		if err != nil {
			t.Fatalf("failed to read settings.json: %v", err)
		}

		// Verify the JSON contains the SessionStart hook with "agenc prime"
		content := string(data)
		if !strings.Contains(content, "SessionStart") {
			t.Error("settings missing SessionStart hook")
		}
		if !strings.Contains(content, "agenc prime") {
			t.Error("SessionStart hook missing 'agenc prime' command")
		}
	})
}

func TestAssistantClaudeMdContent(t *testing.T) {
	t.Run("embedded content is non-empty", func(t *testing.T) {
		if assistantClaudeMdContent == "" {
			t.Fatal("assistantClaudeMdContent is empty")
		}
	})

	t.Run("contains expected sections", func(t *testing.T) {
		expectedPhrases := []string{
			"AgenC Assistant Mission",
			"CLI quick reference",
			"$AGENC_MISSION_UUID",
			"Do NOT modify other missions",
		}
		for _, phrase := range expectedPhrases {
			if !strings.Contains(assistantClaudeMdContent, phrase) {
				t.Errorf("assistant CLAUDE.md missing expected phrase: %q", phrase)
			}
		}
	})
}
