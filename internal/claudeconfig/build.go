package claudeconfig

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
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
// directory from the shadow repo. It copies tracked files with path expansion,
// applies AgenC modifications (merged CLAUDE.md, merged settings.json with
// hooks), symlinks auth files, and symlinks plugins to ~/.claude/plugins.
func BuildMissionConfigDir(agencDirpath string, missionID string) error {
	shadowDirpath := GetShadowRepoDirpath(agencDirpath)
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	claudeConfigDirpath := filepath.Join(missionDirpath, MissionClaudeConfigDirname)

	if err := os.MkdirAll(claudeConfigDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create claude-config directory")
	}

	// Copy tracked directories from shadow repo with path expansion
	for _, dirName := range TrackedDirNames {
		srcDirpath := filepath.Join(shadowDirpath, dirName)
		dstDirpath := filepath.Join(claudeConfigDirpath, dirName)

		if _, err := os.Stat(srcDirpath); os.IsNotExist(err) {
			// Source doesn't exist — remove destination if it exists
			os.RemoveAll(dstDirpath)
			continue
		}

		// Remove existing destination and copy fresh with path expansion
		os.RemoveAll(dstDirpath)
		if err := copyDirWithExpansion(srcDirpath, dstDirpath, claudeConfigDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to copy '%s' from shadow repo", dirName)
		}
	}

	// Merge CLAUDE.md: user's from shadow repo (expanded) + agenc modifications
	agencModsDirpath := config.GetClaudeModificationsDirpath(agencDirpath)
	if err := buildMergedClaudeMd(shadowDirpath, agencModsDirpath, claudeConfigDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to build merged CLAUDE.md")
	}

	// Merge settings.json: user's from shadow repo (expanded) + agenc modifications + hooks/deny
	if err := buildMergedSettings(shadowDirpath, agencModsDirpath, claudeConfigDirpath, agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to build merged settings.json")
	}

	// Symlink .claude.json (account identity)
	if err := symlinkClaudeJSON(claudeConfigDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to symlink .claude.json")
	}

	// Dump credentials directly into the mission config directory
	if err := writeCredentialsToDir(claudeConfigDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to write .credentials.json")
	}

	// Symlink plugins to ~/.claude/plugins
	if err := symlinkPlugins(claudeConfigDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to symlink plugins")
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
// modifications, applies path expansion, merges them, and writes to the
// destination config directory.
func buildMergedClaudeMd(shadowDirpath string, agencModsDirpath string, destDirpath string) error {
	destFilepath := filepath.Join(destDirpath, "CLAUDE.md")

	userContent, err := os.ReadFile(filepath.Join(shadowDirpath, "CLAUDE.md"))
	if err != nil && !os.IsNotExist(err) {
		return stacktrace.Propagate(err, "failed to read user CLAUDE.md from shadow repo")
	}

	// Expand ${CLAUDE_CONFIG_DIR} → actual mission config path
	if userContent != nil {
		userContent = ExpandPaths(userContent, destDirpath)
	}

	modsContent, err := os.ReadFile(filepath.Join(agencModsDirpath, "CLAUDE.md"))
	if err != nil && !os.IsNotExist(err) {
		return stacktrace.Propagate(err, "failed to read agenc modifications CLAUDE.md")
	}

	mergedBytes := MergeClaudeMd(userContent, modsContent)
	if mergedBytes == nil {
		// Both empty — remove destination if it exists
		os.Remove(destFilepath)
		return nil
	}

	return WriteIfChanged(destFilepath, mergedBytes)
}

// buildMergedSettings reads user settings from shadow repo and agenc
// modifications, deep-merges them, adds agenc hooks/deny, and writes to dest.
// No path expansion is applied — settings.json contains permission entries
// with user-specified paths that must not be rewritten.
func buildMergedSettings(shadowDirpath string, agencModsDirpath string, destDirpath string, agencDirpath string) error {
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

	mergedData, err := MergeSettings(userSettingsData, modsSettingsData, agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to merge settings")
	}

	return WriteIfChanged(destFilepath, mergedData)
}

// symlinkPlugins creates a symlink from the mission config's plugins/
// directory to ~/.claude/plugins/. If ~/.claude/plugins/ doesn't exist,
// the symlink is still created (it will resolve when the user installs a plugin).
func symlinkPlugins(claudeConfigDirpath string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine home directory")
	}

	pluginsTargetDirpath := filepath.Join(homeDir, ".claude", "plugins")
	pluginsLinkPath := filepath.Join(claudeConfigDirpath, "plugins")

	// Remove existing plugins directory/file if it exists
	os.RemoveAll(pluginsLinkPath)

	return os.Symlink(pluginsTargetDirpath, pluginsLinkPath)
}

// copyDirWithExpansion recursively copies a directory tree from src to dst,
// applying ${CLAUDE_CONFIG_DIR} path expansion to text files.
func copyDirWithExpansion(srcDirpath string, dstDirpath string, claudeConfigDirpath string) error {
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

		// Regular file — copy contents with optional path expansion
		data, err := os.ReadFile(path)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read '%s'", path)
		}

		if isTextFile(path) {
			data = ExpandPaths(data, claudeConfigDirpath)
		}

		return os.WriteFile(dstPath, data, info.Mode())
	})
}

// symlinkClaudeJSON creates a symlink from the mission config's .claude.json
// to the user's existing .claude.json file.
// Lookup order: ~/.claude/.claude.json (primary), ~/.claude.json (fallback).
func symlinkClaudeJSON(claudeConfigDirpath string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine home directory")
	}

	// Try primary location: ~/.claude/.claude.json
	primaryPath := filepath.Join(homeDir, ".claude", ".claude.json")
	fallbackPath := filepath.Join(homeDir, ".claude.json")

	var targetPath string
	if _, err := os.Stat(primaryPath); err == nil {
		targetPath = primaryPath
	} else if _, err := os.Stat(fallbackPath); err == nil {
		targetPath = fallbackPath
	} else {
		return stacktrace.NewError(
			".claude.json not found at '%s' or '%s'; run 'claude login' first",
			primaryPath, fallbackPath,
		)
	}

	linkPath := filepath.Join(claudeConfigDirpath, ".claude.json")
	return ensureSymlink(linkPath, targetPath)
}

// writeCredentialsToDir dumps credentials directly into the given directory
// as a real file (not a symlink). On macOS this reads from Keychain; on Linux
// it copies from ~/.claude/.credentials.json.
func writeCredentialsToDir(claudeConfigDirpath string) error {
	credentialsFilepath := filepath.Join(claudeConfigDirpath, ".credentials.json")
	return dumpCredentials(credentialsFilepath)
}

// dumpCredentials dumps auth credentials to the specified file.
// On macOS, reads from Keychain. On Linux, copies from ~/.claude/.credentials.json.
func dumpCredentials(destFilepath string) error {
	if runtime.GOOS == "darwin" {
		return dumpCredentialsFromKeychain(destFilepath)
	}
	return dumpCredentialsFromFile(destFilepath)
}

// dumpCredentialsFromKeychain reads credentials from macOS Keychain.
func dumpCredentialsFromKeychain(destFilepath string) error {
	user := os.Getenv("USER")
	if user == "" {
		return stacktrace.NewError("USER environment variable not set")
	}

	cmd := exec.Command("security", "find-generic-password", "-a", user, "-w", "-s", "Claude Code-credentials")
	output, err := cmd.Output()
	if err != nil {
		return stacktrace.NewError(
			"failed to read credentials from Keychain; run 'claude login' first (error: %v)", err,
		)
	}

	if err := os.WriteFile(destFilepath, output, 0600); err != nil {
		return stacktrace.Propagate(err, "failed to write credentials to '%s'", destFilepath)
	}

	return nil
}

// dumpCredentialsFromFile copies credentials from ~/.claude/.credentials.json (Linux).
func dumpCredentialsFromFile(destFilepath string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine home directory")
	}

	srcFilepath := filepath.Join(homeDir, ".claude", ".credentials.json")
	data, err := os.ReadFile(srcFilepath)
	if err != nil {
		return stacktrace.NewError(
			"credentials not found at '%s'; run 'claude login' first", srcFilepath,
		)
	}

	if err := os.WriteFile(destFilepath, data, 0600); err != nil {
		return stacktrace.Propagate(err, "failed to write credentials to '%s'", destFilepath)
	}

	return nil
}

// ResolveConfigCommitHash returns the HEAD commit hash from the git repo
// containing the config source directory. Returns empty string if not a git repo.
func ResolveConfigCommitHash(configSourceDirpath string) string {
	repoRootDirpath := findGitRoot(configSourceDirpath)
	if repoRootDirpath == "" {
		return ""
	}

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoRootDirpath
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
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

// ensureSymlink ensures that linkPath is a symlink pointing to targetPath.
// If linkPath already exists and is correct, it does nothing. If it exists
// but is wrong, it removes and recreates.
func ensureSymlink(linkPath string, targetPath string) error {
	info, err := os.Lstat(linkPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(linkPath)
			if readErr == nil && target == targetPath {
				return nil // Already correct
			}
		}
		// Wrong symlink target, or not a symlink — remove it
		if err := os.RemoveAll(linkPath); err != nil {
			return stacktrace.Propagate(err, "failed to remove existing item at '%s'", linkPath)
		}
	} else if !os.IsNotExist(err) {
		return stacktrace.Propagate(err, "failed to stat '%s'", linkPath)
	}

	if err := os.Symlink(targetPath, linkPath); err != nil {
		return stacktrace.Propagate(err, "failed to create symlink '%s' -> '%s'", linkPath, targetPath)
	}
	return nil
}

