Session-Agnostic Tmux Integration
==================================

Status: Approved
Date: 2026-02-18


Overview
--------

Remove the requirement to be inside a dedicated `agenc` tmux session. AgenC tmux
features (`agenc tmux window new`, palette, pane new, switch, window renaming,
pane registration) should work from any tmux session the user is already in.

The `agenc tmux attach` command is preserved for users who prefer the dedicated
session workflow, but it is no longer required.


Problem
-------

All AgenC tmux features are gated on the `AGENC_TMUX=1` environment variable,
which is only set on windows inside the dedicated `agenc` tmux session. Users
cannot run `agenc tmux window new` or use the command palette from their own
existing tmux sessions (e.g. a project session, a personal workspace).


Solution
--------

Replace `AGENC_TMUX` with the standard `$TMUX` environment variable as the
"are we in tmux?" signal. `$TMUX` is set automatically by tmux on every session
and every window — no explicit setup required.

Remove `AGENC_TMUX` entirely from the codebase. No backward-compatibility shim
is needed because no external scripts depend on it.


Detailed Changes
----------------

### cmd/tmux_helpers.go

- Remove the `agencTmuxEnvVar = "AGENC_TMUX"` constant.
- Rename `isInsideAgencTmux()` → `isInsideTmux()`.
- Change its implementation to `return os.Getenv("TMUX") != ""`.

### cmd/tmux_attach.go

- Change the nested-attach guard (currently `AGENC_TMUX == "1"`) to check
  `os.Getenv("TMUX") != ""`.
- In `createTmuxSession()`, remove `AGENC_TMUX=1` from the inline env var string
  prepended to the initial command.
- Remove the `setTmuxSessionEnv(agencTmuxEnvVar, "1")` call.
- Update `printInsideSessionError()` message to refer to "a tmux session" rather
  than "the agenc session".
- Update the command docstring to remove `AGENC_TMUX=1` mention.

### cmd/tmux_palette.go, cmd/tmux_window_new.go, cmd/tmux_pane_new.go, cmd/tmux_switch.go

- Call `isInsideTmux()` instead of `isInsideAgencTmux()`.
- Update error messages: "must be run inside a tmux session" (remove `AGENC_TMUX`
  reference and `agenc session` reference).

### internal/wrapper/tmux.go

- Remove the `agencTmuxEnvVar = "AGENC_TMUX"` constant.
- In `renameWindowForTmux()`: change guard from `os.Getenv(agencTmuxEnvVar) != "1"`
  to `os.Getenv("TMUX") == ""`.
- In `updateWindowTitleFromSession()`: same guard change.
- Update comments referencing `AGENC_TMUX == 1`.
- Note: `resolveWindowID()` already checks `os.Getenv("TMUX") == ""` — no change.

### internal/claudeconfig/adjutant_claude.md

- Update Adjutant's instructions: check `$TMUX` (not `$AGENC_TMUX`) to determine
  whether it is inside a tmux session and can launch missions.

### docs/system-architecture.md

- Remove `AGENC_TMUX=1` from the description of `wrapper/tmux.go`.

### specs/tmux-mission-windows.md

- Update all references to `$AGENC_TMUX` / `AGENC_TMUX=1` to reflect the new
  `$TMUX`-based detection.

### docs/cli/agenc_tmux_attach.md, docs/cli/agenc_attach.md

- Remove `AGENC_TMUX=1` from the command description.


Behavior After Change
---------------------

| Scenario | Before | After |
|---|---|---|
| Run `agenc tmux window new` from `agenc` session | Works | Works |
| Run `agenc tmux window new` from any other session | Error | Works |
| Run `agenc tmux window new` outside tmux | Error | Error (same) |
| Window renaming / pane registration | Only in `agenc` session | Any tmux session |
| `agenc tmux attach` from outside tmux | Creates+attaches `agenc` session | Same |
| `agenc tmux attach` from inside any tmux session | Blocked only if in `agenc` | Blocked in any session |


What Does Not Change
--------------------

- `agenc tmux attach` still creates and attaches the dedicated `agenc` session.
- `AGENC_DIRPATH` propagation is unaffected.
- `resolveWindowID()` was already session-agnostic.
- The tmux keybindings file (`tmux-keybindings.conf`) applies to the whole tmux
  server — no change needed.
- The `agenc` key table name in keybindings (`const agencKeyTable = "agenc"`) is
  a tmux key-table identifier, not a session name — no change needed.
