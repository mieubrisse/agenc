Palette Command Execution Modes Implementation Plan
====================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace the ad-hoc command dispatch system with a unified `executionMode` field so keybindings and the palette dispatch commands identically.

**Architecture:** Add an `ExecutionMode` enum (`run`, `popup`, `pane`, `window`) to `PaletteCommandConfig`. A shared `WrapCommand` function generates the tmux wrapper for any mode. The keybinding generator calls it at generation time; the palette calls it at selection time and hands off via `tmux run-shell -b`. The `DisplayPopup` field, `builtinDisplayPopupCommands` map, and embedded tmux primitives in command strings are all removed.

**Tech Stack:** Go, tmux, Cobra CLI

**Design doc:** `docs/plans/2026-03-02-palette-command-execution-modes-design.md`

---

Task 1: Add ExecutionMode type and field to config structs
----------------------------------------------------------

**Files:**
- Modify: `internal/config/agenc_config.go:77-91` (PaletteCommandConfig)
- Modify: `internal/config/agenc_config.go:235-245` (ResolvedPaletteCommand)
- Modify: `internal/config/agenc_config.go:88-91` (IsEmpty)
- Test: `internal/config/agenc_config_test.go`

**Step 1: Write the failing test**

Add to `internal/config/agenc_config_test.go`:

```go
func TestExecutionModeValidation(t *testing.T) {
	tests := []struct {
		mode  ExecutionMode
		valid bool
	}{
		{ExecRun, true},
		{ExecPopup, true},
		{ExecPane, true},
		{ExecWindow, true},
		{ExecutionMode("invalid"), false},
		{ExecutionMode(""), true}, // empty defaults to run
	}
	for _, tt := range tests {
		if tt.mode.IsValid() != tt.valid {
			t.Errorf("ExecutionMode(%q).IsValid() = %v, want %v", tt.mode, !tt.valid, tt.valid)
		}
	}
}

func TestPaletteCommandConfigIsEmpty_IgnoresExecutionMode(t *testing.T) {
	// ExecutionMode should NOT affect IsEmpty (which detects "disable" entries)
	cfg := PaletteCommandConfig{ExecutionMode: ExecPopup}
	if !cfg.IsEmpty() {
		t.Error("expected IsEmpty() == true when only ExecutionMode is set")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd internal/config && go test -run TestExecutionMode -v`
Expected: FAIL — `ExecutionMode` type not defined

**Step 3: Implement**

In `internal/config/agenc_config.go`, add the type and constants (after the existing `CallingMissionUUIDEnvVar` const block around line 233):

```go
// ExecutionMode defines how a palette command runs in tmux.
type ExecutionMode string

const (
	// ExecRun executes the command via run-shell (fire-and-forget, no TTY).
	ExecRun ExecutionMode = "run"
	// ExecPopup opens a tmux display-popup with a TTY for interactive input.
	ExecPopup ExecutionMode = "popup"
	// ExecPane opens a tmux split-window in the current window.
	ExecPane ExecutionMode = "pane"
	// ExecWindow opens a tmux new-window in the current session.
	ExecWindow ExecutionMode = "window"
)

// IsValid returns true if the mode is a recognized execution mode or empty
// (empty defaults to run).
func (m ExecutionMode) IsValid() bool {
	switch m {
	case "", ExecRun, ExecPopup, ExecPane, ExecWindow:
		return true
	default:
		return false
	}
}
```

Add `ExecutionMode` field to `PaletteCommandConfig`:

```go
type PaletteCommandConfig struct {
	Title          string        `yaml:"title,omitempty"`
	Description    string        `yaml:"description,omitempty"`
	Command        string        `yaml:"command,omitempty"`
	TmuxKeybinding string        `yaml:"tmuxKeybinding,omitempty"`
	ExecutionMode  ExecutionMode `yaml:"executionMode,omitempty"`
}
```

`IsEmpty()` must NOT check `ExecutionMode` — a config entry with only `executionMode` set is still a "disable" override. Keep the existing check unchanged (it already only checks Title, Description, Command, TmuxKeybinding).

Add `ExecutionMode` field to `ResolvedPaletteCommand`, replacing `DisplayPopup`:

```go
type ResolvedPaletteCommand struct {
	Name           string
	Title          string
	Description    string
	Command        string
	TmuxKeybinding string
	IsBuiltin      bool
	ExecutionMode  ExecutionMode
}
```

**Step 4: Run test to verify it passes**

Run: `cd internal/config && go test -run TestExecutionMode -v && go test -run TestPaletteCommandConfigIsEmpty -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/config/agenc_config.go internal/config/agenc_config_test.go
git commit -m "Add ExecutionMode type and field to palette command config"
```

---

Task 2: Update builtin commands with execution modes and remove DisplayPopup
-----------------------------------------------------------------------------

**Files:**
- Modify: `internal/config/agenc_config.go:93-199` (BuiltinPaletteCommands, builtinDisplayPopupCommands)
- Modify: `internal/config/agenc_config.go:663-731` (GetResolvedPaletteCommands)

**Step 1: Write the failing test**

Add to `internal/config/agenc_config_test.go`:

```go
func TestBuiltinExecutionModes(t *testing.T) {
	// Verify key builtins have the expected execution mode
	expectations := map[string]ExecutionMode{
		"quickClaude":   ExecRun,
		"newMission":    ExecPopup,
		"switchMission": ExecPopup,
		"renameSession": ExecPopup,
		"resumeMission": ExecPopup,
		"removeMission": ExecPopup,
		"nukeMissions":  ExecPopup,
		"sideShell":     ExecPane,
		"shell":         ExecWindow,
		"stopMission":   ExecRun,
	}
	for name, expectedMode := range expectations {
		builtin, ok := BuiltinPaletteCommands[name]
		if !ok {
			t.Errorf("expected builtin %q to exist", name)
			continue
		}
		if builtin.ExecutionMode != expectedMode {
			t.Errorf("builtin %q: ExecutionMode = %q, want %q", name, builtin.ExecutionMode, expectedMode)
		}
	}
}

func TestBuiltinCommandsNoEmbeddedTmuxPrimitives(t *testing.T) {
	// No builtin command string should start with "tmux new-window" or "tmux split-window"
	// because execution context is now controlled by ExecutionMode
	for name, cmd := range BuiltinPaletteCommands {
		if strings.HasPrefix(cmd.Command, "tmux new-window") {
			t.Errorf("builtin %q embeds 'tmux new-window' in command; use ExecutionMode instead", name)
		}
		if strings.HasPrefix(cmd.Command, "tmux split-window") {
			t.Errorf("builtin %q embeds 'tmux split-window' in command; use ExecutionMode instead", name)
		}
	}
}
```

You will need to add `"strings"` to the test file imports.

**Step 2: Run test to verify it fails**

Run: `cd internal/config && go test -run TestBuiltin -v`
Expected: FAIL — modes not set, commands still embed tmux primitives

**Step 3: Implement**

Update `BuiltinPaletteCommands` map. Key changes:
- Add `ExecutionMode` to every entry (defaults to `ExecRun` if blank, but be explicit for clarity)
- Remove `tmux new-window -a` prefix from `resumeMission`, `removeMission`, `nukeMissions`
- Remove `tmux split-window -h -c ...` from `sideShell` — command becomes `$SHELL`
- Remove `tmux new-window -a -c ...` from `shell` — command becomes `$SHELL`
- Add `ExecutionMode: ExecPopup` to `newMission`, `switchMission`, `renameSession`, `resumeMission`, `removeMission`, `nukeMissions`
- Add `ExecutionMode: ExecPane` to `sideShell`
- Add `ExecutionMode: ExecWindow` to `shell`

```go
var BuiltinPaletteCommands = map[string]PaletteCommandConfig{
	"quickClaude": {
		Title:         "🦀  Quick Claude",
		Description:   "Creates a blank mission in a new window",
		Command:       "agenc mission new --blank",
		ExecutionMode: ExecRun,
	},
	"talkToAgenc": {
		Title:         "🤖  Adjutant",
		Description:   "Launch an Adjutant mission in a new window",
		Command:       "agenc mission new --adjutant",
		ExecutionMode: ExecRun,
	},
	"newMission": {
		Title:          "🚀  New Mission",
		Description:    "Create a new mission and launch Claude",
		Command:        "agenc mission new",
		TmuxKeybinding: "-n C-n",
		ExecutionMode:  ExecPopup,
	},
	"switchMission": {
		Title:          "🔀  Switch Mission",
		Description:    "Switch to a running mission's tmux window",
		Command:        "agenc tmux switch",
		TmuxKeybinding: "-n C-m",
		ExecutionMode:  ExecPopup,
	},
	"resumeMission": {
		Title:         "🟢  Resume Mission",
		Description:   "Resume a stopped mission with claude --continue",
		Command:       "agenc mission resume",
		ExecutionMode: ExecPopup,
	},
	"sideShell": {
		Title:          "🐚  Side Shell",
		Description:    "Split pane and open a shell in the current mission's workspace",
		Command:        "$SHELL",
		TmuxKeybinding: "-n C-p",
		ExecutionMode:  ExecPane,
	},
	"shell": {
		Title:         "🐚  Shell",
		Description:   "Open a shell in a new window",
		Command:       "$SHELL",
		ExecutionMode: ExecWindow,
	},
	"copyMissionUuid": {
		Title:         "📋  Copy Mission ID",
		Description:   "Copy the focused mission's UUID to the clipboard",
		Command:       "printf '%s' $AGENC_CALLING_MISSION_UUID | pbcopy",
		ExecutionMode: ExecRun,
	},
	"renameSession": {
		Title:          "✨  Rename Session",
		Description:    "Rename the focused mission's window",
		Command:        "agenc mission rename $AGENC_CALLING_MISSION_UUID",
		TmuxKeybinding: "-n C-.",
		ExecutionMode:  ExecPopup,
	},
	"stopMission": {
		Title:          "🛑  Stop Mission",
		Description:    "Stop the mission in the focused pane",
		Command:        "agenc mission stop $AGENC_CALLING_MISSION_UUID",
		TmuxKeybinding: "-n C-s",
		ExecutionMode:  ExecRun,
	},
	"reconfigMission": {
		Title:         "🔧  Reconfig & Reload Mission",
		Description:   "Update the mission's config and restart to apply changes",
		Command:       "agenc mission reconfig $AGENC_CALLING_MISSION_UUID && agenc mission reload $AGENC_CALLING_MISSION_UUID",
		ExecutionMode: ExecRun,
	},
	"reloadMission": {
		Title:         "🔄  Reload Mission",
		Description:   "Stop and restart the mission in the focused pane",
		Command:       "agenc mission reload $AGENC_CALLING_MISSION_UUID",
		ExecutionMode: ExecRun,
	},
	"removeMission": {
		Title:         "❌  Remove Mission",
		Description:   "Remove a mission and its directory",
		Command:       "agenc mission rm",
		ExecutionMode: ExecPopup,
	},
	"nukeMissions": {
		Title:         "💥  Nuke Missions",
		Description:   "Remove all archived missions",
		Command:       "agenc mission nuke",
		ExecutionMode: ExecPopup,
	},
	"sendFeedback": {
		Title:         "💬  Send Feedback",
		Description:   "Send feedback about AgenC",
		Command:       "agenc mission new --adjutant --prompt \"I'd like to send feedback about AgenC\"",
		ExecutionMode: ExecRun,
	},
	"joinDiscord": {
		Title:         "👾  Join the Discord",
		Description:   "Join the AgenC Discord community",
		Command:       "agenc discord",
		ExecutionMode: ExecRun,
	},
	"starAgenc": {
		Title:         "⭐  Star AgenC on Github",
		Description:   "Open the AgenC GitHub repo in your browser",
		Command:       "agenc star",
		ExecutionMode: ExecRun,
	},
	"exitTmux": {
		Title:         "🚪  Detach (Exit)",
		Description:   "Detach from tmux (session stays running; reattach anytime)",
		Command:       "tmux detach",
		ExecutionMode: ExecRun,
	},
}
```

Delete the `builtinDisplayPopupCommands` map entirely (lines 193-199).

Update `GetResolvedPaletteCommands()` (line 683): replace `DisplayPopup: builtinDisplayPopupCommands[name]` with `ExecutionMode: builtin.ExecutionMode`. For custom commands (line 719-727), add `ExecutionMode: cmdCfg.ExecutionMode`.

In the builtin override merge block (lines 687-700), add execution mode merging:

```go
if override.ExecutionMode != "" {
	resolved.ExecutionMode = override.ExecutionMode
}
```

**Step 4: Run tests**

Run: `cd internal/config && go test -v`
Expected: PASS. Some compilation errors may appear in `internal/tmux/keybindings.go` due to the removed `DisplayPopup` field — that's expected and will be fixed in Task 3.

Actually, run the config tests only first:
Run: `cd internal/config && go test -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/config/agenc_config.go internal/config/agenc_config_test.go
git commit -m "Set execution modes on builtins, remove builtinDisplayPopupCommands"
```

---

Task 3: Add WrapCommand function and update keybinding generation
-----------------------------------------------------------------

**Files:**
- Modify: `internal/tmux/keybindings.go:29-37` (CustomKeybinding struct)
- Modify: `internal/tmux/keybindings.go:45-124` (GenerateKeybindingsContent)
- Modify: `internal/tmux/keybindings.go:142-169` (BuildKeybindingsFromCommands)
- Test: `internal/tmux/keybindings_test.go`

**Step 1: Write the failing tests**

Add to `internal/tmux/keybindings_test.go`:

```go
func TestWrapCommand_Run(t *testing.T) {
	result := WrapCommand("agenc mission stop $UUID", config.ExecRun, false)
	if result != "agenc mission stop $UUID" {
		t.Errorf("ExecRun should return command unchanged, got: %s", result)
	}
}

func TestWrapCommand_Popup(t *testing.T) {
	result := WrapCommand("agenc mission new", config.ExecPopup, false)
	if !strings.Contains(result, "display-popup") {
		t.Errorf("ExecPopup should wrap in display-popup, got: %s", result)
	}
	if !strings.Contains(result, "agenc mission new") {
		t.Errorf("ExecPopup should contain the original command, got: %s", result)
	}
}

func TestWrapCommand_Pane(t *testing.T) {
	result := WrapCommand("$SHELL", config.ExecPane, false)
	if !strings.Contains(result, "split-window") {
		t.Errorf("ExecPane should wrap in split-window, got: %s", result)
	}
}

func TestWrapCommand_Window(t *testing.T) {
	result := WrapCommand("$SHELL", config.ExecWindow, false)
	if !strings.Contains(result, "new-window") {
		t.Errorf("ExecWindow should wrap in new-window, got: %s", result)
	}
}

func TestWrapCommand_PaneMissionScoped(t *testing.T) {
	result := WrapCommand("$SHELL", config.ExecPane, true)
	// Should include the mission workspace directory
	if !strings.Contains(result, "AGENC_CALLING_MISSION_UUID") {
		t.Errorf("mission-scoped pane should use mission workspace dir, got: %s", result)
	}
	if !strings.Contains(result, "split-window") {
		t.Errorf("ExecPane should wrap in split-window, got: %s", result)
	}
}

func TestWrapCommand_WindowMissionScoped(t *testing.T) {
	result := WrapCommand("$SHELL", config.ExecWindow, true)
	if !strings.Contains(result, "AGENC_CALLING_MISSION_UUID") {
		t.Errorf("mission-scoped window should use mission workspace dir, got: %s", result)
	}
	if !strings.Contains(result, "new-window") {
		t.Errorf("ExecWindow should wrap in new-window, got: %s", result)
	}
}

func TestGenerateKeybindingsContent_PopupMode(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:           "-n C-n",
			Command:       "agenc mission new",
			Comment:       "newMission",
			ExecutionMode: config.ExecPopup,
		},
	}
	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)
	if !strings.Contains(content, "display-popup") {
		t.Error("popup mode keybinding should contain display-popup")
	}
}

func TestGenerateKeybindingsContent_PaneMode(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:             "-n C-p",
			Command:         "$SHELL",
			Comment:         "sideShell",
			IsMissionScoped: true,
			ExecutionMode:   config.ExecPane,
		},
	}
	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)
	if !strings.Contains(content, "split-window") {
		t.Error("pane mode keybinding should contain split-window")
	}
}

func TestGenerateKeybindingsContent_WindowMode(t *testing.T) {
	keybindings := []CustomKeybinding{
		{
			Key:             "w",
			Command:         "$SHELL",
			Comment:         "shell",
			IsMissionScoped: true,
			ExecutionMode:   config.ExecWindow,
		},
	}
	content := GenerateKeybindingsContent(3, 4, "-T agenc k", keybindings)
	if !strings.Contains(content, "new-window") {
		t.Error("window mode keybinding should contain new-window")
	}
}

func TestGenerateKeybindingsContent_PopupOmittedOnOldTmux(t *testing.T) {
	// Popup mode should fall back to run mode on tmux < 3.2
	keybindings := []CustomKeybinding{
		{
			Key:           "-n C-n",
			Command:       "agenc mission new",
			Comment:       "newMission",
			ExecutionMode: config.ExecPopup,
		},
	}
	content := GenerateKeybindingsContent(3, 1, "-T agenc k", keybindings)
	if strings.Contains(content, "display-popup") {
		t.Error("popup mode should fall back on tmux < 3.2")
	}
	// Should still generate the command as run mode
	if !strings.Contains(content, "run-shell") {
		t.Error("popup fallback should use run-shell")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd internal/tmux && go test -run TestWrapCommand -v`
Expected: FAIL — `WrapCommand` not defined

**Step 3: Implement**

Replace `DisplayPopup bool` with `ExecutionMode config.ExecutionMode` on `CustomKeybinding`:

```go
type CustomKeybinding struct {
	Key             string
	Command         string
	Comment         string
	IsMissionScoped bool
	ExecutionMode   config.ExecutionMode
}
```

Add the `WrapCommand` function:

```go
// WrapCommand wraps a command string in the appropriate tmux primitive based
// on execution mode. For pane and window modes, if missionScoped is true,
// the working directory is set to the mission's agent directory.
func WrapCommand(command string, mode config.ExecutionMode, missionScoped bool) string {
	workdirFlag := ""
	if missionScoped && (mode == config.ExecPane || mode == config.ExecWindow) {
		workdirFlag = ` -c "${AGENC_DIRPATH:-$HOME/.agenc}/missions/$AGENC_CALLING_MISSION_UUID/agent"`
	}

	switch mode {
	case config.ExecPopup:
		return fmt.Sprintf(`tmux display-popup -E "%s"`, strings.ReplaceAll(command, `"`, `\"`))
	case config.ExecPane:
		return fmt.Sprintf("tmux split-window -h%s %s", workdirFlag, command)
	case config.ExecWindow:
		return fmt.Sprintf("tmux new-window -a%s %s", workdirFlag, command)
	default:
		return command
	}
}
```

Rewrite the `GenerateKeybindingsContent` dispatch loop. Replace the `usePopup` logic (lines 88-120) with a mode-based approach:

```go
for _, kb := range customKeybindings {
	sb.WriteString("\n")
	if kb.Comment != "" {
		fmt.Fprintf(&sb, "# %s\n", kb.Comment)
	}

	bindKeyArgs := fmt.Sprintf("-T %s %s", agencKeyTable, kb.Key)
	if strings.HasPrefix(kb.Key, "-") {
		bindKeyArgs = kb.Key
	}

	// Determine effective mode: popup falls back to run on old tmux
	effectiveMode := kb.ExecutionMode
	if effectiveMode == config.ExecPopup && !(tmuxMajor > 3 || (tmuxMajor == 3 && tmuxMinor >= 2)) {
		effectiveMode = config.ExecRun
	}

	wrappedCommand := WrapCommand(kb.Command, effectiveMode, kb.IsMissionScoped)
	escapedWrapped := escapeSingleQuotes(wrappedCommand)

	if kb.IsMissionScoped {
		fmt.Fprintf(&sb, "bind-key %s run-shell '"+
			"AGENC_CALLING_MISSION_UUID=$(%s tmux resolve-mission \"#{pane_id}\"); "+
			"[ -n \"$AGENC_CALLING_MISSION_UUID\" ] && %s"+
			"'\n", bindKeyArgs, agencBinary, escapedWrapped)
	} else {
		fmt.Fprintf(&sb, "bind-key %s run-shell '%s'\n", bindKeyArgs, escapedWrapped)
	}
}
```

Update `BuildKeybindingsFromCommands` to use `ExecutionMode` instead of `DisplayPopup`:

```go
keybindings = append(keybindings, CustomKeybinding{
	Key:             cmd.TmuxKeybinding,
	Command:         cmd.Command,
	Comment:         comment,
	IsMissionScoped: cmd.IsMissionScoped(),
	ExecutionMode:   cmd.ExecutionMode,
})
```

**Step 4: Run tests**

Run: `cd internal/tmux && go test -v`
Expected: PASS

Some existing tests may need minor updates since the generated keybinding format changes. In particular:
- `TestGenerateKeybindingsContent_SingleQuotesInCommand` — the sideShell command is now `$SHELL` (no embedded `tmux split-window`), so this test's command may need updating to still test single-quote escaping in a meaningful way.

Update old tests as needed to match the new format, keeping the behavior they verify.

**Step 5: Commit**

```
git add internal/tmux/keybindings.go internal/tmux/keybindings_test.go
git commit -m "Add WrapCommand function, rewrite keybinding generation to use ExecutionMode"
```

---

Task 4: Update palette dispatch to use WrapCommand + run-shell -b
-----------------------------------------------------------------

**Files:**
- Modify: `cmd/tmux_palette.go:160-170` (dispatch section)

**Step 1: Implement**

Replace the dispatch block (lines 160-170) in `runTmuxPalette`:

```go
// Dispatch: compute the wrapped command (same wrapping as keybindings)
// and hand it off to the tmux server via run-shell -b. This closes the
// palette popup first, then tmux executes the command asynchronously.
wrappedCommand := tmux.WrapCommand(selectedEntry.Command, selectedEntry.ExecutionMode, selectedEntry.IsMissionScoped())
escapedCommand := strings.ReplaceAll(wrappedCommand, "'", `'\''`)

runShellCmd := exec.Command("tmux", "run-shell", "-b", escapedCommand)
runShellCmd.Stdout = os.Stdout
runShellCmd.Stderr = os.Stderr

if err := runShellCmd.Run(); err != nil {
	return stacktrace.Propagate(err, "failed to dispatch palette selection via tmux run-shell")
}

return nil
```

Add `"github.com/odyssey/agenc/internal/tmux"` to the import block.

Also update the command's Long description (line 18) to reflect the new dispatch mechanism:

```go
Long: `Presents an fzf-based command picker inside a tmux display-popup.
On selection, the chosen command is dispatched to the tmux server via
run-shell -b with the appropriate execution mode wrapping (popup, pane,
window, or direct). On cancel (Ctrl-C or Esc), the popup closes with
no action.

This command is designed to be invoked by the palette keybinding
(prefix + a, k).`,
```

**Step 2: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS — full build + test suite

**Step 3: Commit**

```
git add cmd/tmux_palette.go
git commit -m "Update palette dispatch to use WrapCommand + tmux run-shell -b"
```

---

Task 5: Remove tmux switch auto-popup and feedback.go tmux wrapper
------------------------------------------------------------------

**Files:**
- Modify: `cmd/tmux_switch.go:40-50` (auto-popup block)
- Modify: `cmd/feedback.go` (tmux new-window wrapper)

**Step 1: Implement tmux_switch.go change**

Remove the auto-popup TTY detection block (lines 40-50 in `tmux_switch.go`):

```go
// Delete these lines:
// Auto-wrap in a popup if we're running without a TTY.
if !term.IsTerminal(int(os.Stdin.Fd())) {
	cmdArgs := []string{"agenc", "tmux", "switch"}
	cmdArgs = append(cmdArgs, args...)
	cmdStr := strings.Join(cmdArgs, " ")

	popupCmd := exec.Command("tmux", "display-popup", "-E", "-w", "90%", "-h", "63%", cmdStr)
	popupCmd.Stdout = os.Stdout
	popupCmd.Stderr = os.Stderr
	return popupCmd.Run()
}
```

Remove the now-unused imports `"golang.org/x/term"` and `"os"` (if no longer referenced).

**Step 2: Implement feedback.go change**

Replace the `tmux new-window -a` wrapper with a direct `agenc mission new` call. The server handles tmux window creation:

```go
RunE: func(cmd *cobra.Command, args []string) error {
	feedbackCmd := exec.Command("agenc", "mission", "new",
		"--adjutant",
		"--prompt", feedbackPrompt,
	)
	feedbackCmd.Stdout = os.Stdout
	feedbackCmd.Stderr = os.Stderr

	if err := feedbackCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to launch feedback mission")
	}
	return nil
},
```

Update the Long description to remove the `tmux new-window -a` reference.

**Step 3: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 4: Commit**

```
git add cmd/tmux_switch.go cmd/feedback.go
git commit -m "Remove tmux switch auto-popup and feedback.go tmux new-window wrapper"
```

---

Task 6: Add executionMode to CLI add/update commands and ls output
------------------------------------------------------------------

**Files:**
- Modify: `cmd/config_palette_command_add.go:47-55` (flags)
- Modify: `cmd/config_palette_command_add.go:96-101` (struct construction)
- Modify: `cmd/config_palette_command_update.go:36-42` (flags)
- Modify: `cmd/config_palette_command_update.go:44-108` (update logic)
- Modify: `cmd/config_palette_command_ls.go:35-86` (display)
- Modify: `cmd/config_palette_command.go` (help text example)

**Step 1: Implement**

Add `--execution-mode` flag to the `add` command:

```go
configPaletteCommandAddCmd.Flags().String(paletteCommandExecutionModeFlagName, "",
	"execution mode: run (default), popup, pane, or window")
```

Add the flag name constant wherever the other flag names are defined (likely in `cmd/config_palette_command.go` or a constants file — find the pattern):

```go
const paletteCommandExecutionModeFlagName = "execution-mode"
```

In `runConfigPaletteCommandAdd`, read and validate the flag:

```go
executionModeStr, _ := cmd.Flags().GetString(paletteCommandExecutionModeFlagName)
executionMode := config.ExecutionMode(executionModeStr)
if executionModeStr != "" && !executionMode.IsValid() {
	return stacktrace.NewError("invalid execution mode %q; must be one of: run, popup, pane, window", executionModeStr)
}
```

Include it in the config struct:

```go
cfg.PaletteCommands[name] = config.PaletteCommandConfig{
	Title:          title,
	Description:    description,
	Command:        command,
	TmuxKeybinding: keybinding,
	ExecutionMode:  executionMode,
}
```

Same pattern for the `update` command — add the flag, check `Changed()`, validate, apply.

In `ls` output, add an `EXEC MODE` column:

```go
tbl := tableprinter.NewTable("NAME", "TITLE", "KEYBINDING", "EXEC MODE", "COMMAND", "SOURCE")
```

And populate it with `cmd.ExecutionMode` (showing "run" for empty/default).

Update the help text in `config_palette_command.go` to include an `executionMode` example:

```yaml
# Custom command with interactive popup
myPicker:
  title: "🔍 My Picker"
  command: "my-interactive-picker"
  executionMode: popup
```

**Step 2: Run tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```
git add cmd/config_palette_command*.go
git commit -m "Add executionMode flag to palette command add/update CLI and ls output"
```

---

Task 7: Add config validation for executionMode
------------------------------------------------

**Files:**
- Modify: `internal/config/agenc_config.go` (validation in ReadAgencConfig or similar)
- Test: `internal/config/agenc_config_test.go`

**Step 1: Write the failing test**

```go
func TestInvalidExecutionModeInConfig(t *testing.T) {
	yaml := `
paletteCommands:
  myCmd:
    title: "test"
    command: "echo hello"
    executionMode: "bogus"
`
	// Write to temp dir, read config, expect validation error
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	os.MkdirAll(configDir, 0755)
	os.WriteFile(filepath.Join(configDir, "config.yml"), []byte(yaml), 0644)

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Error("expected validation error for invalid executionMode")
	}
	if !strings.Contains(err.Error(), "invalid execution mode") {
		t.Errorf("expected error about invalid execution mode, got: %v", err)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `cd internal/config && go test -run TestInvalidExecutionMode -v`
Expected: FAIL — no validation yet

**Step 3: Implement**

Find the config validation logic (search for where `ReadAgencConfig` validates other fields). Add a validation pass that checks all `PaletteCommands` entries for valid `ExecutionMode`:

```go
for name, cmd := range cfg.PaletteCommands {
	if !cmd.ExecutionMode.IsValid() {
		return nil, nil, stacktrace.NewError(
			"invalid execution mode %q for palette command '%s' in %s; must be one of: run, popup, pane, window",
			cmd.ExecutionMode, name, configFilepath)
	}
}
```

**Step 4: Run test**

Run: `cd internal/config && go test -run TestInvalidExecutionMode -v`
Expected: PASS

**Step 5: Run full suite**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 6: Commit**

```
git add internal/config/agenc_config.go internal/config/agenc_config_test.go
git commit -m "Add config validation for executionMode field"
```

---

Task 8: Update documentation
-----------------------------

**Files:**
- Modify: `docs/system-architecture.md` — update palette command section to describe execution modes
- Verify: `cmd/genprime/main.go` — check if the prime content references palette dispatch; update if needed

**Step 1: Implement**

Read `docs/system-architecture.md` and find the section on palette commands / keybindings. Update it to describe the execution mode model. Key points:
- Commands declare an `executionMode` (run, popup, pane, window)
- A shared `WrapCommand` function wraps commands for both keybinding generation and palette dispatch
- The palette hands off via `tmux run-shell -b` so keybinding and palette behavior are identical

**Step 2: Run full suite**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: PASS

**Step 3: Commit**

```
git add docs/system-architecture.md
git commit -m "Update architecture docs for execution mode model"
```

---

Task 9: Final integration verification
---------------------------------------

**Step 1: Build the binary**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Expected: Clean build

**Step 2: Verify generated keybindings**

Run: `./agenc tmux inject --dry-run` (or similar) to inspect the generated keybindings content. Verify:
- `newMission` keybinding contains `display-popup`
- `sideShell` keybinding contains `split-window` with mission workspace dir
- `shell` keybinding contains `new-window` with mission workspace dir
- `stopMission` keybinding is plain `run-shell`
- No command strings contain embedded `tmux new-window` or `tmux split-window`

**Step 3: Verify palette command list**

Run: `./agenc config paletteCommand ls`
Verify the EXEC MODE column shows correct modes for all builtins.

**Step 4: Commit and push**

```
git push
```
