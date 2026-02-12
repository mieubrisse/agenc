package tmux

import (
	"strings"
	"testing"
)

func TestGenerateKeybindingsContent_MissionScopedKeybinding(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:             "x",
			Command:         "agenc mission stop $AGENC_CALLING_MISSION_UUID",
			Comment:         "stopThisMission — Stop this mission (prefix + a, x)",
			IsMissionScoped: true,
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)

	// Should contain resolve-mission preamble for mission-scoped keybinding
	if !strings.Contains(content, "resolve-mission") {
		t.Error("expected mission-scoped keybinding to contain resolve-mission preamble")
	}

	// Should contain the guard that skips execution when UUID is empty
	if !strings.Contains(content, "[ -n \"$AGENC_CALLING_MISSION_UUID\" ]") {
		t.Error("expected mission-scoped keybinding to contain non-empty UUID guard")
	}
}

func TestGenerateKeybindingsContent_NonMissionScopedKeybinding(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:     "d",
			Command: "agenc tmux window new -- agenc do",
			Comment: "do — Do (prefix + a, d)",
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)

	// Should contain the simple run-shell form
	if !strings.Contains(content, "run-shell 'agenc tmux window new -- agenc do'") {
		t.Error("expected non-mission-scoped keybinding to use simple run-shell form")
	}

	// The keybinding line itself should NOT contain resolve-mission
	// (palette keybinding does, but that's separate)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.Contains(line, "bind-key -T agenc d") {
			if strings.Contains(line, "resolve-mission") {
				t.Error("expected non-mission-scoped keybinding to not contain resolve-mission")
			}
			break
		}
	}
}

func TestGenerateKeybindingsContent_PaletteIncludesResolveMission(t *testing.T) {
	content := GenerateKeybindingsContent(3, 4, "-T agenc k", nil)

	// Palette keybinding (k) should always include resolve-mission
	if !strings.Contains(content, "resolve-mission") {
		t.Error("expected palette keybinding to include resolve-mission")
	}

	// Should pass UUID into the popup environment
	if !strings.Contains(content, "AGENC_CALLING_MISSION_UUID") {
		t.Error("expected palette keybinding to pass AGENC_CALLING_MISSION_UUID")
	}

	// Should use #{pane_id} directly, not via display-message
	if strings.Contains(content, "display-message") {
		t.Error("expected keybindings to use #{pane_id} directly, not via display-message")
	}
}

func TestGenerateKeybindingsContent_PaletteOmittedOnOldTmux(t *testing.T) {
	content := GenerateKeybindingsContent(3, 1, "-T agenc k", nil)

	// tmux < 3.2 should not have the palette keybinding
	if strings.Contains(content, "display-popup") {
		t.Error("expected palette keybinding to be omitted on tmux < 3.2")
	}
}

func TestGenerateKeybindingsContent_CustomPaletteKey(t *testing.T) {
	content := GenerateKeybindingsContent(3, 4, "-T agenc p", nil)

	// Should bind the palette to the custom key in the agenc table
	if !strings.Contains(content, "bind-key -T agenc p run-shell") {
		t.Error("expected palette to be bound to custom key 'p'")
	}

	// Should NOT bind to the default key 'k'
	if strings.Contains(content, "bind-key -T agenc k") {
		t.Error("expected palette NOT to be bound to default key 'k' when overridden")
	}
}

func TestGenerateKeybindingsContent_PaletteKeyOutsideAgencTable(t *testing.T) {
	// User binds the palette directly on prefix (no agenc table)
	content := GenerateKeybindingsContent(3, 4, "C-k", nil)

	// Should emit the keybinding verbatim
	if !strings.Contains(content, "bind-key C-k run-shell") {
		t.Error("expected palette to be bound directly with 'C-k'")
	}

	// Should NOT be in the agenc table
	if strings.Contains(content, "bind-key -T agenc C-k") {
		t.Error("expected palette keybinding NOT to be in the agenc table")
	}
}
