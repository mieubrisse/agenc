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

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings, "")

	// Should contain resolve-mission preamble for mission-scoped keybinding
	if !strings.Contains(content, "resolve-mission") {
		t.Error("expected mission-scoped keybinding to contain resolve-mission preamble")
	}

	// Should contain the guard that skips execution when UUID is empty
	if !strings.Contains(content, "[ -n \"$AGENC_CALLING_MISSION_UUID\" ]") {
		t.Error("expected mission-scoped keybinding to contain non-empty UUID guard")
	}
}

func TestGenerateKeybindingsContent_MissionScopedFallsBackGracefully(t *testing.T) {
	// When no mission resolves for the pane (a plain shell pane, or a pane
	// restored by tmux-resurrect whose wrapper is gone), the keybinding must not
	// exit non-zero — tmux run-shell surfaces that as a "returned 1" error
	// overlay. It should instead show a status-line message and exit 0.
	keybindings := []CustomKeybinding{
		{
			Key:             "x",
			Command:         "agenc mission detach $AGENC_CALLING_MISSION_UUID",
			Comment:         "detachMission — Detach Mission (prefix + a, x)",
			IsMissionScoped: true,
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings, "")

	// Empty-UUID case falls back to a display-message rather than exiting 1.
	if !strings.Contains(content, "else tmux display-message") {
		t.Errorf("expected mission-scoped keybinding to fall back to display-message when no mission resolves\ngot:\n%s", content)
	}

	// The old `] && AGENC_CALLING_PANE_ID` guard form exits 1 on empty UUID;
	// ensure it is gone so the regression does not reappear.
	if strings.Contains(content, "] && AGENC_CALLING_PANE_ID") {
		t.Errorf("expected mission-scoped keybinding NOT to use the `&&` guard that exits non-zero on empty UUID\ngot:\n%s", content)
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

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings, "")

	// Should contain run-shell with both AGENC_CALLING_PANE_ID and
	// AGENC_CALLING_SESSION_NAME (the latter is what attach/detach use).
	if !strings.Contains(content, "run-shell 'AGENC_CALLING_PANE_ID=#{pane_id} AGENC_CALLING_SESSION_NAME=#{session_name} agenc mission new'") {
		t.Errorf("expected non-mission-scoped keybinding to include AGENC_CALLING_PANE_ID and AGENC_CALLING_SESSION_NAME\ngot:\n%s", content)
	}

	// The keybinding line itself should NOT contain resolve-mission
	// (palette keybinding does, but that's separate)
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		if strings.Contains(line, `bind-key -T agenc "n"`) {
			if strings.Contains(line, "resolve-mission") {
				t.Error("expected non-mission-scoped keybinding to not contain resolve-mission")
			}
			break
		}
	}
}

func TestGenerateKeybindingsContent_PaletteIncludesResolveMission(t *testing.T) {
	content := GenerateKeybindingsContent(3, 4, "-T agenc k", nil, "")

	// Palette keybinding (k) should always include resolve-mission
	if !strings.Contains(content, "resolve-mission") {
		t.Error("expected palette keybinding to include resolve-mission")
	}

	// Should pass UUID into the popup environment
	if !strings.Contains(content, "AGENC_CALLING_MISSION_UUID") {
		t.Error("expected palette keybinding to pass AGENC_CALLING_MISSION_UUID")
	}

	// Should pass calling pane ID into the popup environment
	if !strings.Contains(content, "AGENC_CALLING_PANE_ID=#{pane_id}") {
		t.Error("expected palette keybinding to pass AGENC_CALLING_PANE_ID with #{pane_id}")
	}

	// Should pass calling session name into the popup environment so that
	// attach/detach can identify the user's actual session (which is ambiguous
	// from pane ID alone when a mission's window is linked into multiple
	// sessions).
	if !strings.Contains(content, "AGENC_CALLING_SESSION_NAME=#{session_name}") {
		t.Error("expected palette keybinding to pass AGENC_CALLING_SESSION_NAME with #{session_name}")
	}

	// Should use #{pane_id} directly, not via display-message
	if strings.Contains(content, "display-message") {
		t.Error("expected keybindings to use #{pane_id} directly, not via display-message")
	}
}

func TestGenerateKeybindingsContent_PaletteOmittedOnOldTmux(t *testing.T) {
	content := GenerateKeybindingsContent(3, 1, "-T agenc k", nil, "")

	// tmux < 3.2 should not have the palette keybinding
	if strings.Contains(content, "display-popup") {
		t.Error("expected palette keybinding to be omitted on tmux < 3.2")
	}
}

func TestGenerateKeybindingsContent_CustomPaletteKey(t *testing.T) {
	content := GenerateKeybindingsContent(3, 4, "-T agenc p", nil, "")

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
	content := GenerateKeybindingsContent(3, 4, "C-k", nil, "")

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

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings, "")

	// Should emit "bind-key -n "C-s"" with the key double-quoted
	if !strings.Contains(content, `bind-key -n "C-s" run-shell`) {
		t.Error(`expected global keybinding to use '-n "C-s"'`)
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

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings, "")

	// Should emit "bind-key -n "C-s"" with the key double-quoted
	if !strings.Contains(content, `bind-key -n "C-s" run-shell`) {
		t.Error(`expected global mission-scoped keybinding to use '-n "C-s"'`)
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

func TestGenerateKeybindingsContent_DisplayPopupKeybindingInjectsPaneID(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:     "a",
			Command: `tmux display-popup -E -w 80% -h 80% "agenc mission attach"`,
			Comment: "attachMission — Attach Mission (prefix + a, a)",
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings, "")

	// Should inject -e AGENC_CALLING_PANE_ID=#{pane_id} and
	// -e AGENC_CALLING_SESSION_NAME=#{session_name} into the display-popup flags
	if !strings.Contains(content, "display-popup -e AGENC_CALLING_PANE_ID=#{pane_id} -e AGENC_CALLING_SESSION_NAME=#{session_name}") {
		t.Errorf("expected display-popup keybinding to inject AGENC_CALLING_PANE_ID and AGENC_CALLING_SESSION_NAME\ncontent:\n%s", content)
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

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings, "")

	// The inner single quotes must be escaped as '\'' so tmux parses them correctly.
	expected := `run-shell 'AGENC_CALLING_PANE_ID=#{pane_id} AGENC_CALLING_SESSION_NAME=#{session_name} bash -c '\''echo hello'\'''`
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

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings, "")

	// The escaped single quotes should appear in the mission-scoped preamble
	if strings.Contains(content, "-c '#{pane_current_path}' $SHELL'") {
		t.Error("expected single quotes in mission-scoped command to be escaped, but found unescaped form")
	}
	if !strings.Contains(content, `'\''#{pane_current_path}'\''`) {
		t.Error("expected single quotes to be escaped as '\\'\\'' in mission-scoped command")
	}
}

// display-popup -e arrived in tmux 3.3, so 3.2 (which ParseVersion also reports
// for 3.2a) must get the explanatory message and 3.3 the real popup (KEV-341).
func TestSupportsPopupEnvFlag(t *testing.T) {
	cases := []struct {
		major, minor int
		want         bool
	}{
		{2, 9, false},
		{3, 1, false},
		{3, 2, false},
		{3, 3, true},
		{3, 6, true},
		{4, 0, true},
	}
	for _, c := range cases {
		if got := SupportsPopupEnvFlag(c.major, c.minor); got != c.want {
			t.Errorf("SupportsPopupEnvFlag(%d, %d) = %v, want %v", c.major, c.minor, got, c.want)
		}
	}
}

func TestGenerateKeybindingsContent_PopupKeybindingGatedAtTmux33(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:     "-n C-n",
			Command: `tmux display-popup -E -w 95% -h 90% "agenc notification manage"`,
			Comment: "showNotifications (-n C-n)",
		},
		{
			Key:             "r",
			Command:         `tmux display-popup -E -w 68% -h 63% "agenc mission rename $AGENC_CALLING_MISSION_UUID"`,
			Comment:         "renameSession (prefix + a, r)",
			IsMissionScoped: true,
		},
	}
	wantMessageBindings := []string{
		`bind-key -n "C-n" ` + PopupUnsupportedMessageCommand + "\n",
		`bind-key -T agenc "r" ` + PopupUnsupportedMessageCommand + "\n",
	}

	for _, minor := range []int{1, 2} {
		content := GenerateKeybindingsContent(3, minor, "-T agenc k", keybindings, "")
		if strings.Contains(content, "-e AGENC_CALLING") {
			t.Errorf("tmux 3.%d: expected no display-popup -e, which tmux < 3.3 rejects\ncontent:\n%s", minor, content)
		}
		for _, want := range wantMessageBindings {
			if !strings.Contains(content, want) {
				t.Errorf("tmux 3.%d: expected popup key bound to the tmux 3.3 message\nwant substring: %s\ncontent:\n%s", minor, want, content)
			}
		}
	}

	content := GenerateKeybindingsContent(3, 3, "-T agenc k", keybindings, "")
	if strings.Contains(content, PopupUnsupportedMessageCommand) {
		t.Errorf("tmux 3.3: expected real popup bindings, got the unsupported message\ncontent:\n%s", content)
	}
	if strings.Count(content, "display-popup -e AGENC_CALLING_PANE_ID=#{pane_id} -e AGENC_CALLING_SESSION_NAME=#{session_name}") != 2 {
		t.Errorf("tmux 3.3: expected both popup bindings to inject -e\ncontent:\n%s", content)
	}
}

// The palette's own popup takes no -e and was observed working on tmux 3.2a,
// so it stays available on 3.2 while popup commands are gated at 3.3.
func TestGenerateKeybindingsContent_PaletteKeptOnTmux32(t *testing.T) {
	content := GenerateKeybindingsContent(3, 2, "-n C-y", nil, "")
	if !strings.Contains(content, "bind-key -n C-y run-shell") {
		t.Errorf("expected the palette binding on tmux 3.2\ncontent:\n%s", content)
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

func TestGenerateKeybindingsContent_OutputRedirect(t *testing.T) {
	logFilepath := "/tmp/test/palette.log"

	// Non-mission-scoped command should have output redirected
	keybindings := []CustomKeybinding{
		{
			Key:     "d",
			Command: "agenc discord",
			Comment: "joinDiscord (prefix + a, d)",
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings, logFilepath)

	expectedRedirect := ">> /tmp/test/palette.log 2>&1"
	if !strings.Contains(content, expectedRedirect) {
		t.Errorf("expected output redirect in keybinding\nwant substring: %s\ngot:\n%s", expectedRedirect, content)
	}
}

func TestGenerateKeybindingsContent_OutputRedirectMissionScoped(t *testing.T) {
	logFilepath := "/tmp/test/palette.log"

	keybindings := []CustomKeybinding{
		{
			Key:             "s",
			Command:         "agenc mission stop $AGENC_CALLING_MISSION_UUID",
			Comment:         "stopMission (prefix + a, s)",
			IsMissionScoped: true,
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings, logFilepath)

	expectedRedirect := ">> /tmp/test/palette.log 2>&1"
	if !strings.Contains(content, expectedRedirect) {
		t.Errorf("expected output redirect in mission-scoped keybinding\nwant substring: %s\ngot:\n%s", expectedRedirect, content)
	}
}

func TestGenerateKeybindingsContent_NoRedirectWhenLogEmpty(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:     "d",
			Command: "agenc discord",
			Comment: "joinDiscord (prefix + a, d)",
		},
	}

	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings, "")

	if strings.Contains(content, ">>") {
		t.Errorf("expected no output redirect when logFilepath is empty\ngot:\n%s", content)
	}
}
