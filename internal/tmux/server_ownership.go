package tmux

import (
	"os/exec"

	"github.com/mieubrisse/stacktrace"
)

// AgenC must never create the tmux server. This file is the only place in the
// codebase that builds a server-creating tmux command, and a repository-wide
// source scan in server_ownership_test.go keeps it that way.
//
// WHY THIS MATTERS, because the cost is invisible at the moment of the mistake:
//
// `tmux new-session` starts the tmux server when none is running, and the
// invoking client process *becomes* that server — it keeps its own argv and
// reparents to pid 1. So AgenC calling it does not merely create a session: it
// takes ownership of a shared, long-lived OS resource that outlives every
// AgenC process.
//
// On macOS that ownership is a privacy identity. The kernel fixes the TCC
// "responsible process" for every pane inside the server to whatever created
// the server, for the life of the server. If AgenC created it, the user's own
// interactive shells — opened from their terminal emulator, in sessions AgenC
// knows nothing about — are attributed to the agenc binary. Two consequences
// follow:
//
//  1. AgenC is ad-hoc/linker-signed as "a.out", so macOS cannot remember a
//     permission grant for it across a rebuild. Every prompt recurs forever,
//     and some grants never stick at all: `ls ~/Downloads` fails with
//     "Operation not permitted" in the user's own terminal.
//  2. The right owner is the terminal emulator. A Developer-ID-signed terminal
//     has a stable designated requirement, so grants to it survive app
//     updates. Neither AgenC nor Homebrew's ad-hoc-signed tmux can hold one.
//
// The failure announces itself nowhere. The server persists across weeks of
// uptime, nothing errors when the wrong owner is taken, and the breakage shows
// up later in unrelated work under another process's name. The case that
// produced this file took six weeks to notice and presented as two seemingly
// unrelated mysteries.
//
// Hence: refuse, and say so. A loud refusal the user can act on is strictly
// better than a silent misattribution they cannot see. Starting the server is
// the terminal's job.
//
// Giving AgenC its own tmux socket is NOT an alternative fix: AgenC surfaces
// mission windows inside the user's own sessions with `tmux link-window`, and
// tmux cannot link windows across servers. Sharing one server is load-bearing.
//
// Background: KEV-168 (this file) and KEV-44 (AgenC's signature).
// Architectural summary in docs/system-architecture.md; full design rationale
// in docs/superpowers/specs/2026-10-28-tmux-server-ownership-design.md.

// noStartServerFlag makes tmux refuse to start the server even for a command
// that would normally do so. It is passed on every server-creating call so the
// refusal is enforced by tmux itself rather than only by our own check — the
// check alone leaves a window in which the last session closes between the
// check and the call, and silently acquiring a weeks-long misattribution is
// precisely the failure this guard exists to prevent.
//
// Added in tmux 3.1, which is why cmd/tmux_helpers.go requires that version.
const noStartServerFlag = "-N"

// noTmuxServerErrorMessage is shown to the user when AgenC refuses to create
// the server. It has to carry the cause, because the refusal otherwise looks
// like a bug, and the remedy, because the fix is not guessable.
//
// `tmux start-server` is deliberately NOT the suggested remedy: exit-empty
// defaults to on, so a server with no sessions exits immediately and the next
// AgenC command fails all over again.
const noTmuxServerErrorMessage = `no tmux server is running, and AgenC will not start one.

AgenC refuses to create the tmux server on purpose. On macOS, whichever process
creates the server becomes the privacy identity (the TCC "responsible process")
for every pane inside it — including your own shells — for as long as that
server lives. If AgenC created it, your own terminal work would be attributed to
the agenc binary, which cannot hold a lasting permission grant, and commands
like 'ls ~/Downloads' would start failing with "Operation not permitted" in your
own terminal weeks later for no visible reason.

Starting the server is your terminal's job. Run 'tmux' in a terminal window,
then re-run this command.`

// IsServerRunning reports whether a tmux server is currently running.
//
// `start-server` under the no-start flag is the probe because it asks exactly
// that question and nothing else: it exits 0 when a server is already there, it
// exits non-zero when there is none, it prints nothing, and the flag is what
// stops it doing the one thing its name suggests. Verified in both states.
//
// Two probes that look equivalent and are not: checking for the socket file is
// wrong because a socket outlives the server that made it, and counting
// sessions conflates "no server" with a server configured to stay alive while
// empty ('exit-empty off').
func IsServerRunning() bool {
	return exec.Command("tmux", noStartServerFlag, "start-server").Run() == nil
}

// NewSessionCommand builds a `tmux new-session` command with the given
// arguments, refusing with an explanatory error when no tmux server is running.
//
// Every `tmux new-session` in AgenC goes through here. Callers pass only the
// arguments after "new-session" (e.g. "-d", "-s", name).
func NewSessionCommand(newSessionArgs ...string) (*exec.Cmd, error) {
	if !IsServerRunning() {
		return nil, stacktrace.NewError("%s", noTmuxServerErrorMessage)
	}

	tmuxArgs := make([]string, 0, len(newSessionArgs)+2)
	tmuxArgs = append(tmuxArgs, noStartServerFlag, "new-session")
	tmuxArgs = append(tmuxArgs, newSessionArgs...)
	return exec.Command("tmux", tmuxArgs...), nil
}

// NoServerErrorMessage returns the user-facing explanation for a refusal, for
// callers that need to put it somewhere other than an error return — the
// notification a cron-triggered mission writes, for instance.
func NoServerErrorMessage() string {
	return noTmuxServerErrorMessage
}
