Changelog
=========

Breaking Changes
----------------

### AgenC no longer creates the tmux server, and tmux 3.2 is the minimum

*Shipped in v0.15.0.*

`tmux new-session` starts the tmux server when none is running, and the invoking process *becomes* that server. AgenC called it on startup, so whenever it won that race it took ownership of a resource outliving every AgenC process. On macOS that ownership is a privacy identity: the kernel pins the TCC "responsible process" for every pane in the server, including your own shells in sessions AgenC knows nothing about. The `agenc` binary is ad-hoc signed and cannot hold a lasting grant, so the symptom surfaces weeks later as `ls ~/Downloads` failing with `Operation not permitted` in your own terminal.

AgenC now refuses to create the server and explains why. Starting it is the terminal's job.

The minimum tmux version moves from 3.0 to 3.2, because the refusal uses tmux's `-N` flag ("never start the server even if the command would normally do so"), which landed in 3.2.

**Migration:**

With no tmux server running, `agenc` returns a refusal where it previously gave you a session. Run `tmux` in a terminal window first, then either run `agenc` from a second window that is not itself inside a tmux session, or use the AgenC command palette from inside tmux.

If you are below tmux 3.2, upgrade it (`brew upgrade tmux` on Homebrew).

Upgrading does **not** fix a machine whose tmux server AgenC already created: that server keeps the privacy identity it was born with for as long as it lives. To hand the slot back to your terminal:

1. `agenc stash push` — snapshots every running mission and stops it.
2. From a shell **outside** tmux, `tmux kill-server`. Run from inside a pane it kills the shell issuing it, and either way it closes every attached terminal window.
3. Open a terminal window and run `tmux`. This is the step that puts the terminal in the responsible-process slot.
4. `agenc stash pop`.

Nothing creates the server for you after a reboot, so until you open a terminal and run `tmux`, crons refuse and write a `tmux.no-server` notification. If you run crons unattended, consider a terminal startup command or a login item.

### Remove `agenc cron health`

*Shipped in v0.15.0.*

The `agenc cron health` command has been removed. Cron health monitoring now runs inside the agenc server, which compares each enabled cron's schedule against whether it has actually produced a mission and posts a notification naming every cron that has gone quiet.

**Migration:**

Remove any scripts or aliases invoking `agenc cron health`. There is no replacement command to call: a cron that stops firing is reported to you without being asked. "Quiet" means the cron has skipped two of its own cycles, so a daily cron is noticed within two days and a weekly one within two weeks.

### Rename openShell to sideShell

The builtin palette command "openShell" has been renamed to "sideShell" to better reflect its behavior of opening a shell in a side pane.

**Migration:**

If you have customized `paletteCommands.openShell` in your config.yml, rename the key to `sideShell`:

```yaml
# Before
paletteCommands:
  openShell:
    title: "My Custom Shell"

# After
paletteCommands:
  sideShell:
    title: "My Custom Shell"
```

The display title has also changed from "🐚  Open Shell" to "🐚  Side Shell". The keybinding (`ctrl-p`) remains unchanged.
