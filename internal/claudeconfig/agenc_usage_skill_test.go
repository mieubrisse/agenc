package claudeconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteAgencUsageSkill(t *testing.T) {
	t.Run("creates skill directory and file", func(t *testing.T) {
		tmpDirpath := t.TempDir()

		if err := writeAgencUsageSkill(tmpDirpath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		skillFilepath := filepath.Join(tmpDirpath, "skills", AgencUsageSkillDirname, "SKILL.md")
		data, err := os.ReadFile(skillFilepath)
		if err != nil {
			t.Fatalf("failed to read generated skill file: %v", err)
		}

		if len(data) == 0 {
			t.Fatal("skill file is empty")
		}

		content := string(data)
		if !strings.Contains(content, "AgenC CLI Quick Reference") {
			t.Error("skill file missing expected header")
		}
		if !strings.Contains(content, "agenc mission") {
			t.Error("skill file missing mission commands")
		}
		if !strings.Contains(content, "$AGENC_MISSION_UUID") {
			t.Error("skill file missing mission UUID reference")
		}
	})

	t.Run("content matches embedded constant", func(t *testing.T) {
		tmpDirpath := t.TempDir()

		if err := writeAgencUsageSkill(tmpDirpath); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		skillFilepath := filepath.Join(tmpDirpath, "skills", AgencUsageSkillDirname, "SKILL.md")
		data, err := os.ReadFile(skillFilepath)
		if err != nil {
			t.Fatalf("failed to read generated skill file: %v", err)
		}

		if string(data) != agencUsageSkillContent {
			t.Error("written file content does not match embedded constant")
		}
	})

	t.Run("idempotent writes", func(t *testing.T) {
		tmpDirpath := t.TempDir()

		if err := writeAgencUsageSkill(tmpDirpath); err != nil {
			t.Fatalf("first write failed: %v", err)
		}

		skillFilepath := filepath.Join(tmpDirpath, "skills", AgencUsageSkillDirname, "SKILL.md")
		info1, err := os.Stat(skillFilepath)
		if err != nil {
			t.Fatalf("failed to stat after first write: %v", err)
		}

		// Second write should not change the file (WriteIfChanged)
		if err := writeAgencUsageSkill(tmpDirpath); err != nil {
			t.Fatalf("second write failed: %v", err)
		}

		info2, err := os.Stat(skillFilepath)
		if err != nil {
			t.Fatalf("failed to stat after second write: %v", err)
		}

		if info1.ModTime() != info2.ModTime() {
			t.Error("file was rewritten despite identical content (WriteIfChanged should have skipped)")
		}
	})

	t.Run("overwrites existing content", func(t *testing.T) {
		tmpDirpath := t.TempDir()

		skillDirpath := filepath.Join(tmpDirpath, "skills", AgencUsageSkillDirname)
		if err := os.MkdirAll(skillDirpath, 0755); err != nil {
			t.Fatalf("failed to create skill dir: %v", err)
		}

		skillFilepath := filepath.Join(skillDirpath, "SKILL.md")
		if err := os.WriteFile(skillFilepath, []byte("old user content"), 0644); err != nil {
			t.Fatalf("failed to write seed content: %v", err)
		}

		if err := writeAgencUsageSkill(tmpDirpath); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		data, err := os.ReadFile(skillFilepath)
		if err != nil {
			t.Fatalf("failed to read skill file: %v", err)
		}

		if string(data) == "old user content" {
			t.Error("auto-generated skill did not overwrite existing content")
		}
		if string(data) != agencUsageSkillContent {
			t.Error("overwritten content does not match embedded constant")
		}
	})
}

func TestAgencUsageSkillContent(t *testing.T) {
	t.Run("embedded content is non-empty", func(t *testing.T) {
		if agencUsageSkillContent == "" {
			t.Fatal("agencUsageSkillContent is empty; was the skill generated at build time?")
		}
	})

	t.Run("contains expected sections", func(t *testing.T) {
		expectedPhrases := []string{
			"AgenC CLI Quick Reference",
			"agenc mission",
			"agenc repo",
			"agenc config",
			"agenc cron",
			"agenc daemon",
			"Repo Formats",
			"$AGENC_MISSION_UUID",
			"Never use interactive commands",
		}
		for _, phrase := range expectedPhrases {
			if !strings.Contains(agencUsageSkillContent, phrase) {
				t.Errorf("embedded skill content missing expected phrase: %q", phrase)
			}
		}
	})
}
