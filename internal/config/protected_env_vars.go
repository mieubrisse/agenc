package config

// OAuthTokenEnvVar carries the machine OAuth token into a mission's Claude
// process. Not an AGENC_-prefixed name because it is Claude Code's own, but
// AgenC is what sets it, which is what puts it in the protected set below.
const OAuthTokenEnvVar = "CLAUDE_CODE_OAUTH_TOKEN"

// ProtectedEnvVars is the canonical list of environment variables AgenC sets
// into a mission's Claude process itself. It is the single place this set is
// written down, and two things read it:
//
//   - The spawn path DROPS these names from a mission's own `--env` variables
//     and then re-applies AgenC's own values on top. Both halves are needed.
//     Re-applying is what beats a value sitting in os.Environ(); dropping is
//     what handles a variable whose correct state is UNSET, which no appended
//     value can express. Together they are the actual guarantee, and they hold
//     however a value reached the mission record.
//   - `--env` validation refuses a name in this list, so a caller who names one
//     gets told at creation rather than discovering their value never took
//     effect. That is a courtesy, not the guarantee.
//
// MEMBERSHIP IS BY SET-SITE *OR* READ-SITE, not by "AgenC appends it". That
// distinction is load-bearing and was measured, not assumed. The three
// AGENC_CALLING_* names are never appended to a mission's Claude environment at
// all — the tmux palette sets them into a palette command's shell — yet they
// carry the worst consequence in this list, because the CLI READS them to decide
// which mission or session a command acts on. `mission rename` with no argument
// renames whatever AGENC_CALLING_MISSION_UUID names (cmd/mission_rename.go), and
// detach and stop resolve their target through AGENC_CALLING_SESSION_NAME
// (getCallingSessionName in cmd/mission_helpers.go). A mission able to set those
// could reach sideways into a PEER mission's lifecycle, including stopping it.
// A registry built only from append-sites would have omitted exactly those three.
//
// WHICH MECHANISM PROTECTS WHICH NAME differs, because AgenC does not set all of
// these on every install. Measured on the real production install: a mission's
// Claude has only AGENC_MISSION_SOURCE and AGENC_MISSION_UUID.
//
//   - Override case — AgenC has a value, so the re-application wins it back:
//     AGENC_MISSION_UUID (from the mission ID), AGENC_MISSION_SOURCE and
//     AGENC_MISSION_SOURCE_METADATA (os.Setenv in `mission resume
//     --run-wrapper`). The drop makes it moot but the ordering also holds.
//   - Introduce case — AgenC has NO value on a default install, so only the
//     DROP protects: AGENC_DIRPATH, AGENC_TEST_ENV (both exported by
//     Server.tmuxEnvPrefix, which returns "" unless the install is namespaced),
//     the three AGENC_CALLING_* names (never in a mission's environment), and
//     OAuthTokenEnvVar (appended only when a token file exists, which under
//     State Y it does not). For these there is nothing to append, and "absent"
//     IS the correct state — so the protection cannot come from ordering.
//
// Note what this deliberately does NOT do: it never invents a value. A protected
// name absent from AgenC's own environment stays absent in the mission's. The
// tempting "fix" of appending AGENC_DIRPATH=<resolved> would newly define that
// variable in every production mission, where today it is unset and path
// construction falls back to ~/.agenc through GetAgencDirpath. That may even be
// an improvement, but it would be a behaviour change arriving as a side effect of
// a reorder, so it is left as its own decision.
// TestBuildClaudeCmdProtectsIntroduceCaseWithoutInventingValues pins both halves.
//
// ADDING AN ENVIRONMENT VARIABLE: if AgenC sets OR reads it in a mission's
// context, add it here too. `TestProtectedEnvVarsCoversEveryAgencEnvVar` scans
// the whole repository for AGENC_-prefixed names — which catches read-sites as
// well as set-sites, since reading one requires naming it — and fails naming any
// that is missing. That is the whole reason this is a list in one place rather
// than a check scattered across call sites. A non-AGENC_ name is NOT caught by
// that scan; OAuthTokenEnvVar is the only one today, and a second would need the
// scan taught about it.
//
// DELIBERATELY NOT INCLUDED. Each of these can be set through `--env`, and the
// line is "AgenC sets it" rather than "AgenC depends on it" — the former is a
// set that terminates and can be enforced, the latter is open-ended.
//
//   - CLAUDE_CONFIG_DIR. AgenC does not set it (State Y — Claude reads the
//     user's real ~/.claude, or whatever the surrounding environment already
//     set); it only forwards an existing value into the pool window. AgenC's
//     operational overlay travels by absolute path through --settings, so a
//     mission pointing Claude at a different config dir keeps AgenC's
//     instrumentation. It does change which credentials and which CLAUDE.md
//     that mission gets.
//
//   - PATH, HOME, TMUX, TMUX_PANE, EDITOR and the rest of the base process
//     environment. The line is what a name lets a mission reach. The names in
//     this list let a mission either masquerade to AgenC's own gates
//     (AGENC_MISSION_SOURCE) or aim an AgenC command at a DIFFERENT mission
//     (the AGENC_CALLING_* names) — both cross a boundary out of the mission.
//     The base-environment names only let a mission damage ITSELF: a bad PATH
//     breaks its own state hooks, a bad EDITOR misruns its own `mission draft`.
//     A mission's Claude can already run arbitrary commands, so affecting
//     itself is not a capability `--env` adds, and enumerating everything a
//     mission depends on has no natural end.
//
//     Checked rather than assumed, because the obvious worry is that TMUX or
//     TMUX_PANE could retarget an operation the way the calling names do. They
//     cannot: TMUX is read only as a presence test (`os.Getenv("TMUX") == ""`
//     in getCurrentTmuxSessionName), and the session it then acts on comes from
//     asking the tmux server via `tmux display-message`, not from the variable.
//     TMUX_PANE is read by the wrapper, which is the mission's PARENT process
//     and does not inherit the mission's `--env`. If a future change makes one
//     of these choose a target, it belongs in this list.
var ProtectedEnvVars = []string{
	MissionUUIDEnvVar,
	MissionSourceEnvVar,
	MissionSourceMetadataEnvVar,
	agencDirpathEnvVar,
	TestEnvVar,
	CallingMissionUUIDEnvVar,
	CallingSessionNameEnvVar,
	CallingPaneIDEnvVar,
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

// AgencDirpathEnvVarName returns the name of the variable carrying the AgenC
// base directory. The constant itself is package-private because path
// construction is supposed to flow through GetAgencDirpath rather than through
// the environment; this exposes only the name, for callers reasoning about the
// protected set.
func AgencDirpathEnvVarName() string {
	return agencDirpathEnvVar
}

// IsProtectedEnvVar reports whether AgenC sets this environment variable
// itself, and therefore whether a caller naming it would be overridden.
func IsProtectedEnvVar(name string) bool {
	return protectedEnvVarSet[name]
}
