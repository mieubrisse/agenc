package mission

import (
	"sort"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// Claude CLI flag names AgenC names anywhere. These are external tokens owned
// by the Claude CLI, so they are bound to named symbols rather than spelled
// inline: an upstream rename becomes a one-line change here instead of a silent
// runtime miss scattered across the spawn path.
const (
	modelClaudeFlag  = "--model"
	effortClaudeFlag = "--effort"

	// settingsClaudeFlag is the flag AgenC delivers its own operational overlay
	// through, which is why a caller passing one too is worth a warning.
	settingsClaudeFlag = "--settings"

	// bareClaudeFlag turns off the hooks AgenC tracks mission state with.
	bareClaudeFlag = "--bare"
)

// ForwardedClaudeFlag describes one Claude CLI flag that AgenC forwards from
// `agenc mission new` through to the Claude process the mission spawns.
//
// Key is how the flag is stored in the mission's claude_args JSON object and
// how it is named as an `agenc mission new` flag; Flag is the Claude CLI
// spelling emitted at spawn time.
//
// Every forwarded flag takes a value. Valueless (boolean) Claude flags are not
// supported here — stripConflictingArgs assumes a bare forwarded flag's value
// occupies the following argv slot. Adding one would need that assumption
// revisited, not just a new table entry.
type ForwardedClaudeFlag struct {
	Key   string
	Flag  string
	Usage string
}

// ForwardedClaudeFlags are the Claude CLI flags `agenc mission new` promotes to
// first-class AgenC flags, so that `--help` lists them, a typo is caught at
// parse time, and the value can be deduplicated against the config chain. It is
// a convenience surface, not a permission boundary: any Claude flag at all
// reaches a mission through the passthrough channel in
// claude_passthrough_args.go, so a flag's absence here constrains nothing.
//
// Promoting a new flag is a single entry here. Per-mission values are stored as
// a JSON object keyed by Key, so no database migration is involved.
//
// Order matters: it is the order flags are emitted onto Claude's command line,
// which keeps spawn commands stable and diffable across runs.
var ForwardedClaudeFlags = []ForwardedClaudeFlag{
	{
		Key:   "model",
		Flag:  modelClaudeFlag,
		Usage: `Claude model for this mission (e.g. "opus", "sonnet", "claude-opus-4-6"); overrides the defaultModel config`,
	},
	{
		Key:   "effort",
		Flag:  effortClaudeFlag,
		Usage: `Claude reasoning effort for this mission (low, medium, high, xhigh, max)`,
	},
}

// lookupForwardedClaudeFlag finds the forwarded flag registered under a key.
func lookupForwardedClaudeFlag(key string) (ForwardedClaudeFlag, bool) {
	for _, forwardedFlag := range ForwardedClaudeFlags {
		if forwardedFlag.Key == key {
			return forwardedFlag, true
		}
	}
	return ForwardedClaudeFlag{}, false
}

// forwardedClaudeFlagKeys returns every supported key, for error messages.
func forwardedClaudeFlagKeys() []string {
	keys := make([]string, 0, len(ForwardedClaudeFlags))
	for _, forwardedFlag := range ForwardedClaudeFlags {
		keys = append(keys, forwardedFlag.Key)
	}
	return keys
}

// ValidateMissionClaudeArgs checks that every key in the keyed per-mission arg
// map names a promoted flag and carries a value. The CLI's own flag parsing
// already rejects an unknown AgenC flag, so this is the guard for the server's
// HTTP API, which any client can reach. It says nothing about the passthrough
// channel, which is verbatim argv and has no keys to validate.
func ValidateMissionClaudeArgs(missionClaudeArgs map[string]string) error {
	for key, value := range missionClaudeArgs {
		if _, found := lookupForwardedClaudeFlag(key); !found {
			return stacktrace.NewError(
				"unsupported Claude arg '%v'; supported Claude args are: %v",
				key,
				strings.Join(forwardedClaudeFlagKeys(), ", "),
			)
		}
		if value == "" {
			return stacktrace.NewError("Claude arg '%v' needs a value, but was given an empty one", key)
		}
	}
	return nil
}

// UnknownClaudeArgKeys returns the sorted keys that name no supported flag.
// Per-mission args are validated on the way in, so a stored mission should
// never carry one — but a mission created by a newer AgenC and then read by an
// older one can, and that is worth surfacing rather than silently dropping.
func UnknownClaudeArgKeys(missionClaudeArgs map[string]string) []string {
	var unknown []string
	for key := range missionClaudeArgs {
		if _, found := lookupForwardedClaudeFlag(key); !found {
			unknown = append(unknown, key)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// MissionClaudeArgsToArgv renders per-mission Claude args as CLI argv, in
// ForwardedClaudeFlags order so the result does not depend on Go's randomized
// map iteration. Unknown keys are dropped rather than emitted — passing an
// unrecognized flag to Claude would fail the spawn outright, which is a worse
// outcome than running the mission without it.
func MissionClaudeArgsToArgv(missionClaudeArgs map[string]string) []string {
	var argv []string
	for _, forwardedFlag := range ForwardedClaudeFlags {
		value, found := missionClaudeArgs[forwardedFlag.Key]
		if !found || value == "" {
			continue
		}
		argv = append(argv, forwardedFlag.Flag, value)
	}
	return argv
}

// FormatMissionClaudeArgs renders per-mission Claude args for display to the
// user (mission inspect, mission creation output). Returns an empty string when
// there are none.
func FormatMissionClaudeArgs(missionClaudeArgs map[string]string) string {
	return strings.Join(MissionClaudeArgsToArgv(missionClaudeArgs), " ")
}

// ClaudeArgSources holds every source that can contribute Claude CLI args to a
// mission spawn. It is a struct rather than a positional parameter list because
// three of its four fields are string slices or maps of strings: transposing two
// of them would compile cleanly and fail only at runtime, inside a spawn.
type ClaudeArgSources struct {
	// ResolvedModel comes from the defaultModel config chain (repo, then global).
	ResolvedModel string

	// ConfigClaudeArgs comes from the claudeArgs config chain (global, then repo).
	ConfigClaudeArgs []string

	// MissionClaudeArgs are the keyed per-mission overrides for the flags
	// ForwardedClaudeFlags promotes, set at `agenc mission new` time.
	MissionClaudeArgs map[string]string

	// ClaudePassthroughArgs is the verbatim argv a caller handed to
	// `agenc mission new` after the `--` separator.
	ClaudePassthroughArgs []string
}

// MergeClaudeArgs assembles the Claude CLI args for a mission spawn, lowest
// precedence first: ResolvedModel, then ConfigClaudeArgs, then
// MissionClaudeArgs, then ClaudePassthroughArgs.
//
// A keyed per-mission override removes every occurrence of the same flag from
// the two lower-precedence sources, so each promoted flag reaches Claude exactly
// once. Claude's parser does resolve duplicate flags last-wins, but leaning on
// that is leaning on undocumented behaviour; emitting each flag once makes the
// precedence AgenC's own, and therefore testable.
//
// Passthrough args get no such treatment, deliberately. They are opaque argv —
// AgenC does not know which of them are flags, which take values, or whether a
// flag may be repeated, so it cannot dedupe them without guessing. They are
// appended last instead, which is what makes a passthrough flag outrank the
// same flag from config: for a scalar flag Claude takes the final occurrence.
// A caller who passes a flag the config chain also sets is relying on Claude's
// last-wins, not on AgenC.
func MergeClaudeArgs(sources ClaudeArgSources) []string {
	overriddenFlags := map[string]bool{}
	for key := range sources.MissionClaudeArgs {
		forwardedFlag, found := lookupForwardedClaudeFlag(key)
		if !found {
			continue
		}
		overriddenFlags[forwardedFlag.Flag] = true
	}

	var merged []string
	if sources.ResolvedModel != "" && !overriddenFlags[modelClaudeFlag] {
		merged = append(merged, modelClaudeFlag, sources.ResolvedModel)
	}
	merged = append(merged, stripConflictingArgs(sources.ConfigClaudeArgs, overriddenFlags)...)
	merged = append(merged, MissionClaudeArgsToArgv(sources.MissionClaudeArgs)...)
	merged = append(merged, sources.ClaudePassthroughArgs...)
	return merged
}

// stripConflictingArgs removes from args every occurrence of a flag in the
// overridden set, along with the value token that follows it. Only
// ForwardedClaudeFlags can ever populate the overridden set, and every one of
// those takes a value — so a bare "--flag" consumes the next token, while
// "--flag=value" is self-contained.
func stripConflictingArgs(args []string, overriddenFlags map[string]bool) []string {
	var kept []string
	for i := 0; i < len(args); i++ {
		flagName, hasInlineValue := splitClaudeArg(args[i])
		if !overriddenFlags[flagName] {
			kept = append(kept, args[i])
			continue
		}
		if !hasInlineValue {
			// Drop the separate value token that follows the bare flag. When
			// the flag is the final arg there is no value token to drop, and
			// the loop simply ends.
			i++
		}
	}
	return kept
}

// splitClaudeArg splits a CLI arg into its flag name and whether a value was
// supplied inline: "--model=opus" yields ("--model", true), "--model" yields
// ("--model", false).
func splitClaudeArg(arg string) (string, bool) {
	flagName, _, hasInlineValue := strings.Cut(arg, "=")
	return flagName, hasInlineValue
}
