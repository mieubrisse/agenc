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

	t.Run("successive writes produce correct content", func(t *testing.T) {
		tmpDirpath := t.TempDir()

		if err := writeAgencUsageSkill(tmpDirpath); err != nil {
			t.Fatalf("first write failed: %v", err)
		}

		if err := writeAgencUsageSkill(tmpDirpath); err != nil {
			t.Fatalf("second write failed: %v", err)
		}

		skillFilepath := filepath.Join(tmpDirpath, "skills", AgencUsageSkillDirname, "SKILL.md")
		data, err := os.ReadFile(skillFilepath)
		if err != nil {
			t.Fatalf("failed to read skill file after second write: %v", err)
		}

		if string(data) != agencUsageSkillContent {
			t.Error("content after second write does not match embedded constant")
		}
	})

	t.Run("overwrites existing content and removes stale files", func(t *testing.T) {
		tmpDirpath := t.TempDir()

		skillDirpath := filepath.Join(tmpDirpath, "skills", AgencUsageSkillDirname)
		if err := os.MkdirAll(skillDirpath, 0755); err != nil {
			t.Fatalf("failed to create skill dir: %v", err)
		}

		// Seed with user content: a modified SKILL.md and an extra file
		skillFilepath := filepath.Join(skillDirpath, "SKILL.md")
		if err := os.WriteFile(skillFilepath, []byte("old user content"), 0644); err != nil {
			t.Fatalf("failed to write seed SKILL.md: %v", err)
		}
		staleFilepath := filepath.Join(skillDirpath, "extra-file.md")
		if err := os.WriteFile(staleFilepath, []byte("stale"), 0644); err != nil {
			t.Fatalf("failed to write stale file: %v", err)
		}

		if err := writeAgencUsageSkill(tmpDirpath); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		data, err := os.ReadFile(skillFilepath)
		if err != nil {
			t.Fatalf("failed to read skill file: %v", err)
		}

		if string(data) != agencUsageSkillContent {
			t.Error("overwritten content does not match embedded constant")
		}

		if _, err := os.Stat(staleFilepath); !os.IsNotExist(err) {
			t.Error("stale extra file was not removed during overwrite")
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
