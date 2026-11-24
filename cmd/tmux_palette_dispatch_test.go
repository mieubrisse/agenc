package cmd

import (
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
	agentmux "github.com/odyssey/agenc/internal/tmux"
)

// On tmux < 3.3 the palette must not dispatch a popup command, whose injected
// display-popup -e fails there with only "returned 1" on screen; it shows the
// tmux 3.3 message instead. Everything else dispatches unchanged (KEV-341).
func TestResolvePaletteDispatchCommand_PopupGate(t *testing.T) {
	t.Setenv(config.AgencDirpathEnvVarName(), t.TempDir())
	t.Setenv(config.CallingPaneIDEnvVar, "%7")
	t.Setenv(config.CallingSessionNameEnvVar, "main")
	t.Setenv(config.CallingMissionUUIDEnvVar, "")

	popupEntry := config.ResolvedPaletteCommand{
		Name:    "showNotifications",
		Command: `tmux display-popup -E -w 95% -h 90% "agenc notification manage"`,
	}
	plainEntry := config.ResolvedPaletteCommand{
		Name:    "quickClaude",
		Command: "agenc mission new --blank",
	}
	wantMessage := "tmux " + agentmux.PopupUnsupportedMessageCommand

	if got := resolvePaletteDispatchCommand(popupEntry, "", false); got != wantMessage {
		t.Errorf("popup command without -e support: got %q, want %q", got, wantMessage)
	}

	got := resolvePaletteDispatchCommand(popupEntry, "", true)
	if !strings.Contains(got, "display-popup -e AGENC_CALLING_PANE_ID=%7 -e AGENC_CALLING_SESSION_NAME=main") {
		t.Errorf("popup command with -e support: expected the -e injection, got %q", got)
	}

	for _, isSupported := range []bool{false, true} {
		got := resolvePaletteDispatchCommand(plainEntry, "", isSupported)
		if want := buildPaletteDispatchCommand(plainEntry, ""); got != want {
			t.Errorf("non-popup command (popup -e supported=%v): got %q, want %q", isSupported, got, want)
		}
	}
}
