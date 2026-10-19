package config

// OAuthTokenEnvVar carries the machine OAuth token into a mission's Claude
// process. Not an AGENC_-prefixed name because it is Claude Code's own, but
// AgenC is what sets it, which is what puts it in the protected set below.
const OAuthTokenEnvVar = "CLAUDE_CODE_OAUTH_TOKEN"

// ProtectedEnvVars is the canonical list of environment variables AgenC sets
// into a mission's Claude process itself. It is the single place this set is
// written down, and two things read it:
//
//   - The spawn path re-applies these AFTER a mission's own `--env` variables,
//     so AgenC's value wins whatever a caller asked for. That ordering is the
//     actual guarantee.
//   - `--env` validation refuses a name in this list, so a caller who names one
//     gets told at creation rather than discovering their value was overridden
//     at spawn. That is a courtesy, not the guarantee.
//
// ADDING AN ENVIRONMENT VARIABLE: if AgenC sets it into a mission's Claude
// process, add it here too. `TestProtectedEnvVarsCoversEveryAgencEnvVar` scans
// this package for AGENC_-prefixed names and fails if one is missing, so the
// list cannot silently fall behind — which is the whole reason it is a list in
// one place rather than a check scattered across the call sites.
//
// Deliberately NOT included: CLAUDE_CONFIG_DIR. AgenC does not set it (State Y
// — Claude reads the user's real ~/.claude, or whatever the surrounding
// environment already set), it only forwards an existing value into the pool
// window. AgenC's operational overlay travels by absolute path through
// --settings, so a mission pointing Claude at a different config dir does not
// cost AgenC its instrumentation.
var ProtectedEnvVars = []string{
	MissionUUIDEnvVar,
	MissionSourceEnvVar,
	MissionSourceMetadataEnvVar,
	agencDirpathEnvVar,
	TestEnvVar,
	CallingMissionUUIDEnvVar,
	CallingSessionNameEnvVar,
	OAuthTokenEnvVar,
}

// protectedEnvVarSet is ProtectedEnvVars keyed for lookup, built once.
var protectedEnvVarSet = buildProtectedEnvVarSet()

func buildProtectedEnvVarSet() map[string]bool {
	set := make(map[string]bool, len(ProtectedEnvVars))
	for _, name := range ProtectedEnvVars {
		set[name] = true
	}
	return set
}

// IsProtectedEnvVar reports whether AgenC sets this environment variable
// itself, and therefore whether a caller naming it would be overridden.
func IsProtectedEnvVar(name string) bool {
	return protectedEnvVarSet[name]
}
