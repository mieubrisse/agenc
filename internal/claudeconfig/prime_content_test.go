package claudeconfig

import (
	"strings"
	"testing"
)

func TestGetPrimeContent(t *testing.T) {
	t.Run("returns non-empty content", func(t *testing.T) {
		content := GetPrimeContent()
		if content == "" {
			t.Fatal("GetPrimeContent returned empty string")
		}
	})

	t.Run("does not contain YAML frontmatter", func(t *testing.T) {
		content := GetPrimeContent()
		if strings.HasPrefix(content, "---") {
			t.Error("content starts with YAML frontmatter delimiter")
		}
	})

	t.Run("contains expected CLI reference sections", func(t *testing.T) {
		content := GetPrimeContent()
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
			if !strings.Contains(content, phrase) {
				t.Errorf("content missing expected phrase: %q", phrase)
			}
		}
	})
}
