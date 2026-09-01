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

// The tests below hold the line agenc-hxr4 draws: command output is parsed by
// agents and carries no ANSI escapes, while the fzf pickers and the tmux
// command palette are read by a human and keep their color. The two halves
// share formatting code, so a change to one can silently take the other with
// it — these assert both directions.

const ansiEscape = "\033"

func TestFormatRepoDisplay_HasNoAnsi(t *testing.T) {
	result := formatRepoDisplay("github.com/owner/repo", false, nil)
	if strings.Contains(result, ansiEscape) {
		t.Errorf("command output must carry no ANSI escapes, got %q", result)
	}
}

func TestDisplayGitRepo_HasNoAnsi(t *testing.T) {
	for _, gitRepo := range []string{"", "github.com/owner/repo", "gitlab.com/owner/repo"} {
		result := displayGitRepo(gitRepo)
		if strings.Contains(result, ansiEscape) {
			t.Errorf("displayGitRepo(%q) must carry no ANSI escapes, got %q", gitRepo, result)
		}
	}
}

func TestColorizeStatus_KeepsAnsiForPicker(t *testing.T) {
	// mission ls / cron ls / cron history print the bare status; the picker is
	// the only caller of colorizeStatusForPicker, and it keeps the color.
	result := colorizeStatusForPicker(StatusIdle)
	if !strings.Contains(result, ansiEscape) {
		t.Errorf("picker status must keep its ANSI color, got %q", result)
	}
	if string(StatusIdle) == result {
		t.Errorf("expected colorizeStatusForPicker to wrap the status, got the bare value %q", result)
	}
}

// attachedDotForPicker had no test before agenc-afp8's review caught it: the
// E2E suite creates only headless missions, which are never attached, so the
// dot is the empty string throughout and a colour regression there would ship
// green.
func TestAttachedDotForPicker_KeepsAnsi(t *testing.T) {
	const isAttached = true
	result := attachedDotForPicker(isAttached)
	if !strings.Contains(result, ansiEscape) {
		t.Errorf("attached dot must keep its ANSI color, got %q", result)
	}
}

func TestAttachedDotForPicker_EmptyWhenDetached(t *testing.T) {
	const isAttached = false
	if result := attachedDotForPicker(isAttached); result != "" {
		t.Errorf("got %q, want empty string", result)
	}
}

func TestFormatRepoDisplayForPicker_KeepsAnsi(t *testing.T) {
	result := formatRepoDisplayForPicker("github.com/owner/repo", false, nil)
	if !strings.Contains(result, ansiEscape) {
		t.Errorf("picker repo display must keep its ANSI color, got %q", result)
	}
}

func TestFormatRepoDisplayForPicker_EmptyRepoIsPlain(t *testing.T) {
	result := formatRepoDisplayForPicker("", false, nil)
	if result != "--" {
		t.Errorf("got %q, want %q", result, "--")
	}
}
