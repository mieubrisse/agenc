package claudeconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNormalizePaths(t *testing.T) {
	homeDirpath := "/Users/testuser"

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "absolute path with trailing slash",
			input:    `"installPath": "/Users/testuser/.claude/plugins/cache/lua-lsp"`,
			expected: `"installPath": "${CLAUDE_CONFIG_DIR}/plugins/cache/lua-lsp"`,
		},
		{
			name:     "absolute path without trailing slash",
			input:    `"location": "/Users/testuser/.claude"`,
			expected: `"location": "${CLAUDE_CONFIG_DIR}"`,
		},
		{
			name:     "HOME variable with trailing slash",
			input:    `bash ${HOME}/.claude/hooks/my-hook.sh`,
			expected: `bash ${CLAUDE_CONFIG_DIR}/hooks/my-hook.sh`,
		},
		{
			name:     "HOME variable without trailing slash",
			input:    `path: ${HOME}/.claude`,
			expected: `path: ${CLAUDE_CONFIG_DIR}`,
		},
		{
			name:     "tilde path with trailing slash",
			input:    `"command": "bash ~/.claude/hooks/set-style.sh"`,
			expected: `"command": "bash ${CLAUDE_CONFIG_DIR}/hooks/set-style.sh"`,
		},
		{
			name:     "tilde path without trailing slash",
			input:    `Read(~/.claude)`,
			expected: `Read(${CLAUDE_CONFIG_DIR})`,
		},
		{
			name:     "multiple patterns in one string",
			input:    `path /Users/testuser/.claude/foo and ~/.claude/bar`,
			expected: `path ${CLAUDE_CONFIG_DIR}/foo and ${CLAUDE_CONFIG_DIR}/bar`,
		},
		{
			name:     "no matching paths unchanged",
			input:    `"key": "some normal value"`,
			expected: `"key": "some normal value"`,
		},
		{
			name:     "partial match not replaced (different user)",
			input:    `"/Users/otheruser/.claude/stuff"`,
			expected: `"/Users/otheruser/.claude/stuff"`,
		},
		{
			name:     "does not replace non-claude dot directories",
			input:    `"/Users/testuser/.config/foo"`,
			expected: `"/Users/testuser/.config/foo"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizePaths([]byte(tt.input), homeDirpath)
			if string(result) != tt.expected {
				t.Errorf("NormalizePaths:\n  input:    %s\n  expected: %s\n  got:      %s",
					tt.input, tt.expected, string(result))
			}
		})
	}
}

func TestExpandPaths(t *testing.T) {
	configDirpath := "/Users/testuser/.agenc/missions/abc-123/claude-config"

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "placeholder with trailing slash",
			input:    `"installPath": "${CLAUDE_CONFIG_DIR}/plugins/cache/lua-lsp"`,
			expected: `"installPath": "/Users/testuser/.agenc/missions/abc-123/claude-config/plugins/cache/lua-lsp"`,
		},
		{
			name:     "placeholder without trailing slash",
			input:    `"location": "${CLAUDE_CONFIG_DIR}"`,
			expected: `"location": "/Users/testuser/.agenc/missions/abc-123/claude-config"`,
		},
		{
			name:     "multiple placeholders",
			input:    `${CLAUDE_CONFIG_DIR}/foo and ${CLAUDE_CONFIG_DIR}/bar`,
			expected: configDirpath + `/foo and ` + configDirpath + `/bar`,
		},
		{
			name:     "no placeholders unchanged",
			input:    `"key": "value"`,
			expected: `"key": "value"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExpandPaths([]byte(tt.input), configDirpath)
			if string(result) != tt.expected {
				t.Errorf("ExpandPaths:\n  input:    %s\n  expected: %s\n  got:      %s",
					tt.input, tt.expected, string(result))
			}
		})
	}
}

func TestNormalizeExpandRoundTrip(t *testing.T) {
	homeDirpath := "/Users/testuser"
	configDirpath := "/Users/testuser/.agenc/missions/abc-123/claude-config"

	inputs := []string{
		`"installPath": "/Users/testuser/.claude/plugins/cache/lua-lsp/1.0.0"`,
		`"command": "bash ~/.claude/hooks/my-hook.sh"`,
		`path: ${HOME}/.claude/skills/my-skill`,
	}

	for _, input := range inputs {
		normalized := NormalizePaths([]byte(input), homeDirpath)
		expanded := ExpandPaths(normalized, configDirpath)

		// Expanded should not contain any ~/.claude references
		if containsClaudePath(string(expanded), homeDirpath) {
			t.Errorf("round-trip still contains ~/.claude path:\n  input:      %s\n  normalized: %s\n  expanded:   %s",
				input, string(normalized), string(expanded))
		}

		// Expanded should contain the config dirpath
		if !containsSubstring(string(expanded), configDirpath) {
			t.Errorf("expanded result doesn't contain config dirpath:\n  expanded: %s", string(expanded))
		}
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

	// Verify pre-commit hook exists and is executable
	hookFilepath := filepath.Join(gitDirpath, "hooks", "pre-commit")
	info, err := os.Stat(hookFilepath)
	if os.IsNotExist(err) {
		t.Error("pre-commit hook was not created")
	} else if err != nil {
		t.Fatalf("failed to stat hook: %v", err)
	} else if info.Mode()&0111 == 0 {
		t.Error("pre-commit hook is not executable")
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

	// Create tracked files with paths that need normalization
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

	// Verify settings.json was NOT normalized — it contains permission entries
	// with user-specified paths that must not be rewritten
	normalizedSettings, err := os.ReadFile(filepath.Join(shadowDirpath, "settings.json"))
	if err != nil {
		t.Fatalf("failed to read settings: %v", err)
	}
	if containsSubstring(string(normalizedSettings), "${CLAUDE_CONFIG_DIR}") {
		t.Errorf("settings.json should not be normalized (contains permission paths):\n%s", string(normalizedSettings))
	}

	// Verify CLAUDE.md was normalized
	normalizedClaudeMd, err := os.ReadFile(filepath.Join(shadowDirpath, "CLAUDE.md"))
	if err != nil {
		t.Fatalf("failed to read normalized CLAUDE.md: %v", err)
	}
	if containsSubstring(string(normalizedClaudeMd), "~/.claude") {
		t.Errorf("CLAUDE.md still contains ~/.claude:\n%s", string(normalizedClaudeMd))
	}

	// Verify skill file was normalized
	normalizedSkill, err := os.ReadFile(filepath.Join(shadowDirpath, "skills", "my-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read normalized skill: %v", err)
	}
	if containsSubstring(string(normalizedSkill), "~/.claude") {
		t.Errorf("SKILL.md still contains ~/.claude:\n%s", string(normalizedSkill))
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

func TestGetShadowRepoDirpath(t *testing.T) {
	result := GetShadowRepoDirpath("/home/user/.agenc")
	expected := "/home/user/.agenc/claude-config-shadow"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// --- test helpers ---

func containsClaudePath(s string, homeDirpath string) bool {
	return containsSubstring(s, homeDirpath+"/.claude") ||
		containsSubstring(s, "${HOME}/.claude") ||
		containsSubstring(s, "~/.claude")
}

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
