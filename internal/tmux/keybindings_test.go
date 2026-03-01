package tmux

import (
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestGenerateKeybindingsContent_MissionScopedKeybinding(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:             "x",
			Command:         "agenc mission stop $AGENC_CALLING_MISSION_UUID",
			Comment:         "stopMission — Stop Mission (prefix + a, x)",
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
			Key:     "n",
			Command: "agenc mission new",
			Comment: "newMission — New Mission (prefix + a, n)",
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)

	// Should contain the simple run-shell form
	if !strings.Contains(content, "run-shell 'agenc mission new'") {
		t.Error("expected non-mission-scoped keybinding to use simple run-shell form")
	}

	// The keybinding line itself should NOT contain resolve-mission
	// (palette keybinding does, but that's separate)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.Contains(line, "bind-key -T agenc n") {
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

func TestGenerateKeybindingsContent_GlobalKeybinding(t *testing.T) {
	// Keybinding with "-n" prefix should be inserted verbatim (root table),
	// not wrapped with "-T agenc".
	keybindings := []CustomKeybinding{
		{
			Key:     "-n C-s",
			Command: "agenc mission stop $AGENC_CALLING_MISSION_UUID",
			Comment: "stopMission — Stop Mission (-n C-s)",
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)

	// Should emit "bind-key -n C-s" verbatim
	if !strings.Contains(content, "bind-key -n C-s run-shell") {
		t.Error("expected global keybinding to use '-n C-s' verbatim")
	}

	// Should NOT wrap with the agenc table
	if strings.Contains(content, "bind-key -T agenc -n") {
		t.Error("expected global keybinding NOT to be wrapped with '-T agenc'")
	}
}

func TestGenerateKeybindingsContent_GlobalMissionScopedKeybinding(t *testing.T) {
	// Mission-scoped keybinding with "-n" prefix should use root table
	// but still include the resolve-mission preamble.
	keybindings := []CustomKeybinding{
		{
			Key:             "-n C-s",
			Command:         "agenc mission stop $AGENC_CALLING_MISSION_UUID",
			Comment:         "stopMission — Stop Mission (-n C-s)",
			IsMissionScoped: true,
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)

	// Should emit "bind-key -n C-s" verbatim
	if !strings.Contains(content, "bind-key -n C-s run-shell") {
		t.Error("expected global mission-scoped keybinding to use '-n C-s' verbatim")
	}

	// Should still contain resolve-mission preamble
	if !strings.Contains(content, "resolve-mission") {
		t.Error("expected global mission-scoped keybinding to contain resolve-mission")
	}

	// Should NOT wrap with the agenc table
	if strings.Contains(content, "bind-key -T agenc -n") {
		t.Error("expected global keybinding NOT to be wrapped with '-T agenc'")
	}
}

func TestGenerateKeybindingsContent_SingleQuotesInCommand(t *testing.T) {
	// Commands containing single quotes must be escaped so they don't break
	// the run-shell '...' wrapper. For example, the sideShell builtin uses:
	//   tmux split-window -h -c '#{pane_current_path}' $SHELL
	keybindings := []CustomKeybinding{
		{
			Key:     "-n C-p",
			Command: "tmux split-window -h -c '#{pane_current_path}' $SHELL",
			Comment: "sideShell — Side Shell (-n C-p)",
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)

	// The inner single quotes must be escaped as '\'' so tmux parses them correctly.
	// The full line should be: run-shell 'tmux split-window -h -c '\''#{pane_current_path}'\'' $SHELL'
	expected := `run-shell 'tmux split-window -h -c '\''#{pane_current_path}'\'' $SHELL'`
	if !strings.Contains(content, expected) {
		t.Errorf("expected single quotes to be escaped in run-shell wrapper\nwant substring: %s\ngot content:\n%s", expected, content)
	}
}

func TestGenerateKeybindingsContent_SingleQuotesInMissionScopedCommand(t *testing.T) {
	// Mission-scoped commands with single quotes should also be escaped.
	keybindings := []CustomKeybinding{
		{
			Key:             "p",
			Command:         "tmux split-window -h -c '#{pane_current_path}' $SHELL",
			Comment:         "test",
			IsMissionScoped: true,
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)

	// The escaped single quotes should appear in the mission-scoped preamble
	if strings.Contains(content, "-c '#{pane_current_path}' $SHELL'") {
		t.Error("expected single quotes in mission-scoped command to be escaped, but found unescaped form")
	}
	if !strings.Contains(content, `'\''#{pane_current_path}'\''`) {
		t.Error("expected single quotes to be escaped as '\\'\\'' in mission-scoped command")
	}
}

func TestBuildKeybindingsFromCommands_GlobalKeyComment(t *testing.T) {
	// Verify that "-n" prefixed keybindings get appropriate comments
	// (not "prefix + a, ...").
	resolved := []config.ResolvedPaletteCommand{
		{
			Name:           "stopMission",
			Title:          "Stop Mission",
			Command:        "agenc mission stop $AGENC_CALLING_MISSION_UUID",
			TmuxKeybinding: "-n C-s",
		},
	}

	keybindings := BuildKeybindingsFromCommands(resolved)

	if len(keybindings) != 1 {
		t.Fatalf("expected 1 keybinding, got %d", len(keybindings))
	}

	kb := keybindings[0]
	if strings.Contains(kb.Comment, "prefix + a") {
		t.Errorf("expected global keybinding comment NOT to mention 'prefix + a', got: %s", kb.Comment)
	}
	if !strings.Contains(kb.Comment, "-n C-s") {
		t.Errorf("expected global keybinding comment to contain '-n C-s', got: %s", kb.Comment)
	}
}
