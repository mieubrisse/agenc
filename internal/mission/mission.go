package mission

import (
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

// CreateMissionDir sets up the mission directory structure and rsyncs config
// files from the agent template into the agent/ subdirectory. When
// worktreeSource is non-empty, the workspace/ directory is created as a Git
// worktree of the specified repository. Returns the mission root directory
// path (not the agent/ subdirectory).
func CreateMissionDir(agencDirpath string, missionID string, agentTemplate string, worktreeSource string) (string, error) {
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)
	workspaceDirpath := config.GetMissionWorkspaceDirpath(agencDirpath, missionID)

	// Always create mission and agent directories
	for _, dirpath := range []string{missionDirpath, agentDirpath} {
		if err := os.MkdirAll(dirpath, 0755); err != nil {
			return "", stacktrace.Propagate(err, "failed to create directory '%s'", dirpath)
		}
	}

	if worktreeSource != "" {
		// Create workspace as a Git worktree
		branchName := GetWorktreeBranchName(missionID)
		if err := CreateWorktree(worktreeSource, workspaceDirpath, branchName); err != nil {
			return "", stacktrace.Propagate(err, "failed to create git worktree")
		}
	} else {
		// Create a plain workspace directory
		if err := os.MkdirAll(workspaceDirpath, 0755); err != nil {
			return "", stacktrace.Propagate(err, "failed to create directory '%s'", workspaceDirpath)
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

// CreateEmbeddedAgentMissionDir sets up the mission directory structure for an
// embedded-agent mission. The worktree is created directly at the agent/
// directory (no workspace/ subdirectory). No template is synced or tracked.
// Returns the mission root directory path.
func CreateEmbeddedAgentMissionDir(agencDirpath string, missionID string, worktreeSource string) (string, error) {
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)

	if err := os.MkdirAll(missionDirpath, 0755); err != nil {
		return "", stacktrace.Propagate(err, "failed to create directory '%s'", missionDirpath)
	}

	branchName := GetWorktreeBranchName(missionID)
	if err := CreateWorktree(worktreeSource, agentDirpath, branchName); err != nil {
		return "", stacktrace.Propagate(err, "failed to create git worktree at agent directory")
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

// SpawnClaude starts claude as a child process in the given agent directory
// with the given prompt. Returns the running command. The caller is
// responsible for calling cmd.Wait().
func SpawnClaude(agencDirpath string, agentDirpath string, prompt string) (*exec.Cmd, error) {
	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'claude' binary not found in PATH")
	}

	claudeConfigDirpath := config.GetGlobalClaudeDirpath(agencDirpath)

	args := []string{claudeBinary}
	if prompt != "" {
		args = append(args, prompt)
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = agentDirpath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+claudeConfigDirpath)

	if err := cmd.Start(); err != nil {
		return nil, stacktrace.Propagate(err, "failed to start claude")
	}

	return cmd, nil
}

// SpawnClaudeResume starts claude -c as a child process in the given agent
// directory. Returns the running command. The caller is responsible for
// calling cmd.Wait().
func SpawnClaudeResume(agencDirpath string, agentDirpath string) (*exec.Cmd, error) {
	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'claude' binary not found in PATH")
	}

	claudeConfigDirpath := config.GetGlobalClaudeDirpath(agencDirpath)

	cmd := exec.Command(claudeBinary, "-c")
	cmd.Dir = agentDirpath
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "CLAUDE_CONFIG_DIR="+claudeConfigDirpath)

	if err := cmd.Start(); err != nil {
		return nil, stacktrace.Propagate(err, "failed to start claude -c")
	}

	return cmd, nil
}
