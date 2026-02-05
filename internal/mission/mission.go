package mission

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

// CreateMissionDir sets up the mission directory structure and rsyncs config
// files from the agent template into the agent/ subdirectory. When
// gitRepoSource is non-empty, the repository is copied into a subdirectory
// of workspace/ named after the repo (e.g. workspace/some-repo/). gitRepoName
// is the canonical name (e.g. "github.com/owner/repo") used to derive the
// subdirectory name. Returns the mission root directory path (not the agent/
// subdirectory).
func CreateMissionDir(agencDirpath string, missionID string, agentTemplate string, gitRepoName string, gitRepoSource string) (string, error) {
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)
	workspaceDirpath := config.GetMissionWorkspaceDirpath(agencDirpath, missionID)

	// Always create mission, agent, and workspace directories
	for _, dirpath := range []string{missionDirpath, agentDirpath, workspaceDirpath} {
		if err := os.MkdirAll(dirpath, 0755); err != nil {
			return "", stacktrace.Propagate(err, "failed to create directory '%s'", dirpath)
		}
	}

	if gitRepoSource != "" {
		// Copy the repo into workspace/<repo-short-name>/
		repoShortName := filepath.Base(gitRepoName)
		repoDirpath := filepath.Join(workspaceDirpath, repoShortName)
		if err := CopyRepo(gitRepoSource, repoDirpath); err != nil {
			return "", stacktrace.Propagate(err, "failed to copy git repo into workspace")
		}
	}

	if agentTemplate == "" {
		return missionDirpath, nil
	}

	templateDirpath := config.GetRepoDirpath(agencDirpath, agentTemplate)

	if err := RsyncTemplate(templateDirpath, agentDirpath); err != nil {
		return "", stacktrace.Propagate(err, "failed to rsync template into agent directory")
	}

	commitHash, err := ReadTemplateCommitHash(templateDirpath)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read template commit hash")
	}
	commitFilepath := config.GetMissionTemplateCommitFilepath(agencDirpath, missionID)
	if err := os.WriteFile(commitFilepath, []byte(commitHash), 0644); err != nil {
		return "", stacktrace.Propagate(err, "failed to write template-commit file")
	}

	return missionDirpath, nil
}

// RsyncTemplate rsyncs a template directory into the agent directory,
// excluding the workspace/ subdirectory, .git/ metadata, and
// .claude/settings.local.json (mission-local overrides). Uses --delete
// to remove files no longer in the template.
func RsyncTemplate(templateDirpath string, agentDirpath string) error {
	srcPath := templateDirpath + "/"
	dstPath := agentDirpath + "/"

	settingsLocalRelFilepath := config.UserClaudeDirname + "/" + config.SettingsLocalFilename

	cmd := exec.Command("rsync",
		"-a",
		"--delete",
		"--exclude", config.WorkspaceDirname+"/",
		"--exclude", ".git/",
		"--exclude", settingsLocalRelFilepath,
		srcPath,
		dstPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.Propagate(err, "rsync failed: %s", string(output))
	}
	return nil
}

// ReadTemplateCommitHash reads the current commit hash of the default branch
// in the given template repository directory.
func ReadTemplateCommitHash(templateDirpath string) (string, error) {
	defaultBranch, err := GetDefaultBranch(templateDirpath)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to determine default branch in '%s'", templateDirpath)
	}

	cmd := exec.Command("git", "rev-parse", defaultBranch)
	cmd.Dir = templateDirpath
	output, err := cmd.Output()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read git commit hash in '%s'", templateDirpath)
	}
	return strings.TrimSpace(string(output)), nil
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
