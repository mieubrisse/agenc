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

	// ConfigCommitFilename stores the config source repo's pinned commit hash.
	ConfigCommitFilename = "config-commit"
)

// TrackableItemNames lists the files/directories that are copied from the config
// source into the per-mission claude config directory.
var TrackableItemNames = []string{
	"CLAUDE.md",
	"settings.json",
	"skills",
	"hooks",
	"commands",
	"agents",
	"plugins",
}

// ResolveConfigSourceDirpath returns the filesystem path to the config source
// subdirectory based on the user's AgencConfig. Returns empty string if no
// config source is registered.
func ResolveConfigSourceDirpath(agencDirpath string, cfg *config.AgencConfig) string {
	if cfg.ClaudeConfig == nil || cfg.ClaudeConfig.Repo == "" {
		return ""
	}

	repoDirpath := config.GetRepoDirpath(agencDirpath, cfg.ClaudeConfig.Repo)

	if cfg.ClaudeConfig.Subdirectory != "" {
		return filepath.Join(repoDirpath, cfg.ClaudeConfig.Subdirectory)
	}

	return repoDirpath
}

// BuildMissionConfigDir creates and populates the per-mission claude config
// directory from the user's registered config source repo. It copies trackable
// files, applies AgenC modifications (merged CLAUDE.md, merged settings.json
// with hooks), and symlinks auth files.
//
// configSourceDirpath is the path to the config source subdirectory within the
// cloned repo (e.g., ~/.agenc/repos/github.com/owner/dotfiles/claude/).
func BuildMissionConfigDir(agencDirpath string, missionID string, configSourceDirpath string) error {
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	claudeConfigDirpath := filepath.Join(missionDirpath, MissionClaudeConfigDirname)

	if err := os.MkdirAll(claudeConfigDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create claude-config directory")
	}

	// Copy trackable directories from config source
	dirNames := []string{"skills", "hooks", "commands", "agents", "plugins"}
	for _, dirName := range dirNames {
		srcDirpath := filepath.Join(configSourceDirpath, dirName)
		dstDirpath := filepath.Join(claudeConfigDirpath, dirName)

		if _, err := os.Stat(srcDirpath); os.IsNotExist(err) {
			// Source doesn't exist — remove destination if it exists
			os.RemoveAll(dstDirpath)
			continue
		}

		// Remove existing destination and copy fresh
		os.RemoveAll(dstDirpath)
		if err := copyDir(srcDirpath, dstDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to copy '%s' from config source", dirName)
		}
	}

	// Merge CLAUDE.md: user's from config source + agenc modifications
	agencModsDirpath := config.GetClaudeModificationsDirpath(agencDirpath)
	if err := buildMergedClaudeMd(configSourceDirpath, agencModsDirpath, claudeConfigDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to build merged CLAUDE.md")
	}

	// Merge settings.json: user's from config source + agenc modifications + hooks/deny
	if err := buildMergedSettings(configSourceDirpath, agencModsDirpath, claudeConfigDirpath, agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to build merged settings.json")
	}

	// Symlink .claude.json (account identity)
	if err := symlinkClaudeJSON(claudeConfigDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to symlink .claude.json")
	}

	// Ensure central credentials and symlink .credentials.json
	if err := ensureAndSymlinkCredentials(agencDirpath, claudeConfigDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to set up .credentials.json")
	}

	// Record config source commit hash
	if err := recordConfigCommit(configSourceDirpath, missionDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to record config commit")
	}

	return nil
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

// buildMergedClaudeMd reads user CLAUDE.md from config source and agenc
// modifications, merges them, and writes to the destination config directory.
func buildMergedClaudeMd(configSourceDirpath string, agencModsDirpath string, destDirpath string) error {
	destFilepath := filepath.Join(destDirpath, "CLAUDE.md")

	userContent, err := os.ReadFile(filepath.Join(configSourceDirpath, "CLAUDE.md"))
	if err != nil && !os.IsNotExist(err) {
		return stacktrace.Propagate(err, "failed to read user CLAUDE.md from config source")
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

// buildMergedSettings reads user settings from config source and agenc
// modifications, deep-merges them, adds agenc hooks/deny, and writes to dest.
func buildMergedSettings(configSourceDirpath string, agencModsDirpath string, destDirpath string, agencDirpath string) error {
	destFilepath := filepath.Join(destDirpath, "settings.json")

	userSettingsData, err := os.ReadFile(filepath.Join(configSourceDirpath, "settings.json"))
	if err != nil {
		if os.IsNotExist(err) {
			userSettingsData = []byte("{}")
		} else {
			return stacktrace.Propagate(err, "failed to read user settings from config source")
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

// ensureAndSymlinkCredentials ensures a central credentials file exists at
// ~/.agenc/claude/.credentials.json and creates a symlink from the mission
// config directory to it.
func ensureAndSymlinkCredentials(agencDirpath string, claudeConfigDirpath string) error {
	centralCredentialsFilepath := filepath.Join(config.GetGlobalClaudeDirpath(agencDirpath), ".credentials.json")

	// If central copy doesn't exist or is empty, dump from platform source
	if !fileExistsAndNonEmpty(centralCredentialsFilepath) {
		if err := DumpCredentials(centralCredentialsFilepath); err != nil {
			return stacktrace.Propagate(err, "failed to dump credentials to central location")
		}
	}

	linkPath := filepath.Join(claudeConfigDirpath, ".credentials.json")
	return ensureSymlink(linkPath, centralCredentialsFilepath)
}

// DumpCredentials dumps auth credentials to the specified file.
// On macOS, reads from Keychain. On Linux, copies from ~/.claude/.credentials.json.
func DumpCredentials(destFilepath string) error {
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
	repoRootDirpath := FindGitRoot(configSourceDirpath)
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

// recordConfigCommit reads the HEAD commit hash from the config source repo
// and writes it to the mission's config-commit file.
func recordConfigCommit(configSourceDirpath string, missionDirpath string) error {
	// Walk up from configSourceDirpath to find the repo root (.git directory)
	repoRootDirpath := FindGitRoot(configSourceDirpath)
	if repoRootDirpath == "" {
		// Not a git repo — skip commit recording
		return nil
	}

	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoRootDirpath
	output, err := cmd.Output()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get HEAD commit from config source repo")
	}

	commitHash := strings.TrimSpace(string(output))
	commitFilepath := filepath.Join(missionDirpath, ConfigCommitFilename)

	if err := os.WriteFile(commitFilepath, []byte(commitHash+"\n"), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write config-commit file")
	}

	return nil
}

// FindGitRoot walks up from the given path looking for a .git directory.
// Returns the repo root path, or empty string if not found.
func FindGitRoot(startPath string) string {
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

// fileExistsAndNonEmpty returns true if the file exists and has content.
func fileExistsAndNonEmpty(filepath string) bool {
	info, err := os.Stat(filepath)
	if err != nil {
		return false
	}
	return info.Size() > 0
}

// copyDir recursively copies a directory tree from src to dst.
func copyDir(srcDirpath string, dstDirpath string) error {
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

		// Regular file — copy contents
		data, err := os.ReadFile(path)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read '%s'", path)
		}

		return os.WriteFile(dstPath, data, info.Mode())
	})
}
