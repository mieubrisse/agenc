package mission

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

// missionEnvAssignmentSeparator divides a KEY=VALUE assignment as a caller
// writes it on the command line and as the process environment carries it.
const missionEnvAssignmentSeparator = "="

// posixEnvNameRegexp matches the environment-variable names a POSIX shell can
// expand. Validating against it is not pedantry: the only consumer of this
// channel is a Claude Code hook reading "$NAME", and a name the shell cannot
// expand would be delivered to the process and then be unreadable — accepted
// at creation and silently useless at use. Rejecting it at creation turns that
// into an error the caller sees.
var posixEnvNameRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseMissionEnvAssignments turns the KEY=VALUE assignments a caller wrote on
// `agenc mission new --env` into the map stored on the mission. Returns nil
// when there are none, so "this mission sets no environment variables" has one
// representation whether or not the caller typed the flag.
//
// The split is on the FIRST separator, so a value may itself contain one —
// which a JSON value, a URL or a base64 payload routinely does.
//
// A repeated key is last-wins rather than an error. The process environment
// has one value per name, so there is no shape in which both could be
// delivered, and Go's exec resolves a duplicated name in cmd.Env the same way;
// making the resolution AgenC's own keeps it testable rather than inherited.
func ParseMissionEnvAssignments(assignments []string) (map[string]string, error) {
	if len(assignments) == 0 {
		return nil, nil
	}

	missionEnv := map[string]string{}
	for _, assignment := range assignments {
		name, value, found := strings.Cut(assignment, missionEnvAssignmentSeparator)
		if !found {
			return nil, stacktrace.NewError(
				"environment assignment '%v' is not in NAME%vVALUE form",
				assignment,
				missionEnvAssignmentSeparator,
			)
		}
		missionEnv[name] = value
	}

	if err := ValidateMissionEnv(missionEnv); err != nil {
		return nil, stacktrace.Propagate(err, "failed to validate the environment assignments '%v'", assignments)
	}
	return missionEnv, nil
}

// ValidateMissionEnv checks that every name in a mission's environment map is
// one a POSIX shell can expand and every value is one the process environment
// can carry. The CLI's own parsing already rejects a malformed assignment, so
// this is the guard for the server's HTTP API, which any client can reach.
func ValidateMissionEnv(missionEnv map[string]string) error {
	for _, name := range sortedMissionEnvNames(missionEnv) {
		if !posixEnvNameRegexp.MatchString(name) {
			return stacktrace.NewError(
				"environment variable name '%v' is not a valid shell variable name "+
					"(it must start with a letter or underscore and contain only letters, digits and underscores)",
				name,
			)
		}
		// A name AgenC sets itself would be overridden at spawn time, because
		// the spawn path re-applies AgenC's own values on top of the mission's.
		// Refusing it here is the courtesy half of that: the caller is told
		// now, rather than finding out from a value that silently did not take
		// effect. The guarantee is the ordering, not this check — a client that
		// somehow stored one anyway still loses to AgenC at spawn.
		if config.IsProtectedEnvVar(name) {
			return stacktrace.NewError(
				"environment variable '%v' is one AgenC sets itself, so a value given here would be overridden at spawn time and cannot take effect; "+
					"the names AgenC owns are %v",
				name,
				strings.Join(config.ProtectedEnvVars, ", "),
			)
		}
		// A NUL byte cannot be put in a C string, so the exec syscall rejects
		// the whole environment rather than truncating the one value. Caught
		// here because the alternative is a mission that is created
		// successfully and then cannot spawn Claude at all. Unreachable from
		// the CLI, where argv cannot carry a NUL, but the HTTP API's JSON can.
		if strings.ContainsRune(missionEnv[name], 0) {
			return stacktrace.NewError("environment variable '%v' has a value containing a NUL byte, which a process environment cannot carry", name)
		}
	}
	return nil
}

// MissionEnvToAssignments renders a mission's environment map as the
// NAME=VALUE strings exec.Cmd.Env takes, sorted by name.
//
// Sorted because the map's iteration order is random and this output lands on
// a real process: an unsorted environment makes two spawns of the same mission
// differ for no reason, which is noise in a `ps` listing and nondeterminism in
// a test.
func MissionEnvToAssignments(missionEnv map[string]string) []string {
	names := sortedMissionEnvNames(missionEnv)
	assignments := make([]string, 0, len(names))
	for _, name := range names {
		assignments = append(assignments, name+missionEnvAssignmentSeparator+missionEnv[name])
	}
	return assignments
}

// FormatMissionEnv renders the mission environment that will actually take
// effect, as a single display line for `mission inspect`. Returns an empty
// string when the mission sets none that will.
//
// Names AgenC owns are excluded, because they will NOT take effect: the spawn
// path drops them and applies AgenC's own. Showing one here would report a
// value as this mission's environment when the process will never see it, and
// the question `mission inspect` answers is what the mission's environment IS.
// IgnoredMissionEnvNames surfaces them separately so nothing is hidden —
// normally neither is reachable, since validation refuses these names at
// creation, but a row written by an older client or edited by hand can carry
// one and that is exactly when a reader needs the truth.
//
// Any assignment that is not a plain token is rendered with Go quoting, which
// escapes newlines rather than emitting them, for the reason
// FormatClaudePassthroughArgs gives: the line sits in an aligned block that
// callers read line by line, and a raw newline in a value would split it in two.
func FormatMissionEnv(missionEnv map[string]string) string {
	assignments := MissionEnvToAssignments(effectiveMissionEnv(missionEnv))
	rendered := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		if isPlainClaudeArgToken(assignment) {
			rendered = append(rendered, assignment)
			continue
		}
		rendered = append(rendered, strconv.Quote(assignment))
	}
	return strings.Join(rendered, " ")
}

// sortedMissionEnvNames returns the names in a mission's environment map in a
// stable order.
func sortedMissionEnvNames(missionEnv map[string]string) []string {
	names := make([]string, 0, len(missionEnv))
	for name := range missionEnv {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IgnoredMissionEnvNames returns the names in a mission's stored environment
// that AgenC owns, and which the spawn path therefore drops. Sorted, and empty
// in the normal case because validation refuses these names at creation.
//
// It exists so `mission inspect` can say a stored value is inert rather than
// either hiding it or implying it applies. A non-empty result means the row was
// written by something other than today's validation — an older client, or a
// hand edit — which is worth seeing on its own.
func IgnoredMissionEnvNames(missionEnv map[string]string) []string {
	ignored := make([]string, 0, len(missionEnv))
	for _, name := range sortedMissionEnvNames(missionEnv) {
		if config.IsProtectedEnvVar(name) {
			ignored = append(ignored, name)
		}
	}
	return ignored
}

// effectiveMissionEnv returns the mission's environment minus the names AgenC
// owns, which is what the spawned process will actually carry from it.
func effectiveMissionEnv(missionEnv map[string]string) map[string]string {
	effective := make(map[string]string, len(missionEnv))
	for name, value := range missionEnv {
		if config.IsProtectedEnvVar(name) {
			continue
		}
		effective[name] = value
	}
	return effective
}
