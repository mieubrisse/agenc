package cmd

import (
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

// The tmux command palette is an interactive human surface, so it keeps its
// color even though agenc's command output no longer has any (agenc-hxr4).
func TestFormatPaletteEntryLine_KeepsAnsi(t *testing.T) {
	line := formatPaletteEntryLine(config.ResolvedPaletteCommand{
		Name:        "open-repo-example",
		Title:       "Example",
		Description: "Open the example repo",
	})
	if !strings.Contains(line, "\033") {
		t.Errorf("palette entry must keep its ANSI color, got %q", line)
	}
}
