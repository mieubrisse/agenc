# AgenC must not create the tmux server

Design for [KEV-168](https://linear.app/todayendeavors/issue/KEV-168).
Diagnosed in AgenC mission `7b0788d4-23bd-4c61-a60a-e63f1332896f`;
implemented in AgenC mission `33bc2e64-46c9-4ba7-9abf-1f76db129db5`.

## Intent and the why

`tmux new-session` starts the tmux **server** when none is running, and the
invoking client process *becomes* that server — keeping its argv. So when AgenC
wins that race it does not merely create a session: it becomes the owner of a
shared, long-lived OS resource.

On macOS that ownership is a privacy identity. The kernel fixes the TCC
"responsible process" for every pane inside the server to whatever created it,
for the life of the server. When AgenC created it, the user's own shells are
attributed to the `agenc` binary — which, being ad-hoc/linker-signed as `a.out`
(see [KEV-44](https://linear.app/todayendeavors/issue/KEV-44)), can never hold a
durable grant. The user's own `ls ~/Downloads` then fails with
`Operation not permitted` in their own terminal.

The failure is invisible by construction. The server persists across weeks of
uptime, nothing errors at the moment of the mistake, and the breakage surfaces
later, somewhere unrelated, under another process's name. On Kevin's machine it
took six weeks to notice and presented as two seemingly unrelated mysteries.

**So the win condition is not "pick a better owner" — it is that this class of
failure announces itself at the moment it happens.**

## Goals

- AgenC never creates the tmux server, on any code path.
- When no server is running, an AgenC command that needs one fails loudly with
  an error naming both the cause and the remedy.
- A cron-fired mission that cannot start surfaces that failure where Kevin
  actually looks, not only in a launchd log file.
- Normal operation is unchanged when a server already exists.
- A future change cannot silently reintroduce server creation.

## Non-goals and anti-goals

- **Not** giving AgenC its own tmux socket. `tmux link-window`
  (`internal/server/pool.go`) cannot link windows across servers, and
  missions-as-windows is load-bearing.
- **Not** making AgenC or tmux the durable owner. Both are ad-hoc signed; a
  grant to either dies at the next rebuild or upgrade. The right owner is the
  terminal emulator, which is Developer ID signed with a stable designated
  requirement.
- **Not** fixing AgenC's own signature. That is KEV-44, a sibling.
- **Not** remediating the already-wrong server on Kevin's machine from code.
  That needs a one-time manual recipe (below).

## Empirical findings this design rests on

Verified 2026-10-28 on tmux 3.6a, in an isolated tmux world
(`env -u TMUX TMUX_TMPDIR=<short empty dir>`), without touching the real server:

1. `tmux -N <command>` refuses to start a server: with no server running,
   `tmux -N new-session -d -s probe` exits 1 with "no server running on <socket>"
   and **creates no socket**. With a server running, `-N` is inert — the session
   is created normally, exit 0. Documented in `man tmux`: "Do not start the
   server even if the command would normally do so (for example new-session or
   start-server)."
2. `-N` was added in **tmux 3.1** ("Add -N flag to never start server even if
   command would normally do so", CHANGES FROM 3.0a TO 3.1). AgenC's floor is
   currently tmux 3.0 (`cmd/tmux_helpers.go`), so the floor moves to 3.1.
3. `tmux has-session` and `tmux list-sessions` do not create a server either, so
   either is a safe probe.
4. `tmux start-server` does **not** leave a server standing: `exit-empty` defaults
   to `on`, so a server with no sessions exits immediately. The remedy text must
   therefore say `tmux`, not `tmux start-server`.
5. A socket *file* can outlive its server, so "the socket exists" is not a test
   for "a server is running".
6. The TCC attribution itself cannot be read back without root
   (`launchctl procinfo` prints nothing as a normal user), so the privacy flip is
   not assertable in automated tests. See "What is not verifiable here".

## The shape of the fix

### One chokepoint, not three guards

`internal/tmux/server_ownership.go` becomes the only place in the codebase that
constructs a `tmux new-session` command. It carries the full rationale as its
package-level comment — the next reader of this code lands there.

Two layers, because they do different jobs:

- **A probe** (`IsServerRunning`) produces the *good error*: cause plus remedy.
  `-N`'s own message ("no server running on /tmp/tmux-501/default") names
  neither.
- **The `-N` flag** makes the refusal *atomic*. A probe alone leaves a
  time-of-check/time-of-use window in which the last session closes between the
  check and the call. The window is microscopic, but a microscopic chance of
  silently acquiring a weeks-long misattribution is exactly the failure class
  this issue is about, and `-N` closes it in the kernel rather than in our
  reasoning.

Raising the tmux floor from 3.0 to 3.1 buys that atomicity unconditionally
instead of leaving the weaker guarantee on the configurations nobody runs.
tmux 3.1 shipped in April 2020. **This is a user-facing requirement change and
is called out for review.**

### Mechanical enforcement, matching the repo's own idiom

A repo-wide source scan test (`internal/tmux/server_ownership_test.go`) fails if
any `.go` file outside the helper writes `"new-session"` or `"start-server"` as a
string literal. This is the same enforcement pattern the repo already uses for
things that must not drift — `internal/config/protected_env_vars_test.go`,
`cmd/help_text_drift_test.go`, the ANSI function-name tests — and it runs under
`make check`, which the pre-commit hook enforces. Prose comments do not match,
because the pattern includes the quote characters.

This is the answer to "a future change must not silently reintroduce it".
A comment asking people to be careful is not.

### Fail immediately; do not wait for a server

KEV-168 left this open. Failing wins:

- A waiting cron is a *silent* stall — the exact failure mode the issue exists to
  eliminate. A failed cron with a notification is a signal.
- Waiting cannot succeed unattended anyway: the thing being waited for is a human
  opening a terminal. After a reboot with no terminal open, waiting just converts
  a loud failure into a hung process holding a mission slot.
- Failing is reversible and cheap to retry; the user reruns the command.

### Precondition check before anything is created

`handleCreateMission` checks for a running server before it writes the mission
row, so a refusal leaves nothing half-created. Today a `spawnWrapper` failure is
swallowed — the mission row is created and the error only logged — which would
have turned this refusal into a mission with no pane and no explanation.

The site guard in `ensurePoolSession` stays regardless: that is where the
invariant actually lives, and the precondition check is an ergonomic early exit,
not the enforcement.

### Cron visibility

When mission creation is refused for this reason and the request came from a
cron, a `tmux.no-server` notification is written. It deliberately ignores the
cron's `notificationsEnabled` setting: that flag governs the routine
"cron triggered" FYI, and a run that *could not start* is a different class. The
whole point of the notification is that the failure is visible.

### Daemon startup stays a warning

`internal/server/server.go` calls `ensurePoolSession` when the daemon comes up
and logs a warning on failure. That stays. The daemon should still start — it
serves `mission ls`, `notification ls` and the rest — and with the guard in place
the call can no longer create a server. The pool session gets created later, by
`createPoolWindow`, once a terminal-owned server exists.

### The three call sites

All three route through the helper:

- `internal/server/pool.go` — the pool session (the path that caused this).
- `internal/server/handle_stash.go` — sessions pre-created for stash restore.
- `cmd/tmux_attach.go` — the `agenc` entry session.

`cmd/tmux_attach.go` is included on Kevin's explicit instruction. It costs
ergonomics: bare `agenc` from a fresh terminal with no tmux server now refuses
instead of starting one, so the user runs `tmux` first. That is the correct
trade: AgenC cannot tell from inside its own process whether its invocation chain
roots at a Developer-ID-signed terminal or at the detached daemon, and guessing
wrong is what produced a six-week invisible failure.

## What is left to the builder

- Exact wording of the error, subject to: generic (this is a public repo —
  no "Ghostty" in user-facing text), names the cause in two or three lines,
  names the remedy as a command the user can run.
- Whether `IsServerRunning` probes with `has-session` or `list-sessions`.

## Recommendation back to Kevin (environment, not code)

Two things this PR deliberately does not do:

1. **Remediate the live machine.** The misattributed server (pid 78205,
   `tmux new-session -d -s agenc-pool`, created 2026-09-15 00:30:02) persists
   until it is killed. Recipe: stash running missions (`agenc mission stash`),
   `tmux kill-server`, start tmux from the terminal emulator, restore the stash.
   Until that happens the symptom stays, with or without this change.
2. **Guarantee a terminal-owned server after reboot.** This belongs in dotfiles,
   not in AgenC: a Ghostty startup command or shell init that ensures a server
   exists means autonomous crons find one waiting. AgenC's job is to refuse
   loudly; making sure the refusal is rare is the environment's job.
