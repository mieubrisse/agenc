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
// (AGENC_MISSION_UUID, conditionally CLAUDE_CODE_OAUTH_TOKEN) set but does NOT
// set stdin/stdout/stderr — callers should wire those as needed (e.g.
// interactive mode connects to the terminal, headless mode uses pipes).
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
func BuildClaudeCmd(agencDirpath string, missionID string, agentDirpath string, extraClaudeArgs []string, claudeArgs []string) (*exec.Cmd, error) {
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
	cmd.Env = append(os.Environ(),
		config.MissionUUIDEnvVar+"="+missionID,
	)

	// Inject the machine OAuth token only when one is configured (the State-X
	// fallback toggle). When absent, Claude uses its native ~/.claude auth —
	// the default under State Y. An absent token is no longer an error.
	oauthToken, err := config.ReadOAuthToken(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read OAuth token")
	}
	if oauthToken != "" {
		cmd.Env = append(cmd.Env, "CLAUDE_CODE_OAUTH_TOKEN="+oauthToken)
	}

	return cmd, nil
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
	// Ending option parsing first is what makes the prompt unambiguous. It
	// belongs here rather than at the head of the arg list, because at the head
	// it would turn the conversation-shape flags that follow (-c, -r <session>)
	// into positionals instead.
	return []string{endOfClaudeFlagsSeparator, initialPrompt}
}

// SpawnClaudeWithPrompt starts claude with an initial prompt as a child process
// in the given agent directory. Claude always starts in interactive mode; if
// initialPrompt is non-empty, it is passed as a positional argument to pre-fill
// the first message. Returns the running command. The caller is responsible for
// calling cmd.Wait().
func SpawnClaudeWithPrompt(agencDirpath string, missionID string, agentDirpath string, extraClaudeArgs []string, initialPrompt string) (*exec.Cmd, error) {
	args := buildNewConversationArgs(initialPrompt)

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, extraClaudeArgs, args)
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
func SpawnClaudeResumeWithSession(agencDirpath string, missionID string, agentDirpath string, extraClaudeArgs []string, sessionID string, initialPrompt string) (*exec.Cmd, error) {
	args := buildResumeArgs(sessionID, initialPrompt)

	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, extraClaudeArgs, args)
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
