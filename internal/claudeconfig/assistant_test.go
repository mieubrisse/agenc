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

func TestInjectAssistantPermissions(t *testing.T) {
	agencDirpath := "/home/user/.agenc"

	t.Run("injects into empty permissions", func(t *testing.T) {
		input := []byte(`{"permissions": {}}`)
		result, err := injectAssistantPermissions(input, agencDirpath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var settings map[string]json.RawMessage
		if err := json.Unmarshal(result, &settings); err != nil {
			t.Fatalf("failed to parse result: %v", err)
		}

		var perms map[string]json.RawMessage
		if err := json.Unmarshal(settings["permissions"], &perms); err != nil {
			t.Fatalf("failed to parse permissions: %v", err)
		}

		var allow []string
		if err := json.Unmarshal(perms["allow"], &allow); err != nil {
			t.Fatalf("failed to parse allow array: %v", err)
		}

		var deny []string
		if err := json.Unmarshal(perms["deny"], &deny); err != nil {
			t.Fatalf("failed to parse deny array: %v", err)
		}

		if len(allow) != len(BuildAssistantAllowEntries(agencDirpath)) {
			t.Errorf("expected %d allow entries, got %d", len(BuildAssistantAllowEntries(agencDirpath)), len(allow))
		}

		if len(deny) != len(BuildAssistantDenyEntries(agencDirpath)) {
			t.Errorf("expected %d deny entries, got %d", len(BuildAssistantDenyEntries(agencDirpath)), len(deny))
		}
	})

	t.Run("preserves existing permissions", func(t *testing.T) {
		input := []byte(`{"permissions": {"allow": ["Read(foo)"], "deny": ["Write(bar)"]}}`)
		result, err := injectAssistantPermissions(input, agencDirpath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var settings map[string]json.RawMessage
		if err := json.Unmarshal(result, &settings); err != nil {
			t.Fatalf("failed to parse result: %v", err)
		}

		var perms map[string]json.RawMessage
		if err := json.Unmarshal(settings["permissions"], &perms); err != nil {
			t.Fatalf("failed to parse permissions: %v", err)
		}

		var allow []string
		if err := json.Unmarshal(perms["allow"], &allow); err != nil {
			t.Fatalf("failed to parse allow array: %v", err)
		}

		if allow[0] != "Read(foo)" {
			t.Errorf("expected first allow entry to be preserved, got %q", allow[0])
		}

		var deny []string
		if err := json.Unmarshal(perms["deny"], &deny); err != nil {
			t.Fatalf("failed to parse deny array: %v", err)
		}

		if deny[0] != "Write(bar)" {
			t.Errorf("expected first deny entry to be preserved, got %q", deny[0])
		}
	})

	t.Run("creates permissions when missing", func(t *testing.T) {
		input := []byte(`{}`)
		result, err := injectAssistantPermissions(input, agencDirpath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		var settings map[string]json.RawMessage
		if err := json.Unmarshal(result, &settings); err != nil {
			t.Fatalf("failed to parse result: %v", err)
		}

		if _, ok := settings["permissions"]; !ok {
			t.Fatal("expected permissions key in result")
		}
	})
}

func TestBuildAssistantClaudeMd(t *testing.T) {
	t.Run("appends assistant content to merged CLAUDE.md", func(t *testing.T) {
		tmpDirpath := t.TempDir()
		shadowDirpath := filepath.Join(tmpDirpath, "shadow")
		modsDirpath := filepath.Join(tmpDirpath, "mods")
		destDirpath := filepath.Join(tmpDirpath, "dest")

		for _, dirpath := range []string{shadowDirpath, modsDirpath, destDirpath} {
			if err := os.MkdirAll(dirpath, 0755); err != nil {
				t.Fatalf("failed to create dir: %v", err)
			}
		}

		// Write user CLAUDE.md in shadow repo
		userContent := "User Instructions\n================\n\nDo good things.\n"
		if err := os.WriteFile(filepath.Join(shadowDirpath, "CLAUDE.md"), []byte(userContent), 0644); err != nil {
			t.Fatalf("failed to write user CLAUDE.md: %v", err)
		}

		if err := buildAssistantClaudeMd(shadowDirpath, modsDirpath, destDirpath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(destDirpath, "CLAUDE.md"))
		if err != nil {
			t.Fatalf("failed to read CLAUDE.md: %v", err)
		}

		content := string(data)
		if !strings.Contains(content, "User Instructions") {
			t.Error("expected user content to be preserved")
		}
		if !strings.Contains(content, "AgenC Assistant Mission") {
			t.Error("expected assistant content to be appended")
		}
		if !strings.Contains(content, "agenc-self-usage") {
			t.Error("expected assistant content to reference agenc-self-usage skill")
		}
	})

	t.Run("works with empty user CLAUDE.md", func(t *testing.T) {
		tmpDirpath := t.TempDir()
		shadowDirpath := filepath.Join(tmpDirpath, "shadow")
		modsDirpath := filepath.Join(tmpDirpath, "mods")
		destDirpath := filepath.Join(tmpDirpath, "dest")

		for _, dirpath := range []string{shadowDirpath, modsDirpath, destDirpath} {
			if err := os.MkdirAll(dirpath, 0755); err != nil {
				t.Fatalf("failed to create dir: %v", err)
			}
		}

		if err := buildAssistantClaudeMd(shadowDirpath, modsDirpath, destDirpath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		data, err := os.ReadFile(filepath.Join(destDirpath, "CLAUDE.md"))
		if err != nil {
			t.Fatalf("failed to read CLAUDE.md: %v", err)
		}

		content := string(data)
		if !strings.Contains(content, "AgenC Assistant Mission") {
			t.Error("expected assistant content even with no user CLAUDE.md")
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
			"agenc-self-usage",
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
