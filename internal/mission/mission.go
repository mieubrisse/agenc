package mission

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

// CreateMissionDir sets up the mission directory structure. When gitRepoSource
// is non-empty, the repository is copied directly as the agent/ directory
// (agent/ IS the repo). When gitRepoSource is empty, an empty agent/ directory
// is created.
//
// The per-mission claude config directory is built by the wrapper on every
// Claude spawn, so this function does not pre-build it.
//
// The logger receives copy-degradation warnings from CopyRepo.
//
// Returns the mission root directory path (not the agent/ subdirectory).
func CreateMissionDir(logger *log.Logger, agencDirpath string, missionID string, gitRepoName string, gitRepoSource string) (string, error) {
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)

	if err := os.MkdirAll(missionDirpath, 0755); err != nil {
		return "", stacktrace.Propagate(err, "failed to create directory '%s'", missionDirpath)
	}

	if gitRepoSource != "" {
		// Copy the repo directly as agent/ (CopyRepo creates the destination)
		if err := CopyRepo(logger, gitRepoSource, agentDirpath); err != nil {
			return "", stacktrace.Propagate(err, "failed to copy git repo into agent directory")
		}
	} else {
		if err := os.MkdirAll(agentDirpath, 0755); err != nil {
			return "", stacktrace.Propagate(err, "failed to create directory '%s'", agentDirpath)
		}
	}

	return missionDirpath, nil
}

// BuildClaudeCmd constructs an exec.Cmd for running Claude in the given agent
// directory. If a secrets.env file exists at .claude/secrets.env within the
// agent directory, the command is wrapped with `op run` to inject 1Password
// secrets. Otherwise, Claude is invoked directly.
//
// Under State Y (native passthrough) the command does NOT set CLAUDE_CONFIG_DIR
// — Claude reads the user's real ~/.claude (or whatever CLAUDE_CONFIG_DIR the
// surrounding environment already sets, e.g. the e2e harness). AgenC's
// operational layer (hooks, prime, repo-library guard, agent-dir allow, server
// socket) is delivered per-invocation via `claude --settings <op-settings file>`.
// The OAuth token is injected only when a token file is present (machine-token
// fallback); absent, Claude uses its native ~/.claude auth.
//
// The returned command has its working directory and environment variables
// (the mission's own missionEnv, AGENC_MISSION_UUID, conditionally
// CLAUDE_CODE_OAUTH_TOKEN) set but does NOT set stdin/stdout/stderr — callers
// should wire those as needed (e.g. interactive mode connects to the terminal,
// headless mode uses pipes).
//
// Args reach Claude in three groups, in this order: AgenC's --settings overlay,
// then extraClaudeArgs (the model/config/per-mission args already merged by
// MergeClaudeArgs), then claudeArgs (the call-site args that choose the
// conversation shape — -c, -r <session>, --print, the initial prompt).
//
// A call-site arg list that ends in a positional argument must separate it with
// "--", since extraClaudeArgs can now end in a flag that would otherwise
// swallow it; buildNewConversationArgs explains why and buildResumeArgs follows
// the same rule.
//
// missionEnv holds the mission's own environment variables, read off the
// mission record by the caller. They go into the spawned process's environment
// rather than onto its command line, which is what keeps AgenC's --settings
// overlay out of the matter entirely: Claude takes only the last --settings and
// does not merge, so a caller marking a session through --settings would
// replace AgenC's hooks, prime hook, repo-library guard, agent-dir allow and
// server-socket allowlist wholesale while the mission went on looking healthy.
func BuildClaudeCmd(
	agencDirpath string,
	missionID string,
	agentDirpath string,
	extraClaudeArgs []string,
	claudeArgs []string,
	missionEnv map[string]string,
) (*exec.Cmd, error) {
	// Deliver AgenC's operational overlay (hooks, prime, guard, allow/deny,
	// server socket) via --settings so it applies to every spawn shape
	// (interactive, resume, headless). --settings unions with the user's own
	// ~/.claude/settings.json rather than replacing it.
	opSettingsFilepath := config.GetMissionOpSettingsFilepath(agencDirpath, missionID)

	fullArgs := []string{settingsClaudeFlag, opSettingsFilepath}
	fullArgs = append(fullArgs, extraClaudeArgs...)
	fullArgs = append(fullArgs, claudeArgs...)
	claudeArgs = fullArgs

	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'claude' binary not found in PATH")
	}

	secretsEnvFilepath := filepath.Join(agentDirpath, config.UserClaudeDirname, config.SecretsEnvFilename)

	var cmd *exec.Cmd
	if _, statErr := os.Stat(secretsEnvFilepath); statErr == nil {
		// secrets.env exists — wrap with op run
		opBinary, err := exec.LookPath("op")
		if err != nil {
			return nil, stacktrace.Propagate(err, "'op' (1Password CLI) not found in PATH; required because '%s' exists", secretsEnvFilepath)
		}

		opArgs := []string{
			"run",
			"--env-file", secretsEnvFilepath,
			"--no-masking",
			"--",
			claudeBinary,
		}
		opArgs = append(opArgs, claudeArgs...)
		cmd = exec.Command(opBinary, opArgs...)
	} else {
		// No secrets.env — run claude directly
		cmd = exec.Command(claudeBinary, claudeArgs...)
	}

	cmd.Dir = agentDirpath

	// Order is load-bearing, in three layers.
	//
	// os.Environ() is the base. The mission's own variables go on top of it,
	// because a mission that explicitly sets a variable means it and should win
	// over whatever the surrounding shell happened to export. Then AgenC's own
	// variables go on top of those, so a caller naming one of them is
	// overridden rather than overriding — a process resolves a repeated name to
	// the last entry.
	//
	// The third layer is driven by config.ProtectedEnvVars rather than written
	// out here, which is what keeps it from going stale: a new AgenC variable is
	// protected by being added to that one list, with no change at this call
	// site. Several of these reach AgenC by routes that land inside
	// os.Environ() — AGENC_MISSION_SOURCE via an os.Setenv in `mission resume
	// --run-wrapper`, AGENC_DIRPATH via the tmux env prefix — which is exactly
	// why re-applying them here is necessary: without it they would sit BELOW
	// the mission's variables and a caller could override them. One of them is
	// load-bearing for Kevin's own night-time gate, which exempts a mission
	// whose AGENC_MISSION_SOURCE is "cron".
	//
	// A protected name is also DROPPED from the mission's own variables rather
	// than merely outranked. Appending afterwards is not sufficient on its own:
	// for CLAUDE_CODE_OAUTH_TOKEN, AgenC's intent when no token file exists is
	// that the variable be UNSET so Claude uses its native ~/.claude auth, and
	// there is no value to append that expresses "unset". Without the drop, a
	// mission on such an install — which is the default — could supply its own
	// token and have Claude authenticate with it.
	//
	// `--env` validation also refuses these names, so a caller normally learns
	// at creation rather than being silently dropped here. The drop and the
	// ordering are the guarantee; that refusal is the courtesy.
	//
	// One route where the last word is not AgenC's: when a mission has a
	// 1Password secrets file, Claude runs under `op run` with that file, and
	// `op` applies the file over the environment it inherited. So a name
	// appearing in BOTH that file and a mission's own variables resolves to the
	// file's value. That is the secrets file winning rather than a caller, and
	// a protected name cannot reach either side of it -- but it does mean the
	// last-wins resolution on that path belongs to `op`, not to this function.
	cmd.Env = append(os.Environ(), callerOwnedEnvAssignments(missionEnv)...)
	cmd.Env = append(cmd.Env, agencOwnedEnvAssignments(missionID)...)

	// Inject the machine OAuth token only when one is configured (the State-X
	// fallback toggle). When absent, Claude uses its native ~/.claude auth —
	// the default under State Y. An absent token is no longer an error.
	oauthToken, err := config.ReadOAuthToken(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read OAuth token")
	}
	if oauthToken != "" {
		cmd.Env = append(cmd.Env, config.OAuthTokenEnvVar+"="+oauthToken)
	}

	return cmd, nil
}

// callerOwnedEnvAssignments returns a mission's own environment assignments
// with every name AgenC owns removed, so that nothing a caller supplied can
// occupy one of those names in the spawned process. Driven by
// config.ProtectedEnvVars, so a newly protected variable is dropped here
// without a change at this call site.
//
// Normally a no-op: validation refuses these names at creation, so a stored
// mission environment should never contain one. It is here for the value that
// reached the record some other way — a client written against a build that
// predates the validation, or a hand-edited row — because the protection is
// supposed to be a property of the spawn rather than of what validation
// happened to catch.
func callerOwnedEnvAssignments(missionEnv map[string]string) []string {
	return MissionEnvToAssignments(effectiveMissionEnv(missionEnv))
}

// agencOwnedEnvAssignments returns the environment assignments AgenC applies on
// top of a mission's own, so that AgenC's value for a name it owns wins. The
// set comes from config.ProtectedEnvVars, so protecting a new variable is a
// one-line change there rather than a change here.
//
// The mission UUID is produced from missionID, which is the authoritative value
// and the reason this takes a parameter at all. Every other protected variable
// is re-applied from the ambient environment, because AgenC put it there
// earlier in the spawn chain (an os.Setenv in `mission resume --run-wrapper`,
// or the tmux env prefix) and the point is to lift it back above the mission's
// own variables. One that is absent is skipped rather than emitted empty: an
// empty assignment is not the same as an unset variable, and a hook testing
// "${AGENC_MISSION_SOURCE:-}" would read the two differently.
//
// The OAuth token is deliberately not here. It is read from disk rather than
// from the environment and only when a token file exists, so BuildClaudeCmd
// appends it separately — after this, which keeps it protected.
func agencOwnedEnvAssignments(missionID string) []string {
	assignments := make([]string, 0, len(config.ProtectedEnvVars))
	for _, name := range config.ProtectedEnvVars {
		if name == config.MissionUUIDEnvVar {
			assignments = append(assignments, name+"="+missionID)
			continue
		}
		if name == config.OAuthTokenEnvVar {
			continue
		}
		value, found := os.LookupEnv(name)
		if !found {
			continue
		}
		assignments = append(assignments, name+"="+value)
	}
	return assignments
}

// buildNewConversationArgs returns the claude CLI args for starting a fresh
// conversation. Pure helper extracted for unit testing.
func buildNewConversationArgs(initialPrompt string) []string {
	if initialPrompt == "" {
		return nil
	}
	// The prompt is a positional argument, and it is the last thing on the
	// command line — so whatever flag precedes it decides whether Claude reads
	// it as a prompt at all. Several Claude flags take a variable number of
	// values (--add-dir <directories...>) or an optional one (--debug [filter]),
	// and such a flag swallows the following bare token. Since a mission can now
	// carry arbitrary passthrough flags, one of those can land immediately
	// before the prompt, and the failure is silent: the prompt becomes an extra
	// allowed directory, Claude starts with no prompt, and nothing errors.
	//
	// Ending option parsing first is what makes the prompt unambiguous, and it
	// fixes a second failure in the same position: a prompt beginning with a
	// dash stops Claude starting at all. Measured on claude 2.1.274,
	// `claude -p '-Reply with exactly: DASHOK'` exits with
	// "error: unknown option", and the same command with `--` before the prompt
	// answers DASHOK.
	//
	// The separator belongs here rather than at the head of the arg list,
	// because at the head it would turn the conversation-shape flags that
	// follow (-c, -r <session>) into positionals instead.
	return []string{endOfClaudeFlagsSeparator, initialPrompt}
}

// SpawnClaudeWithPrompt starts claude with an initial prompt as a child process
// in the given agent directory. Claude always starts in interactive mode; if
// initialPrompt is non-empty, it is passed as a positional argument to pre-fill
// the first message. Returns the running command. The caller is responsible for
// calling cmd.Wait().
func SpawnClaudeWithPrompt(agencDirpath string, missionID string, agentDirpath string, extraClaudeArgs []string, initialPrompt string, missionEnv map[string]string) (*exec.Cmd, error) {
	args := buildNewConversationArgs(initialPrompt)

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, extraClaudeArgs, args, missionEnv)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to build claude command")
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, stacktrace.Propagate(err, "failed to start claude")
	}

	return cmd, nil
}

// buildResumeArgs returns the claude CLI args for resuming a conversation.
// Pure helper extracted for unit testing.
func buildResumeArgs(sessionID string, initialPrompt string) []string {
	var args []string
	if sessionID != "" {
		args = []string{"-r", sessionID}
	} else {
		args = []string{"-c"}
	}
	// Separate the positional prompt from the flags, for the reason given in
	// buildNewConversationArgs. The resume shape happens to be safe already --
	// the "-c" or "-r <session>" it emits first terminates any variadic flag
	// behind it -- but that safety is incidental to an argument order nothing
	// pins, so the prompt carries its own guard rather than borrowing one.
	if initialPrompt != "" {
		args = append(args, endOfClaudeFlagsSeparator, initialPrompt)
	}
	return args
}

// SpawnClaudeResumeWithSession starts claude with session-based resumption
// using claude -r <session-id>. If sessionID is empty, falls back to
// claude -c. When initialPrompt is non-empty, it is appended as a positional
// argument so claude submits it as the first message after resuming.
// Returns the running command. The caller is responsible for calling
// cmd.Wait().
func SpawnClaudeResumeWithSession(agencDirpath string, missionID string, agentDirpath string, extraClaudeArgs []string, sessionID string, initialPrompt string, missionEnv map[string]string) (*exec.Cmd, error) {
	args := buildResumeArgs(sessionID, initialPrompt)

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, extraClaudeArgs, args, missionEnv)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to build claude resume command")
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, stacktrace.Propagate(err, "failed to start claude resume")
	}

	return cmd, nil
}
