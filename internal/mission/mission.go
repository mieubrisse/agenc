package mission

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

const (
	claudeDirname    = ".claude"
	claudeMDFilename = "CLAUDE.md"
	settingsFilename = "settings.json"
	mcpFilename      = ".mcp.json"
)

// CreateMissionDir sets up the mission directory structure and copies config
// files from the agent template.
func CreateMissionDir(agencDirpath string, missionID string, agentTemplate string) (string, error) {
	missionDirpath := filepath.Join(config.GetMissionsDirpath(agencDirpath), missionID)
	missionClaudeDirpath := filepath.Join(missionDirpath, claudeDirname)

	for _, dirpath := range []string{missionDirpath, missionClaudeDirpath} {
		if err := os.MkdirAll(dirpath, 0755); err != nil {
			return "", stacktrace.Propagate(err, "failed to create directory '%s'", dirpath)
		}
	}

	if agentTemplate == "" {
		return missionDirpath, nil
	}

	agentTemplateDirpath := filepath.Join(config.GetAgentTemplatesDirpath(agencDirpath), agentTemplate)

	// Copy CLAUDE.md
	if err := copyFileIfExists(
		filepath.Join(agentTemplateDirpath, claudeMDFilename),
		filepath.Join(missionDirpath, claudeMDFilename),
	); err != nil {
		return "", stacktrace.Propagate(err, "failed to copy CLAUDE.md")
	}

	// Copy .claude/settings.json
	if err := copyFileIfExists(
		filepath.Join(agentTemplateDirpath, claudeDirname, settingsFilename),
		filepath.Join(missionClaudeDirpath, settingsFilename),
	); err != nil {
		return "", stacktrace.Propagate(err, "failed to copy settings.json")
	}

	// Copy .mcp.json
	if err := copyFileIfExists(
		filepath.Join(agentTemplateDirpath, mcpFilename),
		filepath.Join(missionDirpath, mcpFilename),
	); err != nil {
		return "", stacktrace.Propagate(err, "failed to copy .mcp.json")
	}

	return missionDirpath, nil
}

// copyFileIfExists copies src to dst. If src does not exist, it does nothing.
func copyFileIfExists(srcFilepath string, dstFilepath string) error {
	srcFile, err := os.Open(srcFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return stacktrace.Propagate(err, "failed to open source file '%s'", srcFilepath)
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dstFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create destination file '%s'", dstFilepath)
	}
	defer dstFile.Close()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return stacktrace.Propagate(err, "failed to copy '%s' to '%s'", srcFilepath, dstFilepath)
	}

	return nil
}

// ExecClaude replaces the current process with claude, running in the
// mission directory.
func ExecClaude(agencDirpath string, missionDirpath string, prompt string) error {
	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return stacktrace.Propagate(err, "'claude' binary not found in PATH")
	}

	claudeConfigDirpath := config.GetGlobalClaudeDirpath(agencDirpath)

	env := os.Environ()
	env = append(env, "CLAUDE_CONFIG_DIR="+claudeConfigDirpath)

	args := []string{"claude"}
	if prompt != "" {
		args = append(args, prompt)
	}

	if err := os.Chdir(missionDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to change directory to '%s'", missionDirpath)
	}

	return syscall.Exec(claudeBinary, args, env)
}

// ExecClaudeResume replaces the current process with claude --continue,
// running in the mission directory.
func ExecClaudeResume(agencDirpath string, missionDirpath string) error {
	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return stacktrace.Propagate(err, "'claude' binary not found in PATH")
	}

	claudeConfigDirpath := config.GetGlobalClaudeDirpath(agencDirpath)

	env := os.Environ()
	env = append(env, "CLAUDE_CONFIG_DIR="+claudeConfigDirpath)

	args := []string{"claude", "-c"}

	if err := os.Chdir(missionDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to change directory to '%s'", missionDirpath)
	}

	return syscall.Exec(claudeBinary, args, env)
}
