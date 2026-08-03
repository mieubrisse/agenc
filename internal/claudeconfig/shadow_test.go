package claudeconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

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

// TestIngestFromClaudeDir_PreservesExecutableBit is a regression test for
// https://github.com/mieubrisse/agenc/issues/6 — skill scripts in ~/.claude
// were being copied into the shadow repo with hard-coded 0644, stripping +x
// from anything like skills/foo/upload.py that relies on direct invocation.
func TestIngestFromClaudeDir_PreservesExecutableBit(t *testing.T) {
	tmpDir := t.TempDir()

	claudeDirpath := filepath.Join(tmpDir, ".claude")
	skillDirpath := filepath.Join(claudeDirpath, "skills", "mdbin")
	if err := os.MkdirAll(skillDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	// Minimal required tracked files so we don't trip the "no source" branch.
	if err := os.WriteFile(filepath.Join(claudeDirpath, "CLAUDE.md"), []byte("# test\n"), 0644); err != nil {
		t.Fatal(err)
	}

	execScript := filepath.Join(skillDirpath, "upload.py")
	if err := os.WriteFile(execScript, []byte("#!/usr/bin/env python3\nprint('hi')\n"), 0755); err != nil {
		t.Fatal(err)
	}
	// os.WriteFile only honors mode on creation; assert the source is indeed 0755
	// so the test fails on the perms behavior, not on a setup mistake.
	if err := os.Chmod(execScript, 0755); err != nil {
		t.Fatal(err)
	}
	plainFile := filepath.Join(skillDirpath, "SKILL.md")
	if err := os.WriteFile(plainFile, []byte("# mdbin\n"), 0644); err != nil {
		t.Fatal(err)
	}

	shadowDirpath, err := InitShadowRepo(tmpDir)
	if err != nil {
		t.Fatalf("InitShadowRepo failed: %v", err)
	}
	if err := IngestFromClaudeDir(claudeDirpath, shadowDirpath); err != nil {
		t.Fatalf("IngestFromClaudeDir failed: %v", err)
	}

	shadowedExec := filepath.Join(shadowDirpath, "skills", "mdbin", "upload.py")
	execInfo, err := os.Stat(shadowedExec)
	if err != nil {
		t.Fatalf("failed to stat shadowed script: %v", err)
	}
	if execInfo.Mode().Perm() != 0755 {
		t.Errorf("expected shadow copy of executable script to have mode 0755, got %o", execInfo.Mode().Perm())
	}

	shadowedPlain := filepath.Join(shadowDirpath, "skills", "mdbin", "SKILL.md")
	plainInfo, err := os.Stat(shadowedPlain)
	if err != nil {
		t.Fatalf("failed to stat shadowed plain file: %v", err)
	}
	if plainInfo.Mode().Perm() != 0644 {
		t.Errorf("expected shadow copy of plain file to have mode 0644, got %o", plainInfo.Mode().Perm())
	}
}

// TestIngestFromClaudeDir_RepairsStaleMode covers the upgrade path from a
// previously-buggy shadow repo: identical content but stale 0644 perms on the
// shadowed file should be repaired to match the (executable) source on
// re-ingest, not skipped by the content-equality fast path.
func TestIngestFromClaudeDir_RepairsStaleMode(t *testing.T) {
	tmpDir := t.TempDir()

	claudeDirpath := filepath.Join(tmpDir, ".claude")
	skillDirpath := filepath.Join(claudeDirpath, "skills", "mdbin")
	if err := os.MkdirAll(skillDirpath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(claudeDirpath, "CLAUDE.md"), []byte("# test\n"), 0644); err != nil {
		t.Fatal(err)
	}
	scriptBody := []byte("#!/usr/bin/env python3\nprint('hi')\n")
	execScript := filepath.Join(skillDirpath, "upload.py")
	if err := os.WriteFile(execScript, scriptBody, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(execScript, 0755); err != nil {
		t.Fatal(err)
	}

	shadowDirpath, err := InitShadowRepo(tmpDir)
	if err != nil {
		t.Fatalf("InitShadowRepo failed: %v", err)
	}

	// Simulate the legacy buggy state: shadow has identical content but 0644.
	stalePath := filepath.Join(shadowDirpath, "skills", "mdbin", "upload.py")
	if err := os.MkdirAll(filepath.Dir(stalePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stalePath, scriptBody, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stalePath, 0644); err != nil {
		t.Fatal(err)
	}

	if err := IngestFromClaudeDir(claudeDirpath, shadowDirpath); err != nil {
		t.Fatalf("IngestFromClaudeDir failed: %v", err)
	}

	info, err := os.Stat(stalePath)
	if err != nil {
		t.Fatalf("failed to stat shadowed script after re-ingest: %v", err)
	}
	if info.Mode().Perm() != 0755 {
		t.Errorf("expected re-ingest to repair stale 0644 perms to 0755, got %o", info.Mode().Perm())
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

func TestIngestFromClaudeDir_SymlinkedDirInsideTrackedDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Create an external directory that the symlink will point to (simulates
	// a skill living in another repo, e.g. ~/code/mdbin/skills/mdbin)
	externalSkillDirpath := filepath.Join(tmpDir, "external-repo", "skills", "mdbin")
	if err := os.MkdirAll(externalSkillDirpath, 0755); err != nil {
		t.Fatal(err)
	}
	skillContent := "# mdbin skill\nDoes mdbin things.\n"
	if err := os.WriteFile(filepath.Join(externalSkillDirpath, "SKILL.md"), []byte(skillContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create ~/.claude/skills with a regular skill and a symlinked skill
	claudeDirpath := filepath.Join(tmpDir, ".claude")
	regularSkillDirpath := filepath.Join(claudeDirpath, "skills", "regular-skill")
	if err := os.MkdirAll(regularSkillDirpath, 0755); err != nil {
		t.Fatal(err)
	}
	regularContent := "# Regular skill\n"
	if err := os.WriteFile(filepath.Join(regularSkillDirpath, "SKILL.md"), []byte(regularContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Symlink ~/.claude/skills/mdbin -> external-repo/skills/mdbin (a directory)
	if err := os.Symlink(externalSkillDirpath, filepath.Join(claudeDirpath, "skills", "mdbin")); err != nil {
		t.Fatal(err)
	}

	// Initialize shadow repo and ingest
	shadowDirpath, err := InitShadowRepo(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := IngestFromClaudeDir(claudeDirpath, shadowDirpath); err != nil {
		t.Fatalf("IngestFromClaudeDir failed: %v", err)
	}

	// Verify regular skill was ingested
	data, err := os.ReadFile(filepath.Join(shadowDirpath, "skills", "regular-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read regular skill: %v", err)
	}
	if string(data) != regularContent {
		t.Errorf("regular skill content mismatch: expected %q, got %q", regularContent, string(data))
	}

	// Verify symlinked directory skill was ingested as a real directory with content
	data, err = os.ReadFile(filepath.Join(shadowDirpath, "skills", "mdbin", "SKILL.md"))
	if err != nil {
		t.Fatalf("failed to read symlinked skill: %v", err)
	}
	if string(data) != skillContent {
		t.Errorf("symlinked skill content mismatch: expected %q, got %q", skillContent, string(data))
	}

	// Verify it's a real directory, not a symlink
	info, err := os.Lstat(filepath.Join(shadowDirpath, "skills", "mdbin"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("shadow repo should contain a real directory, not a symlink")
	}
	if !info.IsDir() {
		t.Error("shadow repo entry should be a directory")
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
