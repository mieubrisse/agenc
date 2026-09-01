package cmd

import (
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

// The tmux command palette is an interactive human surface, so it keeps its
// color even though agenc's command output no longer has any (agenc-hxr4).
// Every colored palette surface needs a test of its own: review found that
// stripping the escape from the unread banner left `make check` green, and
// that banner holds the exact escape agenc-hxr4 flagged as the one a
// constants-only change would miss.

func TestFormatPaletteEntryLine_KeepsAnsi_WithDescription(t *testing.T) {
	line := formatPaletteEntryLineForPicker(config.ResolvedPaletteCommand{
		Name:        "open-repo-example",
		Title:       "Example",
		Description: "Open the example repo",
	})
	if !strings.Contains(line, ansiEscape) {
		t.Errorf("palette entry must keep its ANSI color, got %q", line)
	}
}

// An entry without a Description returns through a different branch, so the
// with-description case alone leaves the bold label unpinned.
func TestFormatPaletteEntryLine_KeepsAnsi_WithoutDescription(t *testing.T) {
	line := formatPaletteEntryLineForPicker(config.ResolvedPaletteCommand{
		Name:  "open-repo-example",
		Title: "Example",
	})
	if !strings.Contains(line, ansiEscape) {
		t.Errorf("palette entry without a description must still keep its ANSI color, got %q", line)
	}
}

func TestFormatUnreadBannerForPicker_KeepsAnsi(t *testing.T) {
	const unreadCount = 3
	banner := formatUnreadBannerForPicker(unreadCount)
	if !strings.Contains(banner, ansiEscape) {
		t.Errorf("palette unread banner must keep its ANSI color, got %q", banner)
	}
	if !strings.Contains(banner, "3 unread notifications") {
		t.Errorf("expected a pluralized count in the banner, got %q", banner)
	}
}

func TestFormatUnreadBannerForPicker_SingularNoun(t *testing.T) {
	const unreadCount = 1
	banner := formatUnreadBannerForPicker(unreadCount)
	if !strings.Contains(banner, "1 unread notification") {
		t.Errorf("expected a singular noun for one notification, got %q", banner)
	}
}
