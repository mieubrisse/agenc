package mission

import (
	"os"
	"os/exec"
	"path/filepath"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

// CreateMissionDir sets up the mission directory structure. When gitRepoSource
// is non-empty, the repository is copied directly as the agent/ directory
// (agent/ IS the repo). When gitRepoSource is empty, an empty agent/ directory
// is created. Returns the mission root directory path (not the agent/
// subdirectory).
func CreateMissionDir(agencDirpath string, missionID string, gitRepoName string, gitRepoSource string) (string, error) {
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)

	if err := os.MkdirAll(missionDirpath, 0755); err != nil {
		return "", stacktrace.Propagate(err, "failed to create directory '%s'", missionDirpath)
	}

	if gitRepoSource != "" {
		// Copy the repo directly as agent/ (CopyRepo creates the destination)
		if err := CopyRepo(gitRepoSource, agentDirpath); err != nil {
			return "", stacktrace.Propagate(err, "failed to copy git repo into agent directory")
		}
	} else {
		if err := os.MkdirAll(agentDirpath, 0755); err != nil {
			return "", stacktrace.Propagate(err, "failed to create directory '%s'", agentDirpath)
		}
	}

	return missionDirpath, nil
}

// buildClaudeCmd constructs an exec.Cmd for running Claude in the given agent
// directory. If a secrets.env file exists at .claude/secrets.env within the
// agent directory, the command is wrapped with `op run` to inject 1Password
// secrets. Otherwise, Claude is invoked directly.
func buildClaudeCmd(agencDirpath string, missionID string, agentDirpath string, claudeArgs []string) (*exec.Cmd, error) {
	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'claude' binary not found in PATH")
	}

	claudeConfigDirpath := config.GetGlobalClaudeDirpath(agencDirpath)

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
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"CLAUDE_CONFIG_DIR="+claudeConfigDirpath,
		"AGENC_MISSION_UUID="+missionID,
	)

	return cmd, nil
}

// SpawnClaude starts claude as a child process in the given agent directory.
// Returns the running command. The caller is responsible for calling cmd.Wait().
func SpawnClaude(agencDirpath string, missionID string, agentDirpath string) (*exec.Cmd, error) {
	return SpawnClaudeWithPrompt(agencDirpath, missionID, agentDirpath, "")
}

// SpawnClaudeWithPrompt starts claude with an initial prompt as a child process
// in the given agent directory. Claude always starts in interactive mode; if
// initialPrompt is non-empty, it is passed as a positional argument to pre-fill
// the first message. Returns the running command. The caller is responsible for
// calling cmd.Wait().
func SpawnClaudeWithPrompt(agencDirpath string, missionID string, agentDirpath string, initialPrompt string) (*exec.Cmd, error) {
	var args []string
	if initialPrompt != "" {
		// Pass prompt as positional argument for interactive mode with pre-filled message
		args = []string{initialPrompt}
	}

	cmd, err := buildClaudeCmd(agencDirpath, missionID, agentDirpath, args)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to build claude command")
	}

	if err := cmd.Start(); err != nil {
		return nil, stacktrace.Propagate(err, "failed to start claude")
	}

	return cmd, nil
}

// SpawnClaudeResume starts claude -c as a child process in the given agent
// directory. Returns the running command. The caller is responsible for
// calling cmd.Wait().
func SpawnClaudeResume(agencDirpath string, missionID string, agentDirpath string) (*exec.Cmd, error) {
	cmd, err := buildClaudeCmd(agencDirpath, missionID, agentDirpath, []string{"-c"})
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to build claude resume command")
	}

	if err := cmd.Start(); err != nil {
		return nil, stacktrace.Propagate(err, "failed to start claude -c")
	}

	return cmd, nil
}
