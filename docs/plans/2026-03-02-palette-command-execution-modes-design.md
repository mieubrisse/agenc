Palette Command Execution Modes
================================

Problem
-------

Every palette command needs to run in a specific tmux execution context (fire-and-forget
via `run-shell`, interactive popup, split pane, new window), but this context is expressed
ad-hoc across three different places:

1. **The command string itself** — some commands embed `tmux new-window -a` or
   `tmux split-window -h` directly in their command (e.g., `resumeMission`,
   `sideShell`, `removeMission`)
2. **A hardcoded builtin map** — `builtinDisplayPopupCommands` flags three commands
   for popup dispatch, but only for keybindings
3. **The keybinding generator** — `GenerateKeybindingsContent` decides wrapping based
   on `DisplayPopup` and `IsMissionScoped`, but the palette ignores this entirely

This produces two problems:

- **Keybindings and palette diverge.** A keybinding for a popup command wraps it in
  `tmux display-popup`. The palette runs the same command via `sh -c` in its own popup.
  Commands like `tmux switch` auto-detect whether they have a TTY to self-wrap, creating
  a third dispatch path.
- **"What to run" is tangled with "how to run."** Commands like `resumeMission` say
  `tmux new-window -a agenc mission resume` — the execution context is glued into the
  command string. Custom commands have no way to declare they need a popup or pane.

Design
------

### Execution modes

Add an `executionMode` field to palette commands. This is the single source of truth for
how a command runs, replacing the embedded tmux primitives and the `DisplayPopup` flag.

| Mode | Behavior | Example commands |
|------|----------|-----------------|
| `run` | Fire-and-forget via `run-shell`. No TTY. Default if omitted. | stopMission, copyMissionUuid, exitTmux, joinDiscord, starAgenc, quickClaude, talkToAgenc, sendFeedback, reconfigMission, reloadMission |
| `popup` | Opens a `tmux display-popup -E` with TTY for interactive input. | newMission, switchMission, renameSession, resumeMission, removeMission, nukeMissions |
| `pane` | Opens a `tmux split-window -h` in the current window. | sideShell |
| `window` | Opens a `tmux new-window -a` in the current session. | shell |

### Command strings become pure "what to run"

Commands no longer embed tmux dispatch primitives. The execution mode controls the
wrapping. Before and after:

| Command | Before | After |
|---------|--------|-------|
| `resumeMission` | `tmux new-window -a agenc mission resume` | `agenc mission resume` |
| `removeMission` | `tmux new-window -a agenc mission rm` | `agenc mission rm` |
| `nukeMissions` | `tmux new-window -a agenc mission nuke` | `agenc mission nuke` |
| `sideShell` | `tmux split-window -h -c "..." $SHELL` | `$SHELL` (working dir from mode) |
| `shell` | `tmux new-window -a -c "..." $SHELL` | `$SHELL` (working dir from mode) |

For `pane` and `window` modes, the working directory is derived from the mission
context when `$AGENC_CALLING_MISSION_UUID` is present — the wrapping function computes
`-c "$AGENC_DIRPATH/missions/$UUID/agent"`. This replaces the hardcoded path template
currently embedded in `sideShell` and `shell` command strings.

### Shared wrapping function

A single Go function computes the fully-wrapped shell command for any execution mode:

```go
func WrapCommand(command string, mode ExecutionMode, missionScoped bool) string
```

This function is called from two sites:

1. **Keybinding generator** — at keybinding-file generation time, to produce the
   `bind-key ... run-shell '...'` entries (as today, but using the mode instead of
   ad-hoc `DisplayPopup` logic)
2. **Palette** — at fzf selection time, to produce the command string that gets
   handed off to the tmux server

Both paths produce the identical wrapped command for a given input.

### Palette dispatch via `tmux run-shell -b`

When the user selects a command from the palette, the palette:

1. Looks up the selected command's execution mode
2. Calls `WrapCommand` to produce the fully-wrapped command
3. Calls `tmux run-shell -b '<wrapped-command>'` to hand it off to the tmux server
4. Exits (closing the palette popup)

The tmux server then executes the wrapped command asynchronously. For popup-mode
commands, this means the palette popup closes first, then a new popup opens — clean
sequential behavior with no nesting.

This eliminates the current divergence where the palette runs commands via `sh -c`
inline. All commands — regardless of whether they were triggered by keybinding or
palette — go through the same wrapping and execution path.

### Mission-scoped commands

Mission UUID resolution is orthogonal to execution mode. The current approach is
preserved: `IsMissionScoped()` checks whether the command string references
`$AGENC_CALLING_MISSION_UUID`. The keybinding preamble resolves the UUID via
`agenc tmux resolve-mission`, and the palette passes it through the environment.

The `WrapCommand` function does not handle mission scoping — that remains in the
keybinding generator's preamble logic and the palette's environment setup. This keeps
the wrapping function focused on a single concern.

### Config.yml exposure

The `executionMode` field is exposed in `config.yml` for custom commands:

```yaml
paletteCommands:
  myTool:
    title: "🔧  My Tool"
    description: "Run my interactive tool"
    command: "my-interactive-tool"
    executionMode: popup
```

For builtins, the execution mode is set in the `BuiltinPaletteCommands` map and is
overridable via config (same merge semantics as other fields). If omitted, defaults
to `run`.

### What gets removed

- `builtinDisplayPopupCommands` map — replaced by `executionMode` on each command
- `DisplayPopup` field on `PaletteCommandConfig` and `ResolvedPaletteCommand`
- `DisplayPopup` field on `CustomKeybinding`
- The `tmux switch` auto-popup TTY detection (lines 41-49 of `tmux_switch.go`) — the
  caller now guarantees a popup via execution mode
- All `tmux new-window -a` and `tmux split-window -h` prefixes from command strings

### Keybinding generation changes

`GenerateKeybindingsContent` currently has a 2x2 matrix of mission-scoped × popup.
This becomes a 2×4 matrix of mission-scoped × execution mode. The structure is the same
but `usePopup` is replaced by a switch on `ExecutionMode`:

- `run`: `run-shell '<command>'`
- `popup`: `run-shell 'tmux display-popup -E "<command>"'`
- `pane`: `run-shell 'tmux split-window -h <command>'`
- `window`: `run-shell 'tmux new-window -a <command>'`

With the mission-scoped preamble prepended when `IsMissionScoped` is true, same as
today.

### Builtin command reclassification

Current commands with their corrected execution modes:

```go
"quickClaude":     {Command: "agenc mission new --blank",     ExecutionMode: ExecRun}
"talkToAgenc":     {Command: "agenc mission new --adjutant",  ExecutionMode: ExecRun}
"newMission":      {Command: "agenc mission new",             ExecutionMode: ExecPopup}
"switchMission":   {Command: "agenc tmux switch",             ExecutionMode: ExecPopup}
"resumeMission":   {Command: "agenc mission resume",          ExecutionMode: ExecPopup}
"sideShell":       {Command: "$SHELL",                        ExecutionMode: ExecPane}
"shell":           {Command: "$SHELL",                        ExecutionMode: ExecWindow}
"copyMissionUuid": {Command: "printf '%s' ...",               ExecutionMode: ExecRun}
"renameSession":   {Command: "agenc mission rename ...",      ExecutionMode: ExecPopup}
"stopMission":     {Command: "agenc mission stop ...",        ExecutionMode: ExecRun}
"reconfigMission": {Command: "agenc mission reconfig ...",    ExecutionMode: ExecRun}
"reloadMission":   {Command: "agenc mission reload ...",      ExecutionMode: ExecRun}
"removeMission":   {Command: "agenc mission rm",              ExecutionMode: ExecPopup}
"nukeMissions":    {Command: "agenc mission nuke",            ExecutionMode: ExecPopup}
"sendFeedback":    {Command: "agenc mission new --adjutant --prompt \"...\"",
                                                              ExecutionMode: ExecRun}
"joinDiscord":     {Command: "agenc discord",                 ExecutionMode: ExecRun}
"starAgenc":       {Command: "agenc star",                    ExecutionMode: ExecRun}
"exitTmux":        {Command: "tmux detach",                   ExecutionMode: ExecRun}
```

Key changes from current state:
- `quickClaude`, `talkToAgenc`, `sendFeedback` — change from `tmux new-window -a ...`
  to plain `agenc mission new ...` with `ExecRun`. The server handles tmux window
  creation and focusing.
- `resumeMission` — changes from `tmux new-window -a agenc mission resume` to
  `agenc mission resume` with `ExecPopup` (needs fzf picker).
- `removeMission`, `nukeMissions` — change from `tmux new-window -a ...` to
  `ExecPopup` (needs fzf picker).
- `sideShell`, `shell` — command becomes `$SHELL`, execution context moves to mode.

Files changed
-------------

- `internal/config/agenc_config.go` — Add `ExecutionMode` type and field to
  `PaletteCommandConfig`, `ResolvedPaletteCommand`. Remove `DisplayPopup` field
  and `builtinDisplayPopupCommands` map. Update all builtin command definitions.
- `internal/tmux/keybindings.go` — Replace `DisplayPopup bool` on `CustomKeybinding`
  with `ExecutionMode`. Rewrite `GenerateKeybindingsContent` dispatch logic to switch
  on mode. Add `WrapCommand` function. Add working-directory logic for pane/window modes.
- `cmd/tmux_palette.go` — Replace inline `sh -c` dispatch with `WrapCommand` +
  `tmux run-shell -b` handoff.
- `cmd/tmux_switch.go` — Remove the auto-popup TTY detection (lines 41-49). The
  caller now guarantees the appropriate execution context.
