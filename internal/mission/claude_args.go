package mission

import (
	"strings"
)

// Claude CLI flag names AgenC names anywhere. These are external tokens owned
// by the Claude CLI, so they are bound to named symbols rather than spelled
// inline: an upstream rename becomes a one-line change here instead of a silent
// runtime miss scattered across the spawn path.
//
// AgenC deliberately names no model or effort flag here. Those reach Claude as
// ordinary argv — from the claudeArgs config chain or from the mission's own
// list — so AgenC has no notion of what a model is.
const (
	// settingsClaudeFlag is the flag AgenC delivers its own operational overlay
	// through, which is why a caller passing one too is worth a warning.
	settingsClaudeFlag = "--settings"

	// bareClaudeFlag turns off the hooks AgenC tracks mission state with.
	bareClaudeFlag = "--bare"

	// endOfClaudeFlagsSeparator ends Claude's option parsing, so that what
	// follows is read as a positional argument whatever precedes it.
	endOfClaudeFlagsSeparator = "--"
)

// ClaudeArgSources holds every source that can contribute Claude CLI args to a
// mission spawn. It is a struct rather than a positional parameter list because
// both fields are string slices: transposing them would compile cleanly and
// fail only at runtime, inside a spawn, as a silently inverted precedence.
type ClaudeArgSources struct {
	// ConfigClaudeArgs comes from the claudeArgs config chain (global, then
	// repo). It is how a default model, or any other default flag, is set.
	ConfigClaudeArgs []string

	// ClaudePassthroughArgs is the mission's own ordered argv, given to
	// `agenc mission new` after the `--` separator and stored on the mission.
	ClaudePassthroughArgs []string
}

// MergeClaudeArgs assembles the Claude CLI args for a mission spawn: the config
// chain first, then the mission's own list.
//
// That ordering is the whole precedence rule. AgenC dedupes nothing, because
// the args are opaque argv — AgenC does not know which of them are flags, which
// take values, or whether a flag may repeat, so it cannot drop one without
// guessing. A later occurrence of a scalar flag wins because Claude resolves a
// repeated scalar flag last-wins (verified for --model, 2026-11-19), so a
// mission's `--model sonnet` outranks a config `--model opus`, and a repo's
// claudeArgs outrank the global ones.
func MergeClaudeArgs(sources ClaudeArgSources) []string {
	var merged []string
	merged = append(merged, sources.ConfigClaudeArgs...)
	merged = append(merged, sources.ClaudePassthroughArgs...)
	return merged
}

// splitClaudeArg splits a CLI arg into its flag name and whether a value was
// supplied inline: "--settings=x" yields ("--settings", true), "--settings"
// yields ("--settings", false).
func splitClaudeArg(arg string) (string, bool) {
	flagName, _, hasInlineValue := strings.Cut(arg, "=")
	return flagName, hasInlineValue
}
