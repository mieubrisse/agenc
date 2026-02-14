package claudeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestBuildAdjutantAllowEntries(t *testing.T) {
	agencDirpath := "/home/user/.agenc"
	entries := BuildAdjutantAllowEntries(agencDirpath)

	expectedTools := []string{"Read", "Write", "Edit", "Glob", "Grep"}
	expectedPattern := agencDirpath + "/**"

	// One entry per tool + Bash(agenc:*) + Bash(gh:*)
	expectedLen := len(expectedTools) + 2
	if len(entries) != expectedLen {
		t.Fatalf("expected %d entries, got %d: %v", expectedLen, len(entries), entries)
	}

	for i, tool := range expectedTools {
		expected := tool + "(" + expectedPattern + ")"
		if entries[i] != expected {
			t.Errorf("entry %d: expected %q, got %q", i, expected, entries[i])
		}
	}

	agencBashEntry := entries[len(expectedTools)]
	if agencBashEntry != "Bash(agenc:*)" {
		t.Errorf("entry %d: expected %q, got %q", len(expectedTools), "Bash(agenc:*)", agencBashEntry)
	}

	ghBashEntry := entries[len(expectedTools)+1]
	if ghBashEntry != "Bash(gh:*)" {
		t.Errorf("entry %d: expected %q, got %q", len(expectedTools)+1, "Bash(gh:*)", ghBashEntry)
	}
}

func TestBuildAdjutantDenyEntries(t *testing.T) {
	agencDirpath := "/home/user/.agenc"
	entries := BuildAdjutantDenyEntries(agencDirpath)

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

func TestWriteAdjutantAgentConfig(t *testing.T) {
	agencDirpath := "/home/user/.agenc"

	t.Run("writes CLAUDE.md to agent directory", func(t *testing.T) {
		agentDirpath := t.TempDir()

		if err := writeAdjutantAgentConfig(agentDirpath, agencDirpath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(agentDirpath, "CLAUDE.md"))
		if err != nil {
			t.Fatalf("failed to read CLAUDE.md: %v", err)
		}

		content := string(data)
		if !strings.Contains(content, "Adjutant Mission") {
			t.Error("expected adjutant content in agent/CLAUDE.md")
		}
		if !strings.Contains(content, "CLI quick reference") {
			t.Error("expected CLI quick reference mention in agent/CLAUDE.md")
		}
	})

	t.Run("writes settings.json to agent/.claude directory", func(t *testing.T) {
		agentDirpath := t.TempDir()

		if err := writeAdjutantAgentConfig(agentDirpath, agencDirpath); err != nil {
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

		if len(allow) != len(BuildAdjutantAllowEntries(agencDirpath)) {
			t.Errorf("expected %d allow entries, got %d", len(BuildAdjutantAllowEntries(agencDirpath)), len(allow))
		}

		var deny []string
		if err := json.Unmarshal(perms["deny"], &deny); err != nil {
			t.Fatalf("failed to parse deny array: %v", err)
		}

		if len(deny) != len(BuildAdjutantDenyEntries(agencDirpath)) {
			t.Errorf("expected %d deny entries, got %d", len(BuildAdjutantDenyEntries(agencDirpath)), len(deny))
		}
	})

	t.Run("settings contains permissions and hooks", func(t *testing.T) {
		agentDirpath := t.TempDir()

		if err := writeAdjutantAgentConfig(agentDirpath, agencDirpath); err != nil {
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

		if err := writeAdjutantAgentConfig(agentDirpath, agencDirpath); err != nil {
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

func TestAdjutantClaudeMdContent(t *testing.T) {
	t.Run("embedded content is non-empty", func(t *testing.T) {
		if adjutantClaudeMdContent == "" {
			t.Fatal("adjutantClaudeMdContent is empty")
		}
	})

	t.Run("contains expected sections", func(t *testing.T) {
		expectedPhrases := []string{
			"Adjutant Mission",
			"CLI quick reference",
			"{{MISSION_UUID_ENV_VAR}}",
			"Do NOT modify other missions",
		}
		for _, phrase := range expectedPhrases {
			if !strings.Contains(adjutantClaudeMdContent, phrase) {
				t.Errorf("adjutant CLAUDE.md missing expected phrase: %q", phrase)
			}
		}
	})

	t.Run("written CLAUDE.md has env var placeholder replaced", func(t *testing.T) {
		agentDirpath := t.TempDir()
		agencDirpath := "/home/user/.agenc"

		if err := writeAdjutantAgentConfig(agentDirpath, agencDirpath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(agentDirpath, "CLAUDE.md"))
		if err != nil {
			t.Fatalf("failed to read CLAUDE.md: %v", err)
		}

		content := string(data)
		if strings.Contains(content, "{{MISSION_UUID_ENV_VAR}}") {
			t.Error("written CLAUDE.md still contains unreplaced placeholder")
		}
		if !strings.Contains(content, "$"+config.MissionUUIDEnvVar) {
			t.Errorf("written CLAUDE.md missing env var reference $%s", config.MissionUUIDEnvVar)
		}
	})
}
