package mission

import (
	"strconv"
	"strings"
)

// claudeFlagsBreakingAgencInvariants maps a passthrough flag that disables part
// of AgenC itself to the plain-language consequence of passing it.
//
// Nothing here is rejected. `agenc mission new` passes Claude flags through
// verbatim by design — the caller is the machine's own user, who can run
// `claude` with any flag directly, so AgenC refusing one would withhold no
// capability. What AgenC can do is refuse to let the consequence be silent:
// both of these flags leave the mission running and apparently healthy while
// the hooks AgenC tracks its state with are gone, which is a failure that
// emits no signal of its own.
var claudeFlagsBreakingAgencInvariants = map[string]string{
	settingsClaudeFlag: "Claude takes only the last --settings, so this one replaces AgenC's operational overlay: " +
		"mission state tracking, the prime hook, the repo-library guard and the server socket allowlist all stop working for this mission",
	bareClaudeFlag: "--bare skips hooks entirely, so this mission will not report its state back to AgenC",
}

// ClaudePassthroughArgsWarnings returns a warning for each passthrough arg that
// disables part of AgenC, in the order the args were given. Returns nil when
// there is nothing to warn about, which is the common case.
//
// A flag is matched on its name alone, so both "--settings x" and
// "--settings=x" are caught.
func ClaudePassthroughArgsWarnings(claudePassthroughArgs []string) []string {
	var warnings []string
	for _, arg := range claudePassthroughArgs {
		flagName, _ := splitClaudeArg(arg)
		consequence, found := claudeFlagsBreakingAgencInvariants[flagName]
		if !found {
			continue
		}
		warnings = append(warnings, flagName+": "+consequence)
	}
	return warnings
}

// FormatClaudePassthroughArgs renders passthrough args as a single display line
// for `mission inspect` and mission-creation output. Returns an empty string
// when there are none.
//
// Any arg that is not a plain token is rendered with Go quoting, which escapes
// newlines rather than emitting them. A passthrough value is routinely a whole
// system prompt — multi-line, quote-bearing — and letting that reach stdout raw
// would break the aligned output it sits in and corrupt any caller parsing it.
// This is a display rendering, not a shell-safe quoting: nothing reconstructs
// argv from it, because the args reach Claude from the mission record as a
// slice and never pass through a shell.
func FormatClaudePassthroughArgs(claudePassthroughArgs []string) string {
	rendered := make([]string, 0, len(claudePassthroughArgs))
	for _, arg := range claudePassthroughArgs {
		if isPlainClaudeArgToken(arg) {
			rendered = append(rendered, arg)
			continue
		}
		rendered = append(rendered, strconv.Quote(arg))
	}
	return strings.Join(rendered, " ")
}

// isPlainClaudeArgToken reports whether an arg can be displayed as-is: no
// whitespace to make two tokens look like one, no quote to confuse where it
// ends, and not empty.
func isPlainClaudeArgToken(arg string) bool {
	if arg == "" {
		return false
	}
	return !strings.ContainsAny(arg, " \t\r\n\"'")
}
