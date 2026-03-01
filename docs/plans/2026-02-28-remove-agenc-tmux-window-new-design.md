Remove `agenc tmux window new` and `AGENC_WINDOW_NAME`
=====================================================

Problem
-------

`agenc tmux window new` is a thin wrapper around `tmux new-window` that adds
almost no value. Its only unique behavior is injecting the `AGENC_WINDOW_NAME`
env var to prevent the wrapper's dynamic title system from overwriting an
explicitly-set window name. But `/rename` already provides title pinning, making
the env var redundant.

Additionally, the `shell` and `sideShell` palette commands use
`#{pane_current_path}` to set the working directory of new panes/windows. This
tmux format variable is unreliable when commands are executed from inside a
`display-popup` (the palette's execution context). The fix is to derive the
agent directory deterministically from `$AGENC_CALLING_MISSION_UUID`.

Design
------

### Palette command changes

Both `shell` and `sideShell` switch to UUID-based path construction and vanilla
tmux commands. This makes them mission-scoped (hidden when no mission is focused,
keybindings get the `resolve-mission` guard).

| Command | Before | After |
|---------|--------|-------|
| `shell` | `agenc tmux window new -a -- $SHELL` | `tmux new-window -a -c "${AGENC_DIRPATH:-$HOME/.agenc}/missions/$AGENC_CALLING_MISSION_UUID/agent" $SHELL` |
| `sideShell` | `tmux split-window -h -c '#{pane_current_path}' $SHELL` | `tmux split-window -h -c "${AGENC_DIRPATH:-$HOME/.agenc}/missions/$AGENC_CALLING_MISSION_UUID/agent" $SHELL` |
| `removeMission` | `agenc tmux window new -a -- agenc mission rm` | `tmux new-window -a agenc mission rm` |
| `nukeMissions` | `agenc tmux window new -a -- agenc mission nuke` | `tmux new-window -a agenc mission nuke` |

Behavioral change: `shell` and `sideShell` disappear from the palette and their
keybindings become no-ops when not in a mission pane. This is intentional —
opening a random shell is not an AgenC concern.

### `agenc feedback` command

Currently shells out to `agenc tmux window new -a -- agenc mission new --adjutant --prompt "..."`.
Change to use vanilla tmux directly: `tmux new-window -a agenc mission new --adjutant --prompt "..."`.

### `AGENC_WINDOW_NAME` removal

Remove all references from `internal/wrapper/tmux.go`:

- `renameWindowForTmux()`: remove the env var check (lines 54-57). Priority
  becomes: `windowTitle` from config > repo short name > mission ID.
- `updateWindowTitleFromSession()`: remove the env var early-return (lines
  200-204). Priority becomes: `/rename` custom title > AI summary > session name.
- Update doc comments describing the priority order.

### File deletions

- `cmd/tmux_window_new.go` — the entire command implementation
- `docs/cli/agenc_tmux_window_new.md` — CLI reference doc

### File moves

- `buildShellCommand()` — currently defined in `cmd/tmux_window_new.go` but also
  used by `cmd/tmux_pane_new.go`. Move to `cmd/tmux_pane_new.go` (or a shared
  file like `cmd/tmux_helpers.go`).

### `cmd/tmux_window.go` (parent command group)

After removing `tmux_window_new.go`, the `agenc tmux window` parent command has
no subcommands. Delete `cmd/tmux_window.go` as well.

### Documentation and test updates

- `README.md` — remove `agenc tmux window new` references
- `docs/configuration.md` — update palette command examples
- `docs/cli/agenc_tmux_window.md` — delete (no more subcommands)
- `docs/system-architecture.md` — remove `AGENC_WINDOW_NAME` from wrapper
  description
- `internal/config/agenc_config_test.go` — update test expectations for changed
  default commands
- `internal/claudeconfig/adjutant_claude.md` — remove guidance about not using
  `agenc tmux window new`
- `cmd/feedback.go` — update long description text
- `docs/cli/agenc_feedback.md` — update example
- `cmd/tmux_pane_new.go` — remove cross-reference comment to `tmux_window_new.go`
- Archived specs (`specs/ARCHIVE/`) — leave as-is (historical)

### What is NOT changing

- `agenc tmux pane new` — unaffected, still exists
- The wrapper's dynamic title system — still works, just loses the
  `AGENC_WINDOW_NAME` priority level
- `/rename` — still the primary way to pin a window title
- `resumeMission` palette command — already uses vanilla tmux
