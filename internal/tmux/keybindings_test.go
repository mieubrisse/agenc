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
	// the run-shell '...' wrapper.
	keybindings := []CustomKeybinding{
		{
			Key:     "-n C-p",
			Command: "bash -c 'echo hello'",
			Comment: "test — Test (-n C-p)",
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)

	// The inner single quotes must be escaped as '\'' so tmux parses them correctly.
	expected := `run-shell 'bash -c '\''echo hello'\'''`
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

// ---------------------------------------------------------------------------
// WrapCommand tests
// ---------------------------------------------------------------------------

func TestWrapCommand_Run(t *testing.T) {
	cmd := "agenc mission new"
	result := WrapCommand(cmd, config.ExecRun, false)
	if result != cmd {
		t.Errorf("ExecRun should return command unchanged\ngot:  %s\nwant: %s", result, cmd)
	}
}

func TestWrapCommand_Popup(t *testing.T) {
	result := WrapCommand("agenc tmux palette", config.ExecPopup, false)
	want := `tmux display-popup -E -w 68% -h 63% "agenc tmux palette"`
	if result != want {
		t.Errorf("ExecPopup wrapping incorrect\ngot:  %s\nwant: %s", result, want)
	}
}

func TestWrapCommand_Pane(t *testing.T) {
	result := WrapCommand("$SHELL", config.ExecPane, false)
	want := "tmux split-window -h $SHELL"
	if result != want {
		t.Errorf("ExecPane wrapping incorrect\ngot:  %s\nwant: %s", result, want)
	}
}

func TestWrapCommand_Window(t *testing.T) {
	result := WrapCommand("$SHELL", config.ExecWindow, false)
	want := "tmux new-window -a $SHELL"
	if result != want {
		t.Errorf("ExecWindow wrapping incorrect\ngot:  %s\nwant: %s", result, want)
	}
}

func TestWrapCommand_PaneMissionScoped(t *testing.T) {
	result := WrapCommand("$SHELL", config.ExecPane, true)
	if !strings.Contains(result, "split-window") {
		t.Error("expected pane command to contain split-window")
	}
	if !strings.Contains(result, "AGENC_DIRPATH") {
		t.Error("expected mission-scoped pane command to contain AGENC_DIRPATH working directory")
	}
	if !strings.Contains(result, "AGENC_CALLING_MISSION_UUID") {
		t.Error("expected mission-scoped pane command to reference calling mission UUID")
	}
}

func TestWrapCommand_WindowMissionScoped(t *testing.T) {
	result := WrapCommand("$SHELL", config.ExecWindow, true)
	if !strings.Contains(result, "new-window") {
		t.Error("expected window command to contain new-window")
	}
	if !strings.Contains(result, "AGENC_DIRPATH") {
		t.Error("expected mission-scoped window command to contain AGENC_DIRPATH working directory")
	}
	if !strings.Contains(result, "AGENC_CALLING_MISSION_UUID") {
		t.Error("expected mission-scoped window command to reference calling mission UUID")
	}
}

func TestWrapCommand_PopupEscapesDoubleQuotes(t *testing.T) {
	result := WrapCommand(`echo "hello"`, config.ExecPopup, false)
	want := `tmux display-popup -E -w 68% -h 63% "echo \"hello\""`
	if result != want {
		t.Errorf("ExecPopup should escape double quotes\ngot:  %s\nwant: %s", result, want)
	}
}

func TestWrapCommand_PopupMissionScopedNoWorkdir(t *testing.T) {
	// Popup mode should NOT add a working directory flag even when mission-scoped,
	// because display-popup uses a different mechanism.
	result := WrapCommand("agenc cmd", config.ExecPopup, true)
	if strings.Contains(result, "AGENC_DIRPATH") {
		t.Error("expected popup mode NOT to add working directory flag")
	}
	if !strings.Contains(result, "display-popup") {
		t.Error("expected popup wrapping")
	}
}

// ---------------------------------------------------------------------------
// GenerateKeybindingsContent tests for execution modes
// ---------------------------------------------------------------------------

func TestGenerateKeybindingsContent_PopupMode(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:           "n",
			Command:       "agenc mission new",
			Comment:       "newMission (prefix + a, n)",
			ExecutionMode: config.ExecPopup,
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)

	if !strings.Contains(content, "display-popup") {
		t.Errorf("expected popup keybinding to contain display-popup\ngot:\n%s", content)
	}
}

func TestGenerateKeybindingsContent_PaneMode(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:           "-n C-p",
			Command:       "$SHELL",
			Comment:       "sideShell (-n C-p)",
			ExecutionMode: config.ExecPane,
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)

	if !strings.Contains(content, "split-window") {
		t.Errorf("expected pane keybinding to contain split-window\ngot:\n%s", content)
	}
}

func TestGenerateKeybindingsContent_WindowMode(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:           "w",
			Command:       "$SHELL",
			Comment:       "shell (prefix + a, w)",
			ExecutionMode: config.ExecWindow,
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)

	if !strings.Contains(content, "new-window") {
		t.Errorf("expected window keybinding to contain new-window\ngot:\n%s", content)
	}
}

func TestGenerateKeybindingsContent_PopupFallbackOnOldTmux(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:           "n",
			Command:       "agenc mission new",
			Comment:       "newMission (prefix + a, n)",
			ExecutionMode: config.ExecPopup,
		},
	}

	// tmux 3.1 does not support display-popup (requires >= 3.2)
	content := GenerateKeybindingsContent(3, 1, "-T agenc k", keybindings)

	if strings.Contains(content, "display-popup") {
		t.Error("expected popup keybinding to fall back to run mode on tmux < 3.2")
	}

	// Should still contain the command in a simple run-shell form
	if !strings.Contains(content, "run-shell") {
		t.Error("expected fallback keybinding to use run-shell")
	}
	if !strings.Contains(content, "agenc mission new") {
		t.Error("expected fallback keybinding to contain the original command")
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
