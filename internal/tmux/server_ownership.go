package tmux

import (
	"os/exec"
	"sync"

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
// Added in tmux 3.2 ("Add -N flag to never start server even if command would
// normally do so", CHANGES FROM 3.1c TO 3.2), which is why
// cmd/tmux_helpers.go requires that version.
const noStartServerFlag = "-N"

const (
	noStartServerFlagMajorVersion = 3
	noStartServerFlagMinorVersion = 2
)

// noTmuxServerErrorMessage is shown to the user when AgenC refuses to create
// the server. It has to carry the cause, because the refusal otherwise looks
// like a bug, and the remedy, because the fix is not guessable.
//
// Two things about the remedy wording, both of which were wrong in a draft:
//
//   - `tmux start-server` is NOT the suggested command. exit-empty defaults to
//     on, so a server with no sessions exits immediately and the next AgenC
//     command fails all over again. `tmux` leaves a session, and so a server,
//     standing.
//   - It does not promise that re-running the same command will now work,
//     because for `agenc attach` that is false: having run `tmux`, the user is
//     now inside tmux, and attach declines from there. Each caller adds its own
//     next step; this message stops at the thing that is true everywhere.
const noTmuxServerErrorMessage = `no tmux server is running, and AgenC will not start one.

AgenC refuses to create the tmux server on purpose. On macOS, whichever process
creates the server becomes the privacy identity (the TCC "responsible process")
for every pane inside it — including your own shells — for as long as that
server lives. If AgenC created it, your own terminal work would be attributed to
the agenc binary, which cannot hold a lasting permission grant, and commands
like 'ls ~/Downloads' would start failing with "Operation not permitted" in your
own terminal weeks later for no visible reason.

Starting the server is your terminal's job. Open a terminal window and run
'tmux'; AgenC works normally once a tmux server exists.`

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

// RequireRunningServer returns nil when AgenC may go ahead and use the tmux
// server, and otherwise the error to show the user.
//
// It checks the tmux version before checking for the server, and the order is
// the whole point. On a tmux without the no-start flag, every command carrying
// it fails as an unknown option, so IsServerRunning answers false no matter how
// healthy the server is — and the refusal would then name a cause that is
// false and a remedy that cannot work. Diagnosing a correct-looking error with
// the wrong cause in it is what cost six weeks the first time, so a version
// problem has to say it is a version problem.
func RequireRunningServer() error {
	if err := checkNoStartServerFlagSupported(); err != nil {
		return err
	}
	if !IsServerRunning() {
		return stacktrace.NewError("%s", noTmuxServerErrorMessage)
	}
	return nil
}

var (
	noStartServerFlagSupportOnce sync.Once
	noStartServerFlagSupportErr  error
)

// checkNoStartServerFlagSupported reports whether the installed tmux
// understands the no-start flag. The answer cannot change inside one process,
// so it is resolved once rather than paying a `tmux -V` on every guarded call.
func checkNoStartServerFlagSupported() error {
	noStartServerFlagSupportOnce.Do(func() {
		major, minor, err := DetectVersion()
		if err != nil {
			noStartServerFlagSupportErr = stacktrace.Propagate(err, "cannot determine the installed tmux version, which AgenC needs in order to use tmux safely")
			return
		}
		isSupported := major > noStartServerFlagMajorVersion ||
			(major == noStartServerFlagMajorVersion && minor >= noStartServerFlagMinorVersion)
		if !isSupported {
			noStartServerFlagSupportErr = stacktrace.NewError(
				"tmux %d.%d is installed, but AgenC requires tmux >= %d.%d: it uses tmux's '%s' flag to guarantee it never creates the tmux server, and that flag was added in tmux %d.%d. Upgrade tmux.",
				major, minor,
				noStartServerFlagMajorVersion, noStartServerFlagMinorVersion,
				noStartServerFlag,
				noStartServerFlagMajorVersion, noStartServerFlagMinorVersion,
			)
		}
	})
	return noStartServerFlagSupportErr
}

// NewSessionCommand builds a `tmux new-session` command with the given
// arguments, refusing with an explanatory error when no tmux server is running.
//
// Every `tmux new-session` in AgenC goes through here. Callers pass only the
// arguments after "new-session" (e.g. "-d", "-s", name).
func NewSessionCommand(newSessionArgs ...string) (*exec.Cmd, error) {
	if err := RequireRunningServer(); err != nil {
		return nil, err
	}
	return noStartServerCommand(append([]string{"new-session"}, newSessionArgs...)...), nil
}

// AttachSessionCommand builds a `tmux attach-session` command that cannot
// start a server.
//
// attach-session belongs in this file, which is easy to miss: it is not
// obviously server-creating, and with tmux's default `exit-empty on` the
// server it starts exits again immediately, so the behaviour reads as
// harmless. It is not. Under `exit-empty off` that server stays, owned by
// whichever process invoked attach — AgenC, in the one place AgenC attaches.
// Measured in an isolated socket directory: with no server running,
// `tmux attach-session -t =nosuch` creates one whose argv is the attach
// command itself.
//
// Unlike NewSessionCommand this does not refuse up front: its caller reaches
// it having already established that the session exists, so the flag is a
// backstop against the server dying in between rather than the gate.
func AttachSessionCommand(attachSessionArgs ...string) *exec.Cmd {
	return noStartServerCommand(append([]string{"attach-session"}, attachSessionArgs...)...)
}

// noStartServerCommand builds a tmux command carrying the no-start flag. It is
// unexported so that naming a server-creating tmux command stays confined to
// this file, which is what the repository scan in the test enforces.
func noStartServerCommand(tmuxArgs ...string) *exec.Cmd {
	argsWithFlag := make([]string, 0, len(tmuxArgs)+1)
	argsWithFlag = append(argsWithFlag, noStartServerFlag)
	argsWithFlag = append(argsWithFlag, tmuxArgs...)
	return exec.Command("tmux", argsWithFlag...)
}
