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
2. `-N` was added in **tmux 3.2** — "Add -N flag to never start server even if
   command would normally do so", at line 1282 of tmux's `CHANGES`, under the
   header `CHANGES FROM 3.1c TO 3.2` at line 1238. AgenC's floor was tmux 3.0
   (`cmd/tmux_helpers.go`), so the floor moves to 3.2.

   A first pass of this document said 3.1, on the strength of a summarising
   fetch of the same file rather than the file itself. The independent verifier
   caught it, and the line numbers above are from `grep -n` on the primary
   source. A wrong floor here is not cosmetic: it would admit tmux 3.1, where
   every `-N` command fails as an unknown option, so AgenC would refuse
   everything while reporting "no tmux server is running" with a healthy server
   in front of the user.
3. Exactly **three** of the tmux subcommands AgenC invokes start a server:
   `new-session`, `start-server`, and `attach-session`. The other twenty exit
   non-zero without creating one.

   `attach-session` needs the right experiment to see, and the obvious one is
   wrong. Under tmux's default `exit-empty on`, the server it starts exits
   again before a check can observe it, so a sweep that looks afterwards
   reports it harmless — which is what a first pass of this work concluded.
   Re-run with `exit-empty off` the server persists, and its argv is the attach
   command itself. Found by the verifier; confirmed directly.
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

Raising the tmux floor from 3.0 to 3.2 buys that atomicity unconditionally
instead of leaving the weaker guarantee on the configurations nobody runs.
tmux 3.2 shipped in 2021, Homebrew ships 3.6a, and the repo already treats 3.2
as the floor for the command palette (`display-popup` is skipped below it), so
the practical surface is small. **It is still a user-facing requirement change
and is called out for review.**

Because that floor is only checked by one CLI command, `RequireRunningServer`
re-checks it on the daemon paths. Without that, a user on tmux 3.0 or 3.1 would
meet a permanent, confident "no tmux server is running" with a running server in
front of them, and a remedy that cannot work — the same shape of
correct-looking-but-false diagnosis that cost six weeks in the first place.

### Mechanical enforcement, matching the repo's own idiom

A repo-wide scan test (`internal/tmux/server_ownership_test.go`) parses every
Go file and fails if a string literal outside the helper names `new-session` or
`start-server`. This is the same enforcement pattern the repo already uses for
things that must not drift — `internal/config/protected_env_vars_test.go`,
`cmd/help_text_drift_test.go`, the ANSI function-name tests — and it runs under
`make check`, which the pre-commit hook enforces.

It parses rather than greps for a specific reason. Grepping for the quoted token
`"new-session"` catches the argument form and misses `"tmux new-session -d"`
embedded in a shell command string — and that is the form tmux keybinding and
palette commands are built in, which the architecture doc describes as
"self-contained strings that include their own tmux primitives". So the miss is a
plausible future change rather than a hypothetical one. Scanning the contents of
string literals catches both forms, while parsing is what keeps comments that
merely name the command — including the ones in `server_ownership.go` explaining
why it is forbidden — from tripping the test.

Scope is the three commands measured in finding 3 — `new-session`,
`start-server` and `attach-session`. All three are confined to the helper, and
`attach-session` gets its own named builder rather than a generic one precisely
so the scan can stay strict about where the command may be named.

It does not catch a command assembled from pieces (`"new-"+"session"`). That is
deliberate-evasion territory, and the mistake being defended against is a
future caller writing the command the ordinary way.

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
row, so a refusal leaves nothing half-created. Without that, the refusal would
have landed in `spawnWrapper`, whose failures are logged and discarded while the
handler still returns 201 — a mission with no pane and no explanation.

The site guard in `ensurePoolSession` stays regardless: that is where the
invariant actually lives, and the precondition check is an ergonomic early exit,
not the enforcement.

**This narrows that swallow rather than closing it**, and the distinction is
worth stating plainly because the first draft of this document overclaimed it.
If the tmux server dies in the window between the precondition and
`createPoolWindow`, the mission row exists, `spawnWrapper`'s error is logged and
dropped, and the caller still gets a 201. That swallow is pre-existing and
untouched here; making `spawnWrapper` failures surface to the caller is a larger
change than this issue, and is left as follow-up work rather than quietly
counted as done.

`handlePopStash` gets the same precondition, and there it is load-bearing rather
than ergonomic. A restore deletes the stash file when it finishes regardless of
how many missions came back, so a restore into a serverless world printed
"Restored 0 mission(s)", exited 0, and destroyed the stash — with the cause
visible only in the daemon log. That is both a regression this change would have
introduced and a trap in step 4 of the remediation recipe below. The residual
wart — the stash file being deleted even when nothing was restored, for reasons
other than a missing tmux server — is also pre-existing and also left alone.

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
   until it is killed, so the symptom stays with or without this change. The
   recipe, in this order:

   1. `agenc stash push` — snapshots every running mission and stops it.
   2. From a shell **outside** tmux, `tmux kill-server`. Run from inside a pane
      it kills the shell issuing it, and it closes every terminal window
      attached to that server either way.
   3. Open a terminal window and run `tmux`. This is the step that puts the
      terminal, not AgenC, in the server's responsible-process slot.
   4. `agenc stash pop` — restores the missions into the new server.

   Step 4 depends on step 3: with this change, a restore into a world with no
   tmux server refuses rather than quietly creating one.
2. **Guarantee a terminal-owned server after reboot.** This belongs in dotfiles,
   not in AgenC: a Ghostty startup command or shell init that ensures a server
   exists means autonomous crons find one waiting. AgenC's job is to refuse
   loudly; making sure the refusal is rare is the environment's job.
