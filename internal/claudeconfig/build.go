package claudeconfig

import (
	"context"
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
			_ = os.RemoveAll(dstDirpath)
			continue
		}

		// Remove existing destination and copy fresh with path rewriting
		_ = os.RemoveAll(dstDirpath)
		if err := copyDirWithRewriting(srcDirpath, dstDirpath, claudeConfigDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to copy '%s' from shadow repo", dirName)
		}
	}

	agencModsDirpath := config.GetClaudeModificationsDirpath(agencDirpath)

	// CLAUDE.md: merge user content + agenc modifications
	if err := buildMergedClaudeMd(shadowDirpath, agencModsDirpath, claudeConfigDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to build merged CLAUDE.md")
	}

	// AgenC-managed hook scripts (PreToolUse repo-library guard, etc.).
	if err := WriteAgencHookScripts(claudeConfigDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to write agenc hook scripts")
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

	// Directories that link to ~/.claude/ for shared state across missions.
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

	// Symlink to ~/.claude/ so all missions share centralized state rather than
	// fragmenting caches, telemetry, and session data.
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

// buildMergedClaudeMd assembles the final CLAUDE.md for a mission by combining
// the user's two CLAUDE.md sources:
//  1. User's CLAUDE.md (from the shadow repo / ~/.claude)
//  2. User's claude-modifications CLAUDE.md (from ~/.agenc/config/claude-modifications)
//
// AgenC operating context is no longer prepended here — it's delivered to the
// agent via a SessionStart hook that runs `agenc prime` (see overrides.go).
// Path rewriting resolves `~/.claude/...` references in user content into the
// per-mission snapshot.
func buildMergedClaudeMd(shadowDirpath string, agencModsDirpath string, destDirpath string) error {
	destFilepath := filepath.Join(destDirpath, "CLAUDE.md")

	userClaudeContent, err := os.ReadFile(filepath.Join(shadowDirpath, "CLAUDE.md"))
	if err != nil && !os.IsNotExist(err) {
		return stacktrace.Propagate(err, "failed to read user CLAUDE.md from shadow repo")
	}

	modsClaudeContent, err := os.ReadFile(filepath.Join(agencModsDirpath, "CLAUDE.md"))
	if err != nil && !os.IsNotExist(err) {
		return stacktrace.Propagate(err, "failed to read agenc modifications CLAUDE.md")
	}

	mergedUserContent := MergeClaudeMd(userClaudeContent, modsClaudeContent)
	mergedClaudeMd := RewriteClaudePaths(mergedUserContent, destDirpath)

	if mergedClaudeMd == nil {
		_ = os.Remove(destFilepath)
		return nil
	}

	return WriteIfChanged(destFilepath, mergedClaudeMd)
}

// buildMergedSettings reads user settings from shadow repo and agenc
// modifications, deep-merges them, adds agenc hooks/deny, then selectively
// rewrites paths (preserving permission entries). Writes to dest.
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
	_ = os.RemoveAll(linkPath)

	return os.Symlink(targetDirpath, linkPath)
}

// WriteAgencHookScripts writes the AgenC-managed hook scripts into the
// per-mission claude-config snapshot. Hook scripts are owned by AgenC and
// must not be modified by the agent — `agenc-hooks/` is included in
// claudeConfigProtectedItems so the deny entries cover it.
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
