package claudeconfig

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/session"
)

const (
	// MissionClaudeConfigDirname is the directory name for per-mission config.
	MissionClaudeConfigDirname = "claude-config"
)

// TrackableItemNames lists the files/directories tracked in the shadow repo
// and copied into per-mission claude config directories.
var TrackableItemNames = []string{
	"CLAUDE.md",
	"settings.json",
	"skills",
	"hooks",
	"commands",
	"agents",
}

// BuildMissionConfigDir creates and populates the per-mission claude config
// directory from the shadow repo. It copies tracked files with path rewriting,
// applies AgenC modifications (merged CLAUDE.md, merged settings.json with
// hooks), copies and patches .claude.json, dumps credentials, and symlinks
// plugins to ~/.claude/plugins.
func BuildMissionConfigDir(agencDirpath string, missionID string, trustedMcpServers *config.TrustedMcpServers) error {
	shadowDirpath := GetShadowRepoDirpath(agencDirpath)
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	claudeConfigDirpath := filepath.Join(missionDirpath, MissionClaudeConfigDirname)
	missionAgentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)

	isAdjutant := config.IsMissionAdjutant(agencDirpath, missionID)

	if err := os.MkdirAll(claudeConfigDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create claude-config directory")
	}

	// Copy tracked directories from shadow repo with path rewriting
	for _, dirName := range TrackedDirNames {
		srcDirpath := filepath.Join(shadowDirpath, dirName)
		dstDirpath := filepath.Join(claudeConfigDirpath, dirName)

		if _, err := os.Stat(srcDirpath); os.IsNotExist(err) {
			// Source doesn't exist — remove destination if it exists
			os.RemoveAll(dstDirpath)
			continue
		}

		// Remove existing destination and copy fresh with path rewriting
		os.RemoveAll(dstDirpath)
		if err := copyDirWithRewriting(srcDirpath, dstDirpath, claudeConfigDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to copy '%s' from shadow repo", dirName)
		}
	}

	agencModsDirpath := config.GetClaudeModificationsDirpath(agencDirpath)

	// CLAUDE.md: merge user content + agenc modifications
	if err := buildMergedClaudeMd(shadowDirpath, agencModsDirpath, claudeConfigDirpath, agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to build merged CLAUDE.md")
	}

	// settings.json: merge user settings + agenc modifications + hooks/deny
	if err := buildMergedSettings(shadowDirpath, agencModsDirpath, claudeConfigDirpath, agencDirpath, missionID); err != nil {
		return stacktrace.Propagate(err, "failed to build merged settings.json")
	}

	// Adjutant missions: write adjutant-specific CLAUDE.md and settings to the
	// agent directory (project-level config), separate from claude-config (global).
	if isAdjutant {
		if err := writeAdjutantAgentConfig(missionAgentDirpath, agencDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to write adjutant agent config")
		}
	}

	// Copy and patch .claude.json with trust entry for mission agent dir
	if err := copyAndPatchClaudeJSON(claudeConfigDirpath, missionAgentDirpath, trustedMcpServers); err != nil {
		return stacktrace.Propagate(err, "failed to copy and patch .claude.json")
	}

	// Symlink directories to ~/.claude/ so all missions share centralized
	// state rather than fragmenting caches, telemetry, and session data.
	symlinkDirNames := []string{
		"plugins",         // IDE plugins
		"projects",        // conversation transcripts, subagent logs, auto-memory
		"shell-snapshots", // Claude Code shell snapshot files
		"statsig",         // Statsig SDK feature flag evaluation cache
		"telemetry",       // first-party telemetry event queue
		"usage-data",      // usage analytics for 'claude usage' reporting
		"todos",           // TodoWrite tool data
		"tasks",           // task tracking data
		"debug",           // debug log files
		"session-env",     // per-session environment snapshots
		"file-history",    // @-mention file index cache
		"cache",           // general cache (changelog, etc.)
		"backups",         // config backup files
		"paste-cache",     // paste buffer cache
	}
	for _, dirName := range symlinkDirNames {
		if err := symlinkToGlobalClaudeDir(claudeConfigDirpath, dirName); err != nil {
			return stacktrace.Propagate(err, "failed to symlink %s", dirName)
		}
	}

	return nil
}

// EnsureShadowRepo ensures the shadow repo is initialized. If it doesn't
// exist, creates it and ingests tracked files from ~/.claude.
func EnsureShadowRepo(agencDirpath string) error {
	shadowDirpath := GetShadowRepoDirpath(agencDirpath)

	// Check if already initialized
	gitDirpath := filepath.Join(shadowDirpath, ".git")
	if _, err := os.Stat(gitDirpath); err == nil {
		return nil
	}

	// Initialize shadow repo
	if _, err := InitShadowRepo(agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to initialize shadow repo")
	}

	// Ingest from ~/.claude
	userClaudeDirpath, err := config.GetUserClaudeDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine ~/.claude path")
	}

	if err := IngestFromClaudeDir(userClaudeDirpath, shadowDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to ingest from ~/.claude into shadow repo")
	}

	return nil
}

// GetShadowRepoCommitHash returns the HEAD commit hash from the shadow repo.
// Returns empty string if the shadow repo doesn't exist or has no commits.
func GetShadowRepoCommitHash(agencDirpath string) string {
	shadowDirpath := GetShadowRepoDirpath(agencDirpath)
	return ResolveConfigCommitHash(shadowDirpath)
}

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

// buildMergedClaudeMd reads user CLAUDE.md from shadow repo and agenc
// modifications, merges them, applies path rewriting, and writes to the
// destination config directory.
func buildMergedClaudeMd(shadowDirpath string, agencModsDirpath string, destDirpath string, agencDirpath string) error {
	destFilepath := filepath.Join(destDirpath, "CLAUDE.md")

	userClaudeContent, err := os.ReadFile(filepath.Join(shadowDirpath, "CLAUDE.md"))
	if err != nil && !os.IsNotExist(err) {
		return stacktrace.Propagate(err, "failed to read user CLAUDE.md from shadow repo")
	}

	modsClaudeContent, err := os.ReadFile(filepath.Join(agencModsDirpath, "CLAUDE.md"))
	if err != nil && !os.IsNotExist(err) {
		return stacktrace.Propagate(err, "failed to read agenc modifications CLAUDE.md")
	}

	mergedClaudeMd := MergeClaudeMd(userClaudeContent, modsClaudeContent)

	if mergedClaudeMd == nil {
		// Both empty — remove destination if it exists
		os.Remove(destFilepath)
		return nil
	}

	// Apply mission-specific path rewriting
	rewrittenBytes := RewriteClaudePaths(mergedClaudeMd, destDirpath)

	return WriteIfChanged(destFilepath, rewrittenBytes)
}

// buildMergedSettings reads user settings from shadow repo and agenc
// modifications, deep-merges them, adds agenc hooks/deny, injects the
// statusline wrapper, then selectively rewrites paths (preserving permission
// entries). Writes to dest.
func buildMergedSettings(shadowDirpath string, agencModsDirpath string, destDirpath string, agencDirpath string, missionID string) error {
	destFilepath := filepath.Join(destDirpath, "settings.json")

	userSettingsData, err := os.ReadFile(filepath.Join(shadowDirpath, "settings.json"))
	if err != nil {
		if os.IsNotExist(err) {
			userSettingsData = []byte("{}")
		} else {
			return stacktrace.Propagate(err, "failed to read user settings from shadow repo")
		}
	}

	modsSettingsData, err := os.ReadFile(filepath.Join(agencModsDirpath, "settings.json"))
	if err != nil {
		if os.IsNotExist(err) {
			modsSettingsData = []byte("{}")
		} else {
			return stacktrace.Propagate(err, "failed to read agenc modifications settings")
		}
	}

	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)
	mergedData, err := MergeSettings(userSettingsData, modsSettingsData, agencDirpath, agentDirpath, destDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to merge settings")
	}

	// Inject the statusline wrapper so per-mission messages override the
	// user's original statusLine command
	mergedData, err = injectStatuslineWrapper(mergedData, agencDirpath, missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to inject statusline wrapper")
	}

	// Selectively rewrite paths: permissions block preserved, everything else rewritten
	rewrittenData, err := RewriteSettingsPaths(mergedData, destDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to rewrite settings paths")
	}

	return WriteIfChanged(destFilepath, rewrittenData)
}

// symlinkToGlobalClaudeDir creates a symlink from claudeConfigDirpath/dirName
// to ~/.claude/dirName, ensuring the target directory exists first. Any
// existing file, directory, or symlink at the link path is removed before
// creating the new symlink.
func symlinkToGlobalClaudeDir(claudeConfigDirpath string, dirName string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine home directory")
	}

	targetDirpath := filepath.Join(homeDir, ".claude", dirName)
	linkPath := filepath.Join(claudeConfigDirpath, dirName)

	// Ensure the target directory exists so Claude Code can write into it
	if err := os.MkdirAll(targetDirpath, 0700); err != nil {
		return stacktrace.Propagate(err, "failed to create '%s'", targetDirpath)
	}

	// Remove existing directory/symlink if it exists
	os.RemoveAll(linkPath)

	return os.Symlink(targetDirpath, linkPath)
}

// copyDirWithRewriting recursively copies a directory tree from src to dst,
// applying ~/.claude path rewriting to text files.
func copyDirWithRewriting(srcDirpath string, dstDirpath string, claudeConfigDirpath string) error {
	return filepath.Walk(srcDirpath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(srcDirpath, path)
		if err != nil {
			return stacktrace.Propagate(err, "failed to compute relative path")
		}

		dstPath := filepath.Join(dstDirpath, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		// Handle symlinks
		if info.Mode()&os.ModeSymlink != 0 {
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return stacktrace.Propagate(err, "failed to read symlink '%s'", path)
			}
			return os.Symlink(linkTarget, dstPath)
		}

		// Regular file — copy contents with optional path rewriting
		data, err := os.ReadFile(path)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read '%s'", path)
		}

		if isTextFile(path) {
			data = RewriteClaudePaths(data, claudeConfigDirpath)
		}

		return os.WriteFile(dstPath, data, info.Mode())
	})
}

// copyAndPatchClaudeJSON copies the user's .claude.json into the mission
// config directory and adds a trust entry for the mission's agent directory.
// Lookup order: ~/.claude/.claude.json (primary), ~/.claude.json (fallback).
// If trustedMcpServers is non-nil, the trust entry also includes
// enabledMcpjsonServers and disabledMcpjsonServers to skip Claude Code's
// MCP consent prompt.
func copyAndPatchClaudeJSON(claudeConfigDirpath string, missionAgentDirpath string, trustedMcpServers *config.TrustedMcpServers) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine home directory")
	}

	// Try primary location: ~/.claude/.claude.json
	primaryFilepath := filepath.Join(homeDir, ".claude", ".claude.json")
	fallbackFilepath := filepath.Join(homeDir, ".claude.json")

	var srcFilepath string
	if _, err := os.Stat(primaryFilepath); err == nil {
		srcFilepath = primaryFilepath
	} else if _, err := os.Stat(fallbackFilepath); err == nil {
		srcFilepath = fallbackFilepath
	} else {
		return stacktrace.NewError(
			".claude.json not found at '%s' or '%s'; run 'claude login' first",
			primaryFilepath, fallbackFilepath,
		)
	}

	data, err := os.ReadFile(srcFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read '%s'", srcFilepath)
	}

	// Parse the JSON
	var claudeJSON map[string]json.RawMessage
	if err := json.Unmarshal(data, &claudeJSON); err != nil {
		return stacktrace.Propagate(err, "failed to parse .claude.json")
	}

	// Get or create the "projects" key
	var projects map[string]json.RawMessage
	if existingProjects, ok := claudeJSON["projects"]; ok {
		if err := json.Unmarshal(existingProjects, &projects); err != nil {
			return stacktrace.Propagate(err, "failed to parse projects in .claude.json")
		}
	} else {
		projects = make(map[string]json.RawMessage)
	}

	// Build trust entry for the mission agent directory
	trustEntry := map[string]interface{}{
		"hasTrustDialogAccepted": true,
	}
	if trustedMcpServers != nil {
		if trustedMcpServers.All {
			trustEntry["enabledMcpjsonServers"] = []string{}
			trustEntry["disabledMcpjsonServers"] = []string{}
		} else {
			trustEntry["enabledMcpjsonServers"] = trustedMcpServers.List
			trustEntry["disabledMcpjsonServers"] = []string{}
		}
	}
	trustEntryData, err := json.Marshal(trustEntry)
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal trust entry")
	}
	projects[missionAgentDirpath] = json.RawMessage(trustEntryData)

	// Write projects back
	projectsData, err := json.Marshal(projects)
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal projects")
	}
	claudeJSON["projects"] = json.RawMessage(projectsData)

	// Serialize with indentation
	result, err := json.MarshalIndent(claudeJSON, "", "  ")
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal .claude.json")
	}
	result = append(result, '\n')

	destFilepath := filepath.Join(claudeConfigDirpath, ".claude.json")
	if err := os.WriteFile(destFilepath, result, 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write '%s'", destFilepath)
	}

	return nil
}

// ComputeCredentialServiceName returns the macOS Keychain service name for a
// per-mission credential entry. The name is "Claude Code-credentials-<hash>"
// where <hash> is the first 8 hex characters of the SHA-256 of the
// claudeConfigDirpath. Claude Code uses this naming convention when
// CLAUDE_CONFIG_DIR is set to a non-default path.
func ComputeCredentialServiceName(claudeConfigDirpath string) string {
	hash := sha256.Sum256([]byte(claudeConfigDirpath))
	hashPrefix := hex.EncodeToString(hash[:])[:8]
	return "Claude Code-credentials-" + hashPrefix
}

// GlobalCredentialServiceName is the macOS Keychain service name for the
// global Claude Code credential entry (used when CLAUDE_CONFIG_DIR is unset).
const GlobalCredentialServiceName = "Claude Code-credentials"

// ReadKeychainCredentials reads the credential blob from the macOS Keychain
// entry with the given service name. Returns the raw credential string (trimmed)
// or an error if the entry does not exist or cannot be read.
func ReadKeychainCredentials(serviceName string) (string, error) {
	user := os.Getenv("USER")
	if user == "" {
		return "", stacktrace.NewError("USER environment variable not set")
	}

	readCmd := exec.Command("security", "find-generic-password", "-a", user, "-w", "-s", serviceName)
	credentialData, err := readCmd.Output()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read Keychain entry for service '%s'", serviceName)
	}

	return strings.TrimSpace(string(credentialData)), nil
}

// WriteKeychainCredentials writes (or replaces) a credential blob in the macOS
// Keychain entry with the given service name. Any existing entry is deleted
// first to allow an idempotent overwrite.
func WriteKeychainCredentials(serviceName string, credential string) error {
	user := os.Getenv("USER")
	if user == "" {
		return stacktrace.NewError("USER environment variable not set")
	}

	// Delete any existing entry (ignore errors — may not exist)
	deleteCmd := exec.Command("security", "delete-generic-password", "-a", user, "-s", serviceName)
	_ = deleteCmd.Run()

	addCmd := exec.Command("security", "add-generic-password", "-a", user, "-s", serviceName, "-w", credential)
	if err := addCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to write credentials to Keychain service '%s'", serviceName)
	}

	return nil
}

// CloneKeychainCredentials reads the user's credentials from the default macOS
// Keychain entry ("Claude Code-credentials") and clones them into a per-mission
// entry keyed by claudeConfigDirpath. This avoids writing credential files to disk.
func CloneKeychainCredentials(claudeConfigDirpath string) error {
	credential, err := ReadKeychainCredentials(GlobalCredentialServiceName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read credentials from Keychain; run 'claude login' first")
	}

	targetService := ComputeCredentialServiceName(claudeConfigDirpath)
	if err := WriteKeychainCredentials(targetService, credential); err != nil {
		return stacktrace.Propagate(err, "failed to clone credentials to per-mission Keychain entry")
	}

	return nil
}

// WriteBackKeychainCredentials merges per-mission Keychain credentials back
// into the global entry. This propagates MCP OAuth tokens acquired during a
// mission so that subsequent missions inherit them without re-authentication.
//
// The merge uses MergeCredentialJSON: top-level keys are replaced by the
// per-mission overlay, and mcpOAuth entries are merged per-server using
// expiresAt to keep the newest token. If either side fails to parse or read,
// the function returns nil (non-fatal) to avoid blocking wrapper exit.
func WriteBackKeychainCredentials(claudeConfigDirpath string) error {
	missionService := ComputeCredentialServiceName(claudeConfigDirpath)

	missionCred, err := ReadKeychainCredentials(missionService)
	if err != nil {
		// Per-mission entry may not exist (e.g. mission never ran Claude)
		return nil
	}

	globalCred, err := ReadKeychainCredentials(GlobalCredentialServiceName)
	if err != nil {
		// Global entry missing — nothing to merge into
		return nil
	}

	merged, changed, err := MergeCredentialJSON([]byte(globalCred), []byte(missionCred))
	if err != nil {
		// JSON parse failure — skip silently
		return nil
	}

	if !changed {
		return nil
	}

	if err := WriteKeychainCredentials(GlobalCredentialServiceName, string(merged)); err != nil {
		return stacktrace.Propagate(err, "failed to write merged credentials back to global Keychain entry")
	}

	return nil
}

// DeleteKeychainCredentials removes the per-mission Keychain credential entry.
// Errors are silently ignored if the entry does not exist (idempotent cleanup).
func DeleteKeychainCredentials(claudeConfigDirpath string) error {
	user := os.Getenv("USER")
	if user == "" {
		return stacktrace.NewError("USER environment variable not set")
	}

	targetService := ComputeCredentialServiceName(claudeConfigDirpath)

	deleteCmd := exec.Command("security", "delete-generic-password", "-a", user, "-s", targetService)
	output, err := deleteCmd.CombinedOutput()
	if err != nil {
		// Ignore "item not found" errors — the entry may not exist
		if strings.Contains(string(output), "SecKeychainSearchCopyNext") {
			return nil
		}
		return stacktrace.Propagate(err, "failed to delete Keychain credentials for service '%s'", targetService)
	}

	return nil
}

// CountCommitsBehind returns the number of commits between missionCommitHash
// and HEAD in the shadow repo. Returns 0 if the hashes are equal or if the
// shadow repo has no commits. Returns -1 if the mission commit is not found
// in the shadow repo (e.g., after repo recreation).
func CountCommitsBehind(agencDirpath string, missionCommitHash string, headCommitHash string) int {
	if missionCommitHash == headCommitHash {
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitOperationTimeout)
	defer cancel()

	shadowDirpath := GetShadowRepoDirpath(agencDirpath)
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--count", missionCommitHash+".."+headCommitHash)
	cmd.Dir = shadowDirpath
	output, err := cmd.Output()
	if err != nil {
		return -1
	}

	countStr := strings.TrimSpace(string(output))
	count := 0
	for _, ch := range countStr {
		if ch < '0' || ch > '9' {
			return -1
		}
		count = count*10 + int(ch-'0')
	}
	return count
}

// ResolveConfigCommitHash returns the HEAD commit hash from the git repo
// containing the config source directory. Returns empty string if not a git repo.
func ResolveConfigCommitHash(configSourceDirpath string) string {
	repoRootDirpath := findGitRoot(configSourceDirpath)
	if repoRootDirpath == "" {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), gitOperationTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	cmd.Dir = repoRootDirpath
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

// injectStatuslineWrapper modifies the merged settings JSON to replace any
// existing statusLine.command with our wrapper script. The user's original
// command is saved to a file so the wrapper can delegate to it when there is
// no per-mission message to display.
func injectStatuslineWrapper(settingsData []byte, agencDirpath string, missionID string) ([]byte, error) {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse settings JSON for statusline injection")
	}

	wrapperFilepath := config.GetStatuslineWrapperFilepath(agencDirpath)
	messageFilepath := config.GetMissionStatuslineMessageFilepath(agencDirpath, missionID)
	originalCmdFilepath := config.GetStatuslineOriginalCmdFilepath(agencDirpath)

	// Extract existing statusLine.command, if any, and save it
	if statusLineRaw, ok := settings["statusLine"]; ok {
		var statusLine map[string]json.RawMessage
		if err := json.Unmarshal(statusLineRaw, &statusLine); err == nil {
			if cmdRaw, ok := statusLine["command"]; ok {
				var existingCmd string
				if err := json.Unmarshal(cmdRaw, &existingCmd); err == nil {
					// Only save the original command if it's not already our wrapper
					if !strings.HasPrefix(existingCmd, wrapperFilepath) {
						if err := os.WriteFile(originalCmdFilepath, []byte(existingCmd), 0644); err != nil {
							return nil, stacktrace.Propagate(err, "failed to save original statusLine command")
						}
					}
				}
			}
		}
	}

	// Build our wrapper command: <wrapper-path> <mission-message-filepath>
	wrapperCmd := wrapperFilepath + " " + messageFilepath

	statusLineObj := map[string]string{
		"type":    "command",
		"command": wrapperCmd,
	}
	statusLineData, err := json.Marshal(statusLineObj)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal statusLine object")
	}
	settings["statusLine"] = json.RawMessage(statusLineData)

	result, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal settings with statusline wrapper")
	}
	result = append(result, '\n')

	return result, nil
}

// ExtractExpiresAtFromJSON extracts the claudeAiOauth.expiresAt timestamp
// from a credential JSON blob and returns it as Unix seconds. Returns 0 if
// the field is missing or unparseable.
//
// Claude Code stores expiresAt as epoch milliseconds; this function normalizes
// to seconds so callers can compare directly against time.Now().Unix().
func ExtractExpiresAtFromJSON(credentialJSON []byte) float64 {
	var credMap map[string]json.RawMessage
	if err := json.Unmarshal(credentialJSON, &credMap); err != nil {
		return 0
	}

	oauthRaw, ok := credMap["claudeAiOauth"]
	if !ok {
		return 0
	}

	expiresAt := extractExpiresAt(oauthRaw)
	if expiresAt == 0 {
		return 0
	}

	// Claude Code stores expiresAt in milliseconds. Normalize to seconds
	// by checking magnitude: any value above 1e12 is clearly milliseconds
	// (year ~33658 in seconds vs year ~2001 in milliseconds).
	if expiresAt > 1e12 {
		expiresAt /= 1000
	}

	return expiresAt
}

// GetCredentialExpiresAt reads the global Keychain credentials and returns
// the expiresAt timestamp (in Unix seconds) for the claudeAiOauth token.
// Returns 0 if the credential cannot be read or the expiresAt field is missing.
func GetCredentialExpiresAt() float64 {
	credential, err := ReadKeychainCredentials(GlobalCredentialServiceName)
	if err != nil {
		return 0
	}
	return ExtractExpiresAtFromJSON([]byte(credential))
}

// GetLastSessionID returns the most recent session ID for a mission by
// scanning the filesystem for JSONL session files sorted by modification
// time (most recent first). This is reliable for both active and completed
// sessions, unlike .claude.json's lastSessionId field which is only
// populated at session close.
// Returns empty string if no session is found.
func GetLastSessionID(agencDirpath string, missionID string) string {
	claudeConfigDirpath := filepath.Join(
		config.GetMissionDirpath(agencDirpath, missionID),
		MissionClaudeConfigDirname,
	)

	sessionIDs := session.ListSessionIDs(claudeConfigDirpath, missionID)
	if len(sessionIDs) > 0 {
		return sessionIDs[0]
	}

	return ""
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

// findGitRoot walks up from the given path looking for a .git directory.
// Returns the repo root path, or empty string if not found.
func findGitRoot(startPath string) string {
	path := startPath
	for {
		gitDirpath := filepath.Join(path, ".git")
		if _, err := os.Stat(gitDirpath); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}
