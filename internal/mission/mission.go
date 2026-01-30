package mission

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/merge"
)

const (
	claudeDirname   = ".claude"
	workspaceDirname = "workspace"
	claudeMDFilename    = "CLAUDE.md"
	settingsFilename    = "settings.json"
	mcpFilename         = ".mcp.json"
)

// CreateMissionDir sets up the mission directory structure and merges config files.
func CreateMissionDir(agencDirpath string, missionID string, agentTemplate string) (string, error) {
	missionDirpath := filepath.Join(config.GetMissionsDirpath(agencDirpath), missionID)
	missionClaudeDirpath := filepath.Join(missionDirpath, claudeDirname)
	workspaceDirpath := filepath.Join(missionDirpath, workspaceDirname)

	for _, dirpath := range []string{missionDirpath, missionClaudeDirpath, workspaceDirpath} {
		if err := os.MkdirAll(dirpath, 0755); err != nil {
			return "", stacktrace.Propagate(err, "failed to create directory '%s'", dirpath)
		}
	}

	globalClaudeDirpath := config.GetGlobalClaudeDirpath(agencDirpath)
	agentTemplateDirpath := filepath.Join(config.GetAgentTemplatesDirpath(agencDirpath), agentTemplate)

	// Merge CLAUDE.md
	globalClaudeMDFilepath := filepath.Join(globalClaudeDirpath, claudeMDFilename)
	agentClaudeMDFilepath := filepath.Join(agentTemplateDirpath, claudeMDFilename)
	mergedClaudeMD, err := merge.MergeCLAUDEMD(globalClaudeMDFilepath, agentClaudeMDFilepath)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to merge CLAUDE.md")
	}
	if mergedClaudeMD != "" {
		outputFilepath := filepath.Join(missionDirpath, claudeMDFilename)
		if err := os.WriteFile(outputFilepath, []byte(mergedClaudeMD), 0644); err != nil {
			return "", stacktrace.Propagate(err, "failed to write merged CLAUDE.md")
		}
	}

	// Merge settings.json
	globalSettingsFilepath := filepath.Join(globalClaudeDirpath, settingsFilename)
	agentSettingsFilepath := filepath.Join(agentTemplateDirpath, settingsFilename)
	mergedSettings, err := merge.MergeSettingsJSON(globalSettingsFilepath, agentSettingsFilepath)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to merge settings.json")
	}
	if mergedSettings != nil {
		outputFilepath := filepath.Join(missionClaudeDirpath, settingsFilename)
		if err := os.WriteFile(outputFilepath, mergedSettings, 0644); err != nil {
			return "", stacktrace.Propagate(err, "failed to write merged settings.json")
		}
	}

	// Merge .mcp.json
	globalMCPFilepath := filepath.Join(globalClaudeDirpath, mcpFilename)
	agentMCPFilepath := filepath.Join(agentTemplateDirpath, mcpFilename)
	mergedMCP, err := merge.MergeMCPJSON(globalMCPFilepath, agentMCPFilepath)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to merge .mcp.json")
	}
	if mergedMCP != nil {
		outputFilepath := filepath.Join(missionDirpath, mcpFilename)
		if err := os.WriteFile(outputFilepath, mergedMCP, 0644); err != nil {
			return "", stacktrace.Propagate(err, "failed to write merged .mcp.json")
		}
	}

	return missionDirpath, nil
}

// ExecClaude replaces the current process with claude, running in the
// mission's workspace directory.
func ExecClaude(agencDirpath string, missionDirpath string, prompt string) error {
	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return stacktrace.Propagate(err, "'claude' binary not found in PATH")
	}

	workspaceDirpath := filepath.Join(missionDirpath, workspaceDirname)
	claudeConfigDirpath := config.GetGlobalClaudeDirpath(agencDirpath)

	env := os.Environ()
	env = append(env, "CLAUDE_CONFIG_DIR="+claudeConfigDirpath)

	args := []string{"claude", prompt}

	if err := os.Chdir(workspaceDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to change directory to '%s'", workspaceDirpath)
	}

	return syscall.Exec(claudeBinary, args, env)
}

// ExecClaudeResume replaces the current process with claude --continue,
// running in the mission's workspace directory.
func ExecClaudeResume(agencDirpath string, missionDirpath string) error {
	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return stacktrace.Propagate(err, "'claude' binary not found in PATH")
	}

	workspaceDirpath := filepath.Join(missionDirpath, workspaceDirname)
	claudeConfigDirpath := config.GetGlobalClaudeDirpath(agencDirpath)

	env := os.Environ()
	env = append(env, "CLAUDE_CONFIG_DIR="+claudeConfigDirpath)

	args := []string{"claude", "-c"}

	if err := os.Chdir(workspaceDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to change directory to '%s'", workspaceDirpath)
	}

	return syscall.Exec(claudeBinary, args, env)
}
