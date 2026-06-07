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

	t.Run("contains expected operating context and CLI reference sections", func(t *testing.T) {
		content := GetPrimeContent()
		expectedPhrases := []string{
			// Operating-context preamble (prime_preamble.md)
			"AgenC Operating Context",
			"Mission Filesystem Semantics",
			"Self-Reload Requires `--async`",
			"Cross-Repo Writes Need a New Mission",
			"Briefing a Spawned Mission",
			// Regression guard: the corrected ephemerality framing must survive.
			// Older versions said "only pushed work survives" which was factually wrong
			// and misled agents into mandatory push-everything behavior.
			"does this need to leave the mission",
			// Auto-generated CLI command groups
			"agenc mission",
			"agenc repo",
			"agenc config",
			"agenc cron",
			"agenc server",
			// Repo Formats postamble (prime_postamble.md)
			"Repo Formats",
			// Operational CLI warning
			"Never use interactive commands",
		}
		for _, phrase := range expectedPhrases {
			if !strings.Contains(content, phrase) {
				t.Errorf("content missing expected phrase: %q", phrase)
			}
		}
	})
}
