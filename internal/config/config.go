package config

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mattn/go-isatty"
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
	ServerDirname              = "server"
	ServerPIDFilename          = "server.pid"
	ServerLogFilename          = "server.log"
	ServerSocketFilename       = "server.sock"
	ConfigFilename             = "config.yml"

	AgentDirname                    = "agent"
	PIDFilename                     = "pid"
	GlobalSettingsFilename          = "settings.json"
	GlobalClaudeMdFilename          = "CLAUDE.md"
	WrapperLogFilename              = "wrapper.log"
	SettingsLocalFilename           = "settings.local.json"
	HistoryFilename                 = "history.jsonl"
	SecretsEnvFilename              = "secrets.env"
	ClaudeOutputLogFilename         = "claude-output.log"
	TmuxKeybindingsFilename         = "tmux-keybindings.conf"
	WrapperSocketFilename           = "wrapper.sock"
	StatuslineMessageFilename       = "statusline-message"
	StatuslineOriginalCmdFilename   = "statusline-original-cmd"
	StatuslineWrapperFilename       = "statusline-wrapper.sh"
	MissionUUIDEnvVar               = "AGENC_MISSION_UUID"
	AdjutantMarkerFilename          = ".adjutant"
	GlobalCredentialsExpiryFilename = "global-credentials-expiry"
	CacheDirname                    = "cache"
	OAuthTokenFilename              = "oauth-token"
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
	// Create directories that are always needed (local-only, not part of the
	// config repo). The config/ directory is intentionally excluded — it is
	// created only by cloning the user's config repo.
	dirs := []string{
		filepath.Join(agencDirpath, ReposDirname),
		filepath.Join(agencDirpath, ClaudeDirname),
		filepath.Join(agencDirpath, MissionsDirname),
		filepath.Join(agencDirpath, DaemonDirname),
		filepath.Join(agencDirpath, ServerDirname),
		filepath.Join(agencDirpath, CacheDirname),
	}
	for _, dirpath := range dirs {
		if err := os.MkdirAll(dirpath, 0755); err != nil {
			return stacktrace.Propagate(err, "failed to create directory '%s'", dirpath)
		}
	}

	// Seed files inside the config directory only if it already exists
	// (i.e., a config repo was previously cloned).
	configDirpath := filepath.Join(agencDirpath, ConfigDirname)
	if _, err := os.Stat(configDirpath); err == nil {
		if err := EnsureClaudeModificationsFiles(agencDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to seed claude-modifications files")
		}

		if err := EnsureConfigFile(agencDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to seed config file")
		}
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

// GetServerDirpath returns the path to the server directory.
func GetServerDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ServerDirname)
}

// GetServerPIDFilepath returns the path to the server PID file.
func GetServerPIDFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ServerDirname, ServerPIDFilename)
}

// GetServerLogFilepath returns the path to the server log file.
func GetServerLogFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ServerDirname, ServerLogFilename)
}

// GetServerSocketFilepath returns the path to the server unix socket file.
func GetServerSocketFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, ServerDirname, ServerSocketFilename)
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

// GetGlobalCredentialsExpiryFilepath returns the path to the shared file that
// broadcasts the current global credential expiry timestamp. Wrappers write
// this file after propagating fresh credentials to the global Keychain;
// other wrappers watch it via fsnotify to detect credential changes.
func GetGlobalCredentialsExpiryFilepath(agencDirpath string) string {
	return filepath.Join(agencDirpath, GlobalCredentialsExpiryFilename)
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

// GetMissionAdjutantMarkerFilepath returns the path to the adjutant marker
// file for a mission. The presence of this file indicates the mission is an
// adjutant mission (not a regular code mission).
func GetMissionAdjutantMarkerFilepath(agencDirpath string, missionID string) string {
	return filepath.Join(GetMissionDirpath(agencDirpath, missionID), AdjutantMarkerFilename)
}

// IsMissionAdjutant reports whether a mission is an adjutant mission
// by checking for the presence of the adjutant marker file.
//
// Backward compatibility: Checks both new .adjutant and legacy .assistant markers.
// The legacy .assistant check will be removed in v2.0.0.
func IsMissionAdjutant(agencDirpath string, missionID string) bool {
	missionDirpath := GetMissionDirpath(agencDirpath, missionID)

	// Check new .adjutant marker first (preferred)
	newMarkerFilepath := filepath.Join(missionDirpath, ".adjutant")
	if _, err := os.Stat(newMarkerFilepath); err == nil {
		return true
	}

	// Backward compatibility: check old .assistant marker
	// DEPRECATED: Remove in v2.0.0
	oldMarkerFilepath := filepath.Join(missionDirpath, ".assistant")
	if _, err := os.Stat(oldMarkerFilepath); err == nil {
		return true
	}

	return false
}

// MigrateAssistantMarkerIfNeeded checks if a mission has the old .assistant
// marker file and migrates it to the new .adjutant marker.
//
// This provides automatic migration for missions created before the rename.
// DEPRECATED: Remove in v2.0.0 when backward compatibility is dropped.
func MigrateAssistantMarkerIfNeeded(agencDirpath string, missionID string) error {
	missionDirpath := GetMissionDirpath(agencDirpath, missionID)
	oldMarker := filepath.Join(missionDirpath, ".assistant")

	// Check if old marker exists
	if _, err := os.Stat(oldMarker); err != nil {
		return nil // No old marker, nothing to migrate
	}

	// Create new marker
	newMarker := filepath.Join(missionDirpath, ".adjutant")
	if err := os.WriteFile(newMarker, []byte{}, 0644); err != nil {
		return stacktrace.Propagate(err, "failed to create new adjutant marker file")
	}

	// Remove old marker
	if err := os.Remove(oldMarker); err != nil {
		return stacktrace.Propagate(err, "failed to remove old assistant marker file")
	}

	return nil
}

// EnsureClaudeModificationsFiles creates seed files inside the
// claude-modifications directory if they don't already exist:
//   - CLAUDE.md (empty, 0 bytes)
//   - settings.json (contains "{}\n")
func EnsureClaudeModificationsFiles(agencDirpath string) error {
	modsDirpath := GetClaudeModificationsDirpath(agencDirpath)

	if err := os.MkdirAll(modsDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create directory '%s'", modsDirpath)
	}

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

// GetCacheDirpath returns the path to the cache directory ($AGENC_DIRPATH/cache/).
func GetCacheDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, CacheDirname)
}

// GetOAuthTokenFilepath returns the path to the cached OAuth token file.
func GetOAuthTokenFilepath(agencDirpath string) string {
	return filepath.Join(GetCacheDirpath(agencDirpath), OAuthTokenFilename)
}

// GetCronLogDirpath returns the path to the cron logs directory.
func GetCronLogDirpath(agencDirpath string) string {
	return filepath.Join(agencDirpath, "logs", "crons")
}

// GetCronLogFilepaths returns stdout and stderr log paths for a cron job.
// The cron name must already be validated (alphanumeric, dash, underscore only).
func GetCronLogFilepaths(agencDirpath string, cronName string) (stdout, stderr string) {
	logDir := GetCronLogDirpath(agencDirpath)
	stdout = filepath.Join(logDir, fmt.Sprintf("%s-stdout.log", cronName))
	stderr = filepath.Join(logDir, fmt.Sprintf("%s-stderr.log", cronName))
	return
}

// ReadOAuthToken reads the OAuth token from the cache file. Returns an empty
// string if the file does not exist. Returns an error only for unexpected
// read failures.
func ReadOAuthToken(agencDirpath string) (string, error) {
	tokenFilepath := GetOAuthTokenFilepath(agencDirpath)
	data, err := os.ReadFile(tokenFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", stacktrace.Propagate(err, "failed to read OAuth token file '%s'", tokenFilepath)
	}
	return strings.TrimSpace(string(data)), nil
}

// WriteOAuthToken writes the OAuth token to the cache file with 600 permissions.
// If the token is empty, the file is deleted instead.
func WriteOAuthToken(agencDirpath string, token string) error {
	tokenFilepath := GetOAuthTokenFilepath(agencDirpath)

	if token == "" {
		if err := os.Remove(tokenFilepath); err != nil && !os.IsNotExist(err) {
			return stacktrace.Propagate(err, "failed to remove OAuth token file '%s'", tokenFilepath)
		}
		return nil
	}

	// Ensure cache directory exists
	cacheDirpath := GetCacheDirpath(agencDirpath)
	if err := os.MkdirAll(cacheDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create cache directory '%s'", cacheDirpath)
	}

	if err := os.WriteFile(tokenFilepath, []byte(token+"\n"), 0600); err != nil {
		return stacktrace.Propagate(err, "failed to write OAuth token file '%s'", tokenFilepath)
	}

	return nil
}

// oauthTokenPrefix is the expected prefix for valid Claude Code OAuth tokens.
const oauthTokenPrefix = "sk-ant-"

// cleanupOldAuthFiles previously removed files from the Keychain-based auth
// system during migration to token-file auth. It is now a no-op.
func cleanupOldAuthFiles(_ string) {
	// The global-credentials-expiry file is now used as a broadcast signal for
	// MCP credential downward sync across missions. Do not delete it.
	//
	// Note: Per-mission Keychain entries ("Claude Code-credentials-<hash>") are
	// cleaned up automatically when missions are removed via `agenc mission rm`.
	// Old entries from before this cleanup was added are harmless and left in place.
}

// SetupOAuthToken walks the user through obtaining a long-lived Claude Code
// OAuth token via `claude setup-token`. If a token file already exists, it
// returns nil without overwriting. Requires a TTY on stdin — returns an error
// with manual setup instructions if no TTY is available (e.g. headless mode).
func SetupOAuthToken(agencDirpath string) error {
	// Clean up old Keychain-based auth files from previous AgenC versions
	cleanupOldAuthFiles(agencDirpath)

	existing, err := ReadOAuthToken(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to check existing OAuth token")
	}
	if existing != "" {
		return nil // Already have a token — don't overwrite
	}

	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return stacktrace.NewError(
			"no OAuth token configured and no TTY available for interactive setup\n\n" +
				"To set up authentication:\n" +
				"  1. Run: claude setup-token\n" +
				"  2. Copy the token (starts with sk-ant-)\n" +
				"  3. Run: agenc config set claudeCodeOAuthToken <token>",
		)
	}

	claudeBinary, err := exec.LookPath("claude")
	if err != nil {
		return stacktrace.Propagate(err, "'claude' binary not found in PATH")
	}

	reader := bufio.NewReader(os.Stdin)

	// Step 1: Explain why and get confirmation
	fmt.Println()
	fmt.Println("Claude Code Token Setup")
	fmt.Println("-----------------------")
	fmt.Println("AgenC needs a long-lived Claude Code token. Standard OAuth tokens")
	fmt.Println("don't work well with multiple concurrent sessions:")
	fmt.Println("  https://github.com/anthropics/claude-code/issues/24317")
	fmt.Println()
	fmt.Print("Set up token now? [Y/n] ")
	answer, err := reader.ReadString('\n')
	if err != nil {
		return stacktrace.Propagate(err, "failed to read input")
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "n" || answer == "no" {
		return stacktrace.NewError(
			"OAuth token setup skipped; to set one manually:\n" +
				"  1. Run: claude setup-token\n" +
				"  2. Copy the token (starts with sk-ant-)\n" +
				"  3. Run: agenc config set claudeCodeOAuthToken <token>",
		)
	}

	// Step 2: Explain what's about to happen and wait for confirmation
	fmt.Println()
	fmt.Println("We're going to run 'claude setup-token'. This will open your browser.")
	fmt.Println("After authorizing, copy the token that gets printed (starts with sk-ant-).")
	fmt.Println()
	fmt.Print("Press ENTER when ready...")
	if _, err := reader.ReadString('\n'); err != nil {
		return stacktrace.Propagate(err, "failed to read input")
	}
	fmt.Println()

	cmd := exec.Command(claudeBinary, "setup-token")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return stacktrace.Propagate(err, "'claude setup-token' failed")
	}

	// Step 3: Ask for the token in a validation loop
	fmt.Println()
	for {
		fmt.Print("Paste the token here (starts with sk-ant-): ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return stacktrace.Propagate(err, "failed to read input")
		}
		token := strings.TrimSpace(input)

		if token == "" {
			fmt.Println("No token entered. Please try again.")
			continue
		}
		if !strings.HasPrefix(token, oauthTokenPrefix) {
			fmt.Printf("That doesn't look right — expected a token starting with %q. Please try again.\n", oauthTokenPrefix)
			continue
		}

		if err := WriteOAuthToken(agencDirpath, token); err != nil {
			return stacktrace.Propagate(err, "failed to save OAuth token")
		}

		// Step 4: Confirm and show how to update later
		fmt.Println()
		fmt.Println("Token saved successfully.")
		fmt.Println()
		fmt.Println("To update this token later:")
		fmt.Println("  agenc config set claudeCodeOAuthToken <new-token>")
		return nil
	}
}
