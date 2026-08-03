package claudeconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/session"
)

const (
	// MissionClaudeConfigDirname is the directory name for the per-mission
	// claude-config snapshot. Under State Y this directory is NOT built;
	// the const is retained for tests that assert its absence.
	MissionClaudeConfigDirname = "claude-config"
)

// GetMissionClaudeConfigDirpath returns the per-mission claude config directory
// if it exists, otherwise falls back to the global claude config directory.
// This provides backward compatibility for missions created before per-mission
// config was implemented.
func GetMissionClaudeConfigDirpath(agencDirpath string, missionID string) string {
	missionConfigDirpath := filepath.Join(
		config.GetMissionDirpath(agencDirpath, missionID),
		MissionClaudeConfigDirname,
	)

	if _, err := os.Stat(missionConfigDirpath); err == nil {
		return missionConfigDirpath
	}

	return config.GetGlobalClaudeDirpath(agencDirpath)
}

// WriteAgencHookScripts writes the AgenC-managed hook scripts into the
// given directory. Under State Y, the base directory is the mission dir
// (not a per-mission claude-config snapshot).
func WriteAgencHookScripts(claudeConfigDirpath string) error {
	hooksDirpath := filepath.Join(claudeConfigDirpath, AgencHooksDirname)
	if err := os.MkdirAll(hooksDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create '%s'", hooksDirpath)
	}

	scriptFilepath := filepath.Join(hooksDirpath, RepoLibraryGuardScriptName)
	if err := os.WriteFile(scriptFilepath, []byte(RepoLibraryGuardScript), 0755); err != nil {
		return stacktrace.Propagate(err, "failed to write hook script '%s'", scriptFilepath)
	}

	return nil
}

// GetLastSessionID returns the most recent session ID for a mission by
// scanning the filesystem for JSONL session files sorted by modification
// time (most recent first). This is reliable for both active and completed
// sessions, unlike .claude.json's lastSessionId field which is only
// populated at session close.
// Returns empty string if no session is found.
func GetLastSessionID(agencDirpath string, missionID string) string {
	projectDirpath, err := GetMissionProjectDirpath(agencDirpath, missionID)
	if err != nil {
		return ""
	}

	sessionIDs := session.ListSessionIDs(projectDirpath)
	if len(sessionIDs) > 0 {
		return sessionIDs[0]
	}

	return ""
}

// GetMissionProjectDirpath returns the Claude Code project directory where the
// mission's transcripts live: ~/.claude/projects/<encoded-agent-dir>.
func GetMissionProjectDirpath(agencDirpath string, missionID string) (string, error) {
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)
	return ComputeProjectDirpath(agentDirpath)
}

// ComputeProjectDirpath returns the absolute path to the Claude Code project
// directory for the given agent directory path. Claude Code transforms absolute
// paths into project directory names by converting both slashes and dots to
// hyphens.
// For example: /Users/name/.config/path -> ~/.claude/projects/-Users-name--config-path
func ComputeProjectDirpath(agentDirpath string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting user home directory: %w", err)
	}

	// Claude Code converts both "/" and "." to "-"
	// So "/Users/odyssey/.agenc" becomes "-Users-odyssey--agenc"
	projectDirName := strings.ReplaceAll(strings.ReplaceAll(agentDirpath, "/", "-"), ".", "-")
	projectDirpath := filepath.Join(homeDir, ".claude", "projects", projectDirName)

	return projectDirpath, nil
}

// ProjectDirectoryExists checks whether Claude Code has created a project
// directory for the given agent directory path. Claude Code transforms
// absolute paths into project directory names by converting both slashes
// and dots to hyphens.
// For example: /Users/name/.config/path -> -Users-name--config-path
//
// Callers that use `claude -r <session-id>` should check this before
// attempting resume — Claude Code won't have session data if the project
// directory doesn't exist yet.
func ProjectDirectoryExists(agentDirpath string) bool {
	projectDirpath, err := ComputeProjectDirpath(agentDirpath)
	if err != nil {
		return false
	}

	_, err = os.Stat(projectDirpath)
	return err == nil
}
