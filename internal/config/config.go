package config

import (
	"os"
	"path/filepath"

	"github.com/mieubrisse/stacktrace"
)

const (
	agencDirpathEnvVar  = "AGENC_DIRPATH"
	defaultAgencDirname = ".agenc"

	ClaudeDirname              = "claude"
	ClaudeModificationsDirname = "claude-modifications"
	ConfigDirname              = "config"
	UserClaudeDirname          = ".claude"
	MissionsDirname            = "missions"
	ReposDirname               = "repos"
	DaemonDirname              = "daemon"
	DaemonPIDFilename          = "daemon.pid"
	DaemonLogFilename          = "daemon.log"
	DaemonVersionFilename      = "daemon.version"
	ConfigFilename             = "config.yml"

	AgentDirname            = "agent"
	PIDFilename             = "pid"
	GlobalSettingsFilename = "settings.json"
	GlobalClaudeMdFilename  = "CLAUDE.md"
	WrapperLogFilename      = "wrapper.log"
	SettingsLocalFilename   = "settings.local.json"
	HistoryFilename         = "history.jsonl"
	SecretsEnvFilename      = "secrets.env"
	ClaudeOutputLogFilename = "claude-output.log"
	TmuxKeybindingsFilename        = "tmux-keybindings.conf"
	WrapperSocketFilename          = "wrapper.sock"
	StatuslineMessageFilename      = "statusline-message"
	StatuslineOriginalCmdFilename  = "statusline-original-cmd"
	StatuslineWrapperFilename      = "statusline-wrapper.sh"
	AssistantMarkerFilename        = ".assistant"
)

// GetAgencDirpath returns the agenc config directory path, reading from
// the AGENC_DIRPATH environment variable or defaulting to ~/.agenc (configurable).
func GetAgencDirpath() (string, error) {
	if envVal := os.Getenv(agencDirpathEnvVar); envVal != "" {
		return envVal, nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to determine home directory")
	}
	return filepath.Join(homeDir, defaultAgencDirname), nil
}

// EnsureDirStructure creates the required agenc directory structure if it
// doesn't already exist.
func EnsureDirStructure(agencDirpath string) error {
	dirs := []string{
		filepath.Join(agencDirpath, ReposDirname),
		filepath.Join(agencDirpath, ClaudeDirname),
		filepath.Join(agencDirpath, ConfigDirname, ClaudeModificationsDirname),
		filepath.Join(agencDirpath, MissionsDirname),
		filepath.Join(agencDirpath, DaemonDirname),
	}
	for _, dirpath := range dirs {
		if err := os.MkdirAll(dirpath, 0755); err != nil {
			return stacktrace.Propagate(err, "failed to create directory '%s'", dirpath)
		}
	}

	if err := EnsureClaudeModificationsFiles(agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to seed claude-modifications files")
	}

	if err := EnsureConfigFile(agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to seed config file")
	}

	if err := EnsureStatuslineWrapper(agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to ensure statusline wrapper script")
	}

	return nil
}

// GetMissionsDirpath returns the path to the missions directory.
func GetMissionsDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, MissionsDirname)
}

// GetReposDirpath returns the path to the repos directory.
func GetReposDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ReposDirname)
}

// GetGlobalClaudeDirpath returns the path to the global claude config directory.
func GetGlobalClaudeDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ClaudeDirname)
}

// GetDaemonDirpath returns the path to the daemon directory.
func GetDaemonDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, DaemonDirname)
}

// GetDaemonPIDFilepath returns the path to the daemon PID file.
func GetDaemonPIDFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, DaemonDirname, DaemonPIDFilename)
}

// GetDaemonLogFilepath returns the path to the daemon log file.
func GetDaemonLogFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, DaemonDirname, DaemonLogFilename)
}

// GetDaemonVersionFilepath returns the path to the daemon version file.
func GetDaemonVersionFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, DaemonDirname, DaemonVersionFilename)
}

// GetDatabaseFilepath returns the path to the SQLite database file.
func GetDatabaseFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, "database.sqlite")
}

// GetUserClaudeDirpath returns the path to the user's standard Claude config
// directory (~/.claude/).
func GetUserClaudeDirpath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to determine home directory")
	}
	return filepath.Join(homeDir, UserClaudeDirname), nil
}

// GetMissionDirpath returns the path to a specific mission directory.
func GetMissionDirpath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionsDirpath(agencDirpath), missionID)
}

// GetMissionAgentDirpath returns the path to the agent/ subdirectory within
// a mission. This is the Claude Code project root.
func GetMissionAgentDirpath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), AgentDirname)
}

// GetMissionPIDFilepath returns the path to the wrapper PID file for a mission.
func GetMissionPIDFilepath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), PIDFilename)
}

// GetMissionWrapperLogFilepath returns the path to the wrapper log file for
// a mission.
func GetMissionWrapperLogFilepath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), WrapperLogFilename)
}

// GetRepoDirpath returns the path to a specific repo directory.
func GetRepoDirpath(agencDirpath string, repoName string) string {
	return filepath.Join(GetReposDirpath(agencDirpath), repoName)
}

// GetMissionClaudeOutputLogFilepath returns the path to the claude-output.log
// file for a headless mission.
func GetMissionClaudeOutputLogFilepath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), ClaudeOutputLogFilename)
}

// GetConfigDirpath returns the path to the user-editable config directory
// ($AGENC/config/), intended to be Git-controlled.
func GetConfigDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ConfigDirname)
}

// GetHistoryFilepath returns the path to Claude's history.jsonl file, which
// records every user prompt submission.
func GetHistoryFilepath(agencDirpath string) string {
	return filepath.Join(GetGlobalClaudeDirpath(agencDirpath), HistoryFilename)
}

// GetMissionSocketFilepath returns the path to the wrapper unix socket for a mission.
func GetMissionSocketFilepath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), WrapperSocketFilename)
}

// GetTmuxKeybindingsFilepath returns the path to the agenc-managed tmux
// keybindings configuration file.
func GetTmuxKeybindingsFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, TmuxKeybindingsFilename)
}

// GetClaudeModificationsDirpath returns the path to the claude-modifications
// directory where agenc-specific CLAUDE.md and settings.json overrides live.
func GetClaudeModificationsDirpath(agencDirpath string) string {
	return filepath.Join(GetConfigDirpath(agencDirpath), ClaudeModificationsDirname)
}

// GetMissionStatuslineMessageFilepath returns the path to the per-mission
// statusline message file. When non-empty, its contents are displayed
// in the Claude Code statusline instead of the user's original command.
func GetMissionStatuslineMessageFilepath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), StatuslineMessageFilename)
}

// GetStatuslineOriginalCmdFilepath returns the path to the file that stores
// the user's original statusLine.command before it was replaced by our wrapper.
func GetStatuslineOriginalCmdFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, StatuslineOriginalCmdFilename)
}

// GetStatuslineWrapperFilepath returns the path to the shared statusline
// wrapper script.
func GetStatuslineWrapperFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, StatuslineWrapperFilename)
}

// statuslineWrapperScript is the shell script that checks for a per-mission
// message file. If present and non-empty, its contents are displayed.
// Otherwise, the user's original statusLine command (if any) is executed.
const statuslineWrapperScript = `#!/usr/bin/env bash
set -euo pipefail
script_dirpath="$(cd "$(dirname "${0}")" && pwd)"
message_filepath="${1:-}"
input=$(cat)

if [[ -n "${message_filepath}" ]] && [[ -s "${message_filepath}" ]]; then
    cat "${message_filepath}"
    exit 0
fi

original_cmd_filepath="${script_dirpath}/statusline-original-cmd"
if [[ -s "${original_cmd_filepath}" ]]; then
    cmd="$(cat "${original_cmd_filepath}")"
    echo "${input}" | bash -c "${cmd}"
fi
`

// EnsureStatuslineWrapper writes the statusline wrapper script to
// $AGENC_DIRPATH/statusline-wrapper.sh and makes it executable.
// Idempotent: overwrites the script on every call so upgrades are picked up.
func EnsureStatuslineWrapper(agencDirpath string) error {
	wrapperFilepath := GetStatuslineWrapperFilepath(agencDirpath)
	if err := os.WriteFile(wrapperFilepath, []byte(statuslineWrapperScript), 0755); err != nil {
		return stacktrace.Propagate(err, "failed to write statusline wrapper script")
	}
	return nil
}

// GetMissionAssistantMarkerFilepath returns the path to the assistant marker
// file for a mission. The presence of this file indicates the mission is an
// AgenC assistant (not a regular code mission).
func GetMissionAssistantMarkerFilepath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), AssistantMarkerFilename)
}

// IsMissionAssistant reports whether a mission is an AgenC assistant mission
// by checking for the presence of the .assistant marker file.
func IsMissionAssistant(agencDirpath string, missionID string) bool {
	markerFilepath := GetMissionAssistantMarkerFilepath(agencDirpath, missionID)
	_, err := os.Stat(markerFilepath)
	return err == nil
}

// EnsureClaudeModificationsFiles creates seed files inside the
// claude-modifications directory if they don't already exist:
//   - CLAUDE.md (empty, 0 bytes)
//   - settings.json (contains "{}\n")
func EnsureClaudeModificationsFiles(agencDirpath string) error {
	modsDirpath := GetClaudeModificationsDirpath(agencDirpath)

	claudeMdFilepath := filepath.Join(modsDirpath, "CLAUDE.md")
	if _, err := os.Stat(claudeMdFilepath); os.IsNotExist(err) {
		if err := os.WriteFile(claudeMdFilepath, []byte{}, 0644); err != nil {
			return stacktrace.Propagate(err, "failed to create '%s'", claudeMdFilepath)
		}
	}

	settingsFilepath := filepath.Join(modsDirpath, "settings.json")
	if _, err := os.Stat(settingsFilepath); os.IsNotExist(err) {
		if err := os.WriteFile(settingsFilepath, []byte("{}\n"), 0644); err != nil {
			return stacktrace.Propagate(err, "failed to create '%s'", settingsFilepath)
		}
	}

	return nil
}
