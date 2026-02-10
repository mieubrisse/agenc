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

	content := GenerateKeybindingsContent(3, 4, "agenc", keybindings)

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

	content := GenerateKeybindingsContent(3, 4, "agenc", keybindings)

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
	content := GenerateKeybindingsContent(3, 4, "agenc", nil)

	// Palette keybinding (k) should always include resolve-mission
	if !strings.Contains(content, "resolve-mission") {
		t.Error("expected palette keybinding to include resolve-mission")
	}

	// Should pass UUID into the popup environment
	if !strings.Contains(content, "AGENC_CALLING_MISSION_UUID") {
		t.Error("expected palette keybinding to pass AGENC_CALLING_MISSION_UUID")
	}
}

func TestGenerateKeybindingsContent_PaletteOmittedOnOldTmux(t *testing.T) {
	content := GenerateKeybindingsContent(3, 1, "agenc", nil)

	// tmux < 3.2 should not have the palette keybinding
	if strings.Contains(content, "display-popup") {
		t.Error("expected palette keybinding to be omitted on tmux < 3.2")
	}
}
