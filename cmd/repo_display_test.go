package cmd

import (
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestFormatRepoDisplay_Adjutant(t *testing.T) {
	result := formatRepoDisplay("anything", true, nil)
	if result != "🤖  Adjutant" {
		t.Errorf("got %q, want %q", result, "🤖  Adjutant")
	}
}

func TestFormatRepoDisplay_NilConfig(t *testing.T) {
	result := formatRepoDisplay("github.com/owner/repo", false, nil)
	if !strings.Contains(result, "owner/") {
		t.Errorf("expected owner/ in result, got %q", result)
	}
}

func TestFormatRepoDisplay_EmptyRepo(t *testing.T) {
	result := formatRepoDisplay("", false, nil)
	if result != "--" {
		t.Errorf("got %q, want %q", result, "--")
	}
}

func TestFormatRepoDisplay_TitleOnly(t *testing.T) {
	cfg := &config.AgencConfig{
		RepoConfigs: map[string]config.RepoConfig{
			"github.com/owner/repo": {Title: "My App"},
		},
	}
	result := formatRepoDisplay("github.com/owner/repo", false, cfg)
	if result != "My App" {
		t.Errorf("got %q, want %q", result, "My App")
	}
}

func TestFormatRepoDisplay_EmojiOnly(t *testing.T) {
	cfg := &config.AgencConfig{
		RepoConfigs: map[string]config.RepoConfig{
			"github.com/owner/repo": {Emoji: "🔥"},
		},
	}
	result := formatRepoDisplay("github.com/owner/repo", false, cfg)
	if !strings.HasPrefix(result, "🔥") {
		t.Errorf("expected emoji prefix, got %q", result)
	}
	if !strings.Contains(result, "owner/") {
		t.Errorf("expected owner/ in result, got %q", result)
	}
}

func TestFormatRepoDisplay_EmojiAndTitle(t *testing.T) {
	cfg := &config.AgencConfig{
		RepoConfigs: map[string]config.RepoConfig{
			"github.com/owner/repo": {Emoji: "🔥", Title: "My App"},
		},
	}
	result := formatRepoDisplay("github.com/owner/repo", false, cfg)
	if !strings.HasPrefix(result, "🔥") {
		t.Errorf("expected emoji prefix, got %q", result)
	}
	if !strings.Contains(result, "My App") {
		t.Errorf("expected title in result, got %q", result)
	}
}
