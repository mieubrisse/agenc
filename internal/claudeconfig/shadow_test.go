package claudeconfig

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRewriteClaudePaths(t *testing.T) {
	targetDirpath := "/Users/testuser/.agenc/missions/abc-123/claude-config"

	// Get home dir for absolute path test cases
	homeDirpath, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get home dir: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "tilde path with trailing slash",
			input:    `"command": "bash ~/.claude/hooks/set-style.sh"`,
			expected: `"command": "bash ` + targetDirpath + `/hooks/set-style.sh"`,
		},
		{
			name:     "tilde path without trailing slash",
			input:    `Read(~/.claude)`,
			expected: `Read(` + targetDirpath + `)`,
		},
		{
			name:     "HOME variable with trailing slash",
			input:    `bash ${HOME}/.claude/hooks/my-hook.sh`,
			expected: `bash ` + targetDirpath + `/hooks/my-hook.sh`,
		},
		{
			name:     "HOME variable without trailing slash",
			input:    `path: ${HOME}/.claude`,
			expected: `path: ` + targetDirpath,
		},
		{
			name:     "absolute path with trailing slash",
			input:    `"installPath": "` + homeDirpath + `/.claude/plugins/cache/lua-lsp"`,
			expected: `"installPath": "` + targetDirpath + `/plugins/cache/lua-lsp"`,
		},
		{
			name:     "absolute path without trailing slash",
			input:    `"location": "` + homeDirpath + `/.claude"`,
			expected: `"location": "` + targetDirpath + `"`,
		},
		{
			name:     "multiple patterns in one string",
			input:    `path ~/.claude/foo and ${HOME}/.claude/bar`,
			expected: `path ` + targetDirpath + `/foo and ` + targetDirpath + `/bar`,
		},
		{
			name:     "no matching paths unchanged",
			input:    `"key": "some normal value"`,
			expected: `"key": "some normal value"`,
		},
		{
			name:     "does not replace non-claude dot directories",
			input:    `"` + homeDirpath + `/.config/foo"`,
			expected: `"` + homeDirpath + `/.config/foo"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RewriteClaudePaths([]byte(tt.input), targetDirpath)
			if string(result) != tt.expected {
				t.Errorf("RewriteClaudePaths:\n  input:    %s\n  expected: %s\n  got:      %s",
					tt.input, tt.expected, string(result))
			}
		})
	}
}

func TestIsTextFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"settings.json", true},
		{"CLAUDE.md", true},
		{"hook.sh", true},
		{"script.bash", true},
		{"hook.py", true},
		{"config.yml", true},
		{"config.yaml", true},
		{"config.toml", true},
		{"notes.txt", true},
		{"image.png", false},
		{"binary.exe", false},
		{"noextension", false},
		{"SKILL.md", true},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := isTextFile(tt.path)
			if result != tt.expected {
				t.Errorf("isTextFile(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestInitShadowRepo(t *testing.T) {
	tmpDir := t.TempDir()

	shadowDirpath, err := InitShadowRepo(tmpDir)
	if err != nil {
		t.Fatalf("InitShadowRepo failed: %v", err)
	}

	expectedDirpath := filepath.Join(tmpDir, ShadowRepoDirname)
	if shadowDirpath != expectedDirpath {
		t.Errorf("expected shadow dirpath %q, got %q", expectedDirpath, shadowDirpath)
	}

	// Verify .git directory exists
	gitDirpath := filepath.Join(shadowDirpath, ".git")
	if _, err := os.Stat(gitDirpath); os.IsNotExist(err) {
		t.Error(".git directory was not created")
	}

	// No pre-commit hook should be installed (normalization removed)
	hookFilepath := filepath.Join(gitDirpath, "hooks", "pre-commit")
	if _, err := os.Stat(hookFilepath); err == nil {
		t.Error("pre-commit hook should not be installed (normalization removed)")
	}

	// Calling again should be a no-op
	shadowDirpath2, err := InitShadowRepo(tmpDir)
	if err != nil {
		t.Fatalf("second InitShadowRepo call failed: %v", err)
	}
	if shadowDirpath2 != shadowDirpath {
		t.Errorf("second call returned different path: %q vs %q", shadowDirpath2, shadowDirpath)
	}
}

func TestIngestFromClaudeDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up a fake ~/.claude directory
	claudeDirpath := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create tracked files with ~/.claude paths
	settingsContent := `{
  "permissions": {
    "allow": ["Read(` + claudeDirpath + `/skills/**)"]
  },
  "hooks": {
    "PreToolUse": [{"hooks": [{"type": "command", "command": "bash ~/.claude/hooks/check.sh"}]}]
  }
}`
	if err := os.WriteFile(filepath.Join(claudeDirpath, "settings.json"), []byte(settingsContent), 0644); err != nil {
		t.Fatal(err)
	}

	claudeMdContent := "# Instructions\nSee ~/.claude/skills for available skills.\n"
	if err := os.WriteFile(filepath.Join(claudeDirpath, "CLAUDE.md"), []byte(claudeMdContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a tracked directory with a text file
	skillsDirpath := filepath.Join(claudeDirpath, "skills", "my-skill")
	if err := os.MkdirAll(skillsDirpath, 0755); err != nil {
		t.Fatal(err)
	}
	skillContent := "# My Skill\nConfig at ~/.claude/settings.json\n"
	if err := os.WriteFile(filepath.Join(skillsDirpath, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Initialize shadow repo
	shadowDirpath, err := InitShadowRepo(tmpDir)
	if err != nil {
		t.Fatalf("InitShadowRepo failed: %v", err)
	}

	// Ingest
	if err := IngestFromClaudeDir(claudeDirpath, shadowDirpath); err != nil {
		t.Fatalf("IngestFromClaudeDir failed: %v", err)
	}

	// Verify settings.json stored verbatim — no normalization
	ingestedSettings, err := os.ReadFile(filepath.Join(shadowDirpath, "settings.json"))
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}
	if string(ingestedSettings) != settingsContent {
		t.Errorf("settings.json should be stored verbatim:\n  expected: %s\n  got:      %s",
			settingsContent, string(ingestedSettings))
	}

	// Verify CLAUDE.md stored verbatim — still contains ~/.claude
	ingestedClaudeMd, err := os.ReadFile(filepath.Join(shadowDirpath, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("failed to read CLAUDE.md: %v", err)
	}
	if string(ingestedClaudeMd) != claudeMdContent {
		t.Errorf("CLAUDE.md should be stored verbatim:\n  expected: %s\n  got:      %s",
			claudeMdContent, string(ingestedClaudeMd))
	}

	// Verify skill file stored verbatim — still contains ~/.claude
	ingestedSkill, err := os.ReadFile(filepath.Join(shadowDirpath, "skills", "my-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read skill: %v", err)
	}
	if string(ingestedSkill) != skillContent {
		t.Errorf("SKILL.md should be stored verbatim:\n  expected: %s\n  got:      %s",
			skillContent, string(ingestedSkill))
	}

	// Verify a git commit was created
	cmd := exec.Command("git", "log", "--oneline")
	cmd.Dir = shadowDirpath
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log failed: %v", err)
	}
	if !containsSubstring(string(output), "Sync from ~/.claude") {
		t.Errorf("expected commit message 'Sync from ~/.claude', got:\n%s", string(output))
	}
}

func TestIngestFromClaudeDir_MissingSource(t *testing.T) {
	tmpDir := t.TempDir()

	// ~/.claude doesn't exist
	claudeDirpath := filepath.Join(tmpDir, ".claude")

	// Initialize shadow repo
	shadowDirpath, err := InitShadowRepo(tmpDir)
	if err != nil {
		t.Fatalf("InitShadowRepo failed: %v", err)
	}

	// Ingest should succeed (no tracked files to copy)
	if err := IngestFromClaudeDir(claudeDirpath, shadowDirpath); err != nil {
		t.Fatalf("IngestFromClaudeDir failed for missing source: %v", err)
	}
}

func TestIngestFromClaudeDir_SymlinkSource(t *testing.T) {
	tmpDir := t.TempDir()

	// Create actual config files in a "dotfiles repo"
	dotfilesDirpath := filepath.Join(tmpDir, "dotfiles", "claude")
	if err := os.MkdirAll(dotfilesDirpath, 0755); err != nil {
		t.Fatal(err)
	}
	settingsContent := `{"key": "value from dotfiles"}`
	if err := os.WriteFile(filepath.Join(dotfilesDirpath, "settings.json"), []byte(settingsContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create ~/.claude with a symlink to the dotfiles
	claudeDirpath := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDirpath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(dotfilesDirpath, "settings.json"),
		filepath.Join(claudeDirpath, "settings.json"),
	); err != nil {
		t.Fatal(err)
	}

	// Initialize shadow repo and ingest
	shadowDirpath, err := InitShadowRepo(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := IngestFromClaudeDir(claudeDirpath, shadowDirpath); err != nil {
		t.Fatal(err)
	}

	// Verify the actual content was copied (not the symlink)
	data, err := os.ReadFile(filepath.Join(shadowDirpath, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsSubstring(string(data), "value from dotfiles") {
		t.Errorf("expected content from dotfiles, got:\n%s", string(data))
	}

	// Verify it's a real file, not a symlink
	info, err := os.Lstat(filepath.Join(shadowDirpath, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("shadow repo should contain a real file, not a symlink")
	}
}

func TestIngestFromClaudeDir_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up a fake ~/.claude
	claudeDirpath := filepath.Join(tmpDir, ".claude")
	if err := os.MkdirAll(claudeDirpath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDirpath, "CLAUDE.md"), []byte("# Test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	shadowDirpath, err := InitShadowRepo(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// First ingest
	if err := IngestFromClaudeDir(claudeDirpath, shadowDirpath); err != nil {
		t.Fatal(err)
	}

	// Second ingest with same content — should not create a new commit
	if err := IngestFromClaudeDir(claudeDirpath, shadowDirpath); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("git", "log", "--oneline")
	cmd.Dir = shadowDirpath
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}

	lines := 0
	for _, line := range splitLines(string(output)) {
		if line != "" {
			lines++
		}
	}
	if lines != 1 {
		t.Errorf("expected 1 commit (idempotent), got %d:\n%s", lines, string(output))
	}
}

func TestIngestFromClaudeDir_DeletedFileInTrackedDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up a fake ~/.claude with a tracked directory containing two files
	claudeDirpath := filepath.Join(tmpDir, ".claude")
	skillsDirpath := filepath.Join(claudeDirpath, "skills", "my-skill")
	if err := os.MkdirAll(skillsDirpath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDirpath, "SKILL.md"), []byte("# My Skill\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skillsDirpath, "helper.sh"), []byte("#!/bin/bash\necho hi\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Initialize shadow repo and ingest
	shadowDirpath, err := InitShadowRepo(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := IngestFromClaudeDir(claudeDirpath, shadowDirpath); err != nil {
		t.Fatal(err)
	}

	// Verify both files exist in shadow repo
	for _, name := range []string{"SKILL.md", "helper.sh"} {
		shadowFilepath := filepath.Join(shadowDirpath, "skills", "my-skill", name)
		if _, err := os.Stat(shadowFilepath); os.IsNotExist(err) {
			t.Fatalf("expected %s to exist in shadow repo after first ingest", name)
		}
	}

	// Now delete helper.sh from source and re-ingest
	if err := os.Remove(filepath.Join(skillsDirpath, "helper.sh")); err != nil {
		t.Fatal(err)
	}
	if err := IngestFromClaudeDir(claudeDirpath, shadowDirpath); err != nil {
		t.Fatal(err)
	}

	// SKILL.md should still exist
	if _, err := os.Stat(filepath.Join(shadowDirpath, "skills", "my-skill", "SKILL.md")); os.IsNotExist(err) {
		t.Error("SKILL.md should still exist in shadow repo after re-ingest")
	}

	// helper.sh should be gone
	if _, err := os.Stat(filepath.Join(shadowDirpath, "skills", "my-skill", "helper.sh")); !os.IsNotExist(err) {
		t.Error("helper.sh should be removed from shadow repo after deletion from source")
	}

	// Verify a commit was created for the deletion
	cmd := exec.Command("git", "log", "--oneline")
	cmd.Dir = shadowDirpath
	output, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	commitLines := 0
	for _, line := range splitLines(string(output)) {
		if line != "" {
			commitLines++
		}
	}
	if commitLines != 2 {
		t.Errorf("expected 2 commits (initial + deletion), got %d:\n%s", commitLines, string(output))
	}
}

func TestIngestFromClaudeDir_DeletedSubdirInTrackedDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Set up ~/.claude with two skill subdirectories
	claudeDirpath := filepath.Join(tmpDir, ".claude")
	for _, skillName := range []string{"skill-a", "skill-b"} {
		dirpath := filepath.Join(claudeDirpath, "skills", skillName)
		if err := os.MkdirAll(dirpath, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dirpath, "SKILL.md"), []byte("# "+skillName+"\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	shadowDirpath, err := InitShadowRepo(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := IngestFromClaudeDir(claudeDirpath, shadowDirpath); err != nil {
		t.Fatal(err)
	}

	// Verify both skills exist
	for _, skillName := range []string{"skill-a", "skill-b"} {
		shadowFilepath := filepath.Join(shadowDirpath, "skills", skillName, "SKILL.md")
		if _, err := os.Stat(shadowFilepath); os.IsNotExist(err) {
			t.Fatalf("expected %s to exist in shadow repo", skillName)
		}
	}

	// Delete skill-b entirely from source
	if err := os.RemoveAll(filepath.Join(claudeDirpath, "skills", "skill-b")); err != nil {
		t.Fatal(err)
	}
	if err := IngestFromClaudeDir(claudeDirpath, shadowDirpath); err != nil {
		t.Fatal(err)
	}

	// skill-a should still exist
	if _, err := os.Stat(filepath.Join(shadowDirpath, "skills", "skill-a", "SKILL.md")); os.IsNotExist(err) {
		t.Error("skill-a should still exist after re-ingest")
	}

	// skill-b should be gone
	if _, err := os.Stat(filepath.Join(shadowDirpath, "skills", "skill-b")); !os.IsNotExist(err) {
		t.Error("skill-b directory should be removed from shadow repo after deletion from source")
	}
}

func TestGetShadowRepoDirpath(t *testing.T) {
	result := GetShadowRepoDirpath("/home/user/.agenc")
	expected := "/home/user/.agenc/claude-config-shadow"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestRewriteSettingsPaths(t *testing.T) {
	targetDirpath := "/tmp/claude/test-mission/claude-config"

	settingsData := []byte(`{
  "permissions": {
    "allow": ["Read(~/.claude/skills/**)"],
    "deny": ["Write(~/.agenc/repos/**)"]
  },
  "hooks": {
    "PreToolUse": [{"hooks": [{"type": "command", "command": "bash ~/.claude/hooks/check.sh"}]}]
  },
  "someOtherKey": "path is ~/.claude/foo"
}
`)

	result, err := RewriteSettingsPaths(settingsData, targetDirpath)
	if err != nil {
		t.Fatalf("RewriteSettingsPaths failed: %v", err)
	}

	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(result, &parsed); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Permissions block should be preserved exactly (un-rewritten)
	var perms map[string]json.RawMessage
	if err := json.Unmarshal(parsed["permissions"], &perms); err != nil {
		t.Fatalf("failed to parse permissions: %v", err)
	}
	var allow []string
	if err := json.Unmarshal(perms["allow"], &allow); err != nil {
		t.Fatalf("failed to parse allow: %v", err)
	}
	if len(allow) != 1 || allow[0] != "Read(~/.claude/skills/**)" {
		t.Errorf("permissions.allow should be preserved, got: %v", allow)
	}

	// Hooks should have paths rewritten
	hooksStr := string(parsed["hooks"])
	if containsSubstring(hooksStr, "~/.claude") {
		t.Errorf("hooks should have paths rewritten, got: %s", hooksStr)
	}
	if !containsSubstring(hooksStr, targetDirpath) {
		t.Errorf("hooks should contain target dirpath, got: %s", hooksStr)
	}

	// Other keys should have paths rewritten
	otherStr := string(parsed["someOtherKey"])
	if containsSubstring(otherStr, "~/.claude") {
		t.Errorf("someOtherKey should have paths rewritten, got: %s", otherStr)
	}
}

func TestCopyAndPatchClaudeJSON(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source .claude.json
	srcDirpath := filepath.Join(tmpDir, "source", ".claude")
	if err := os.MkdirAll(srcDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	originalJSON := map[string]interface{}{
		"oauthAccount": "test@example.com",
		"projects": map[string]interface{}{
			"/existing/project": map[string]interface{}{
				"hasTrustDialogAccepted": true,
			},
		},
	}
	originalData, err := json.MarshalIndent(originalJSON, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	srcFilepath := filepath.Join(srcDirpath, ".claude.json")
	if err := os.WriteFile(srcFilepath, originalData, 0644); err != nil {
		t.Fatal(err)
	}

	// Create destination config dir
	destDirpath := filepath.Join(tmpDir, "dest", "claude-config")
	if err := os.MkdirAll(destDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	// Temporarily override HOME so copyAndPatchClaudeJSON finds our source
	origHome := os.Getenv("HOME")
	os.Setenv("HOME", filepath.Join(tmpDir, "source"))
	defer os.Setenv("HOME", origHome)

	missionAgentDirpath := "/tmp/claude/missions/test-123/agent"
	if err := copyAndPatchClaudeJSON(destDirpath, missionAgentDirpath, nil); err != nil {
		t.Fatalf("copyAndPatchClaudeJSON failed: %v", err)
	}

	// Read the result
	resultData, err := os.ReadFile(filepath.Join(destDirpath, ".claude.json"))
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}

	// Verify it's a real file (not a symlink)
	info, err := os.Lstat(filepath.Join(destDirpath, ".claude.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("result should be a real file, not a symlink")
	}

	// Parse and verify
	var result map[string]json.RawMessage
	if err := json.Unmarshal(resultData, &result); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Verify oauthAccount preserved
	var account string
	if err := json.Unmarshal(result["oauthAccount"], &account); err != nil {
		t.Fatalf("failed to parse oauthAccount: %v", err)
	}
	if account != "test@example.com" {
		t.Errorf("expected oauthAccount 'test@example.com', got %q", account)
	}

	// Verify projects
	var projects map[string]json.RawMessage
	if err := json.Unmarshal(result["projects"], &projects); err != nil {
		t.Fatalf("failed to parse projects: %v", err)
	}

	// Original project should still be there
	if _, ok := projects["/existing/project"]; !ok {
		t.Error("existing project entry should be preserved")
	}

	// Mission agent dir should have trust entry
	missionEntry, ok := projects[missionAgentDirpath]
	if !ok {
		t.Fatalf("mission agent dir entry not found in projects")
	}

	var trustData map[string]bool
	if err := json.Unmarshal(missionEntry, &trustData); err != nil {
		t.Fatalf("failed to parse mission trust entry: %v", err)
	}
	if !trustData["hasTrustDialogAccepted"] {
		t.Error("hasTrustDialogAccepted should be true")
	}
}

// --- test helpers ---

func containsSubstring(s string, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && searchSubstring(s, sub)))
}

func searchSubstring(s string, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
