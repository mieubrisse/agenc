Remove ExecutionMode / WrapCommand
====================================

Goal
----

Remove the `executionMode` field, `WrapCommand` function, and all related
abstractions. Commands become self-contained strings that include their own tmux
primitives (display-popup, split-window, new-window) when needed. Both
keybindings and the palette dispatch the same command string via `run-shell`.

What stays
----------

- `tmux run-shell -b` as the palette's dispatch mechanism (the popup closes,
  tmux executes the command asynchronously)
- `$AGENC_CALLING_MISSION_UUID` env var rendering for palette dispatch (export
  prefix before the command so the tmux server's shell has the vars)
- `IsMissionScoped()` detection via command string content (unchanged)
- Old-tmux gating: skip commands containing `display-popup` when tmux < 3.2

What goes
---------

- `ExecutionMode` type, constants, `IsValid()` method
- `ExecutionMode` field on `PaletteCommandConfig` and `ResolvedPaletteCommand`
- `WrapCommand` function
- `--execution-mode` CLI flag on add/update commands
- `EXEC MODE` column in ls output
- `executionMode` validation in `ReadAgencConfig`
- `paletteCommandExecutionModeFlagName` constant
- Mode-based dispatch logic in keybinding generation

Palette output logging
----------------------

`tmux run-shell` captures command stdout and displays it in the active pane.
For fire-and-forget commands (like `agenc mission new --blank`), this produces
distracting output. For popup/pane/window commands, the tmux primitive creates
its own TTY so there's nothing to capture.

Approach: always redirect stdout/stderr to a log file for palette-dispatched
commands:

    tmux run-shell -b '<command> >> $AGENC_DIRPATH/logs/palette.log 2>&1'

This suppresses pane output, preserves logs for debugging, and is harmless for
popup/pane/window commands (their tmux primitives produce no stdout).

The palette process knows `$AGENC_DIRPATH` and can resolve it to an absolute
path before constructing the run-shell command. No server involvement needed —
it's just file redirection.

---

Task 1: Restore builtin commands and remove ExecutionMode type
--------------------------------------------------------------

**Files:**
- Modify: `internal/config/agenc_config.go`
- Modify: `internal/config/agenc_config_test.go`

Restore all builtin command strings to their pre-refactoring self-contained
form. Remove the `ExecutionMode` type, constants, `IsValid()` method, and the
field from both `PaletteCommandConfig` and `ResolvedPaletteCommand`. Remove the
`executionMode` validation block in `ReadAgencConfig`.

Builtin commands to restore (original form):

| Name | Original command |
|------|-----------------|
| quickClaude | `agenc mission new --blank` (unchanged) |
| talkToAgenc | `agenc mission new --adjutant` (unchanged) |
| newMission | `tmux display-popup -E -w 68% -h 63% "agenc mission new"` |
| switchMission | `tmux display-popup -E -w 68% -h 63% "agenc tmux switch"` |
| resumeMission | `tmux display-popup -E -w 68% -h 63% "agenc mission resume"` |
| sideShell | `tmux split-window -h -c "${AGENC_DIRPATH:-$HOME/.agenc}/missions/$AGENC_CALLING_MISSION_UUID/agent" $SHELL` |
| shell | `tmux new-window -a -c "${AGENC_DIRPATH:-$HOME/.agenc}/missions/$AGENC_CALLING_MISSION_UUID/agent" $SHELL` |
| copyMissionUuid | `printf '%s' $AGENC_CALLING_MISSION_UUID \| pbcopy` (unchanged) |
| renameSession | `tmux display-popup -E -w 68% -h 63% "agenc mission rename $AGENC_CALLING_MISSION_UUID"` |
| stopMission | `agenc mission stop $AGENC_CALLING_MISSION_UUID` (unchanged) |
| reconfigMission | `agenc mission reconfig $AGENC_CALLING_MISSION_UUID && agenc mission reload $AGENC_CALLING_MISSION_UUID` (unchanged) |
| reloadMission | `agenc mission reload $AGENC_CALLING_MISSION_UUID` (unchanged) |
| removeMission | `tmux display-popup -E -w 68% -h 63% "agenc mission rm"` |
| nukeMissions | `tmux display-popup -E -w 68% -h 63% "agenc mission nuke"` |
| sendFeedback | `agenc mission new --adjutant --prompt "I'd like to send feedback about AgenC"` (unchanged) |
| joinDiscord | `agenc discord` (unchanged) |
| starAgenc | `agenc star` (unchanged) |
| exitTmux | `tmux detach` (unchanged) |

Remove tests: `TestExecutionModeValidation`, `TestPaletteCommandConfigIsEmpty_IgnoresExecutionMode`,
`TestBuiltinExecutionModes`, `TestInvalidExecutionModeInConfig`.

Update test: `TestBuiltinCommandsNoEmbeddedTmuxPrimitives` — delete it, since
builtins now intentionally embed tmux primitives.

---

Task 2: Remove WrapCommand and simplify keybinding generation
-------------------------------------------------------------

**Files:**
- Modify: `internal/tmux/keybindings.go`
- Modify: `internal/tmux/keybindings_test.go`

Delete the `WrapCommand` function entirely.

Remove `ExecutionMode` from `CustomKeybinding` struct (it had the old
`DisplayPopup bool` before our refactoring — now it needs neither).

Rewrite the keybinding generation loop. Commands are self-contained, so the
generator just wraps them in `run-shell`. Two cases:

1. **Mission-scoped** (command contains `$AGENC_CALLING_MISSION_UUID`):
   preamble resolves UUID, guard skips if empty, then runs the command.
2. **Non-mission-scoped**: just `bind-key ... run-shell '<command>'`.

Old-tmux handling: skip any command containing `display-popup` when
tmux < 3.2. These commands require a popup and there's no useful fallback.

```
for _, kb := range customKeybindings {
    // Skip popup commands on old tmux
    if strings.Contains(kb.Command, "display-popup") &&
       !(tmuxMajor > 3 || (tmuxMajor == 3 && tmuxMinor >= 2)) {
        continue
    }

    escapedCommand := escapeSingleQuotes(kb.Command)

    if kb.IsMissionScoped {
        // Preamble resolves UUID, guard skips if empty
        fmt.Fprintf(&sb, "bind-key %s run-shell '"+
            "AGENC_CALLING_MISSION_UUID=$(%s tmux resolve-mission \"#{pane_id}\"); "+
            "[ -n \"$AGENC_CALLING_MISSION_UUID\" ] && %s"+
            "'\n", bindKeyArgs, agencBinary, escapedCommand)
    } else {
        fmt.Fprintf(&sb, "bind-key %s run-shell '%s'\n",
            bindKeyArgs, escapedCommand)
    }
}
```

Update `BuildKeybindingsFromCommands` to stop copying `ExecutionMode`.

Delete all `WrapCommand` tests. Update keybinding generation tests to use
self-contained command strings.

---

Task 3: Update palette dispatch with env var rendering and output logging
-------------------------------------------------------------------------

**Files:**
- Modify: `cmd/tmux_palette.go`

Replace the current dispatch block (which calls `WrapCommand` + env exports)
with a simpler approach:

1. The command is already self-contained — no wrapping needed.
2. For mission-scoped commands, prepend `export` statements so the tmux
   server's shell has `$AGENC_CALLING_MISSION_UUID` and `$AGENC_DIRPATH`.
3. Redirect stdout/stderr to a log file to suppress run-shell pane output.
4. Dispatch via `tmux run-shell -b`.

```go
fullCommand := selectedEntry.Command
if selectedEntry.IsMissionScoped() && callingMissionUUID != "" {
    envPrefix := fmt.Sprintf("export AGENC_CALLING_MISSION_UUID=%s; ", callingMissionUUID)
    agencDirpath, err := config.GetAgencDirpath()
    if err == nil {
        envPrefix += fmt.Sprintf("export AGENC_DIRPATH=%s; ", agencDirpath)
    }
    fullCommand = envPrefix + fullCommand
}

// Resolve the log file path for output capture
agencDirpath, _ := config.GetAgencDirpath()
logFilepath := filepath.Join(agencDirpath, "logs", "palette.log")
fullCommand += fmt.Sprintf(" >> %s 2>&1", logFilepath)

runShellCmd := exec.Command("tmux", "run-shell", "-b", fullCommand)
if err := runShellCmd.Run(); err != nil {
    return stacktrace.Propagate(err, "failed to dispatch palette command")
}
```

Remove the `internal/tmux` import (no longer calling `tmux.WrapCommand`).
Add `"path/filepath"` import.

Also ensure the logs directory exists (it should already from the server, but
`os.MkdirAll` is cheap insurance).

---

Task 4: Remove executionMode from CLI commands
-----------------------------------------------

**Files:**
- Modify: `cmd/command_str_consts.go`
- Modify: `cmd/config_palette_command_add.go`
- Modify: `cmd/config_palette_command_update.go`
- Modify: `cmd/config_palette_command_ls.go`
- Modify: `cmd/config_palette_command.go`

Delete `paletteCommandExecutionModeFlagName` constant.

In `config_palette_command_add.go`: remove the `--execution-mode` flag
registration, flag reading, validation, and the `ExecutionMode` field from the
config struct literal. Remove the popup example from the Long description.

In `config_palette_command_update.go`: remove the `--execution-mode` flag
registration, `executionModeChanged` check, the mode update block, and the flag
name from the "at least one flag" error message.

In `config_palette_command_ls.go`: remove `ExecMode` from `displayEntry` struct,
remove the ExecMode population logic, remove `"EXEC MODE"` from the table header,
remove `entry.ExecMode` from `AddRow`.

In `config_palette_command.go`: remove the `executionMode: popup` example from
the Long description.

---

Task 5: Update documentation and generated content
---------------------------------------------------

**Files:**
- Modify: `docs/system-architecture.md`
- Modify: `cmd/genprime/main.go`

In `system-architecture.md`: update the `internal/tmux/` section to remove
references to `WrapCommand` and `ExecutionMode`. Describe the simplified model:
commands are self-contained strings, keybindings and palette both dispatch via
`run-shell`, mission-scoped commands get a UUID preamble (keybindings) or export
prefix (palette).

Update the mission pane tracking section to reflect the palette's export-prefix
approach and log file redirect.

In `genprime/main.go`: remove the `executionMode` mention from the palette
commands description.
