package daemon

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
)

const (
	configSyncInterval = 5 * time.Minute
	settingsFilename   = "settings.json"
)

// symlinkItemNames lists the items from ~/.claude/ to symlink into the agenc
// Claude config directory. Each may be a file or directory.
var symlinkItemNames = []string{
	"skills",
	"commands",
	"agents",
	"plugins",
}

// runConfigSyncLoop periodically syncs the user's Claude config into the agenc
// Claude config directory. This supports legacy missions that still use the
// global config dir. New missions use per-mission config dirs built at creation.
func (d *Daemon) runConfigSyncLoop(ctx context.Context) {
	d.runConfigSyncCycle()

	ticker := time.NewTicker(configSyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runConfigSyncCycle()
		}
	}
}

// runConfigSyncCycle performs a single config sync: symlinks + settings merge + CLAUDE.md merge.
func (d *Daemon) runConfigSyncCycle() {
	userClaudeDirpath, err := config.GetUserClaudeDirpath()
	if err != nil {
		d.logger.Printf("Config sync: failed to determine user claude dir: %v", err)
		return
	}

	agencClaudeDirpath := config.GetGlobalClaudeDirpath(d.agencDirpath)
	agencModsDirpath := config.GetClaudeModificationsDirpath(d.agencDirpath)

	if err := syncSymlinks(userClaudeDirpath, agencClaudeDirpath); err != nil {
		d.logger.Printf("Config sync: symlink sync failed: %v", err)
	}

	if err := syncClaudeMd(userClaudeDirpath, agencModsDirpath, agencClaudeDirpath); err != nil {
		d.logger.Printf("Config sync: CLAUDE.md sync failed: %v", err)
	}

	if err := syncSettings(userClaudeDirpath, agencModsDirpath, agencClaudeDirpath, d.agencDirpath); err != nil {
		d.logger.Printf("Config sync: settings sync failed: %v", err)
	}

	d.logger.Println("Config sync: cycle completed")
}

// syncSymlinks ensures that each item in symlinkItemNames is symlinked from
// the user's Claude dir into the agenc Claude dir. If the source item does not
// exist, any existing symlink in the destination is removed.
func syncSymlinks(userClaudeDirpath string, agencClaudeDirpath string) error {
	var firstErr error
	for _, itemName := range symlinkItemNames {
		srcPath := filepath.Join(userClaudeDirpath, itemName)
		dstPath := filepath.Join(agencClaudeDirpath, itemName)

		srcExists, err := pathExists(srcPath)
		if err != nil {
			if firstErr == nil {
				firstErr = stacktrace.Propagate(err, "failed to check source '%s'", srcPath)
			}
			continue
		}

		if srcExists {
			if err := ensureSymlink(dstPath, srcPath); err != nil {
				if firstErr == nil {
					firstErr = stacktrace.Propagate(err, "failed to ensure symlink for '%s'", itemName)
				}
			}
		} else {
			if err := removeSymlinkIfPresent(dstPath); err != nil {
				if firstErr == nil {
					firstErr = stacktrace.Propagate(err, "failed to remove symlink for '%s'", itemName)
				}
			}
		}
	}
	return firstErr
}

// ensureSymlink ensures that dstPath is a symlink pointing to srcPath.
// If dstPath already exists and is correct, it does nothing. If dstPath exists
// but is wrong (different target or not a symlink), it removes and recreates.
func ensureSymlink(dstPath string, srcPath string) error {
	info, err := os.Lstat(dstPath)
	if err == nil {
		// Something exists at dstPath
		if info.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(dstPath)
			if readErr == nil && target == srcPath {
				return nil // Already correct
			}
		}
		// Wrong symlink target, or not a symlink at all -- remove it
		if err := os.RemoveAll(dstPath); err != nil {
			return stacktrace.Propagate(err, "failed to remove existing item at '%s'", dstPath)
		}
	} else if !os.IsNotExist(err) {
		return stacktrace.Propagate(err, "failed to stat '%s'", dstPath)
	}

	if err := os.Symlink(srcPath, dstPath); err != nil {
		return stacktrace.Propagate(err, "failed to create symlink '%s' -> '%s'", dstPath, srcPath)
	}
	return nil
}

// removeSymlinkIfPresent removes dstPath only if it is a symlink. If dstPath
// does not exist or is not a symlink, it does nothing.
func removeSymlinkIfPresent(dstPath string) error {
	info, err := os.Lstat(dstPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return stacktrace.Propagate(err, "failed to lstat '%s'", dstPath)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(dstPath); err != nil {
			return stacktrace.Propagate(err, "failed to remove symlink '%s'", dstPath)
		}
	}
	return nil
}

// pathExists returns true if the path exists (following symlinks for files,
// using Lstat for symlinks themselves).
func pathExists(path string) (bool, error) {
	_, err := os.Lstat(path)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// syncSettings reads the user's settings.json and the agenc-modifications
// settings.json, deep-merges them, appends agenc hooks, and writes the
// result to the agenc Claude config dir. Only writes if the contents differ.
func syncSettings(userClaudeDirpath string, agencModsDirpath string, agencClaudeDirpath string, agencDirpath string) error {
	userSettingsFilepath := filepath.Join(userClaudeDirpath, settingsFilename)
	modsSettingsFilepath := filepath.Join(agencModsDirpath, settingsFilename)
	agencSettingsFilepath := filepath.Join(agencClaudeDirpath, settingsFilename)

	// Read user settings (empty object if missing)
	userSettingsData, err := os.ReadFile(userSettingsFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			userSettingsData = []byte("{}")
		} else {
			return stacktrace.Propagate(err, "failed to read user settings at '%s'", userSettingsFilepath)
		}
	}

	// Read agenc-modifications settings (empty object if missing)
	modsSettingsData, err := os.ReadFile(modsSettingsFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			modsSettingsData = []byte("{}")
		} else {
			return stacktrace.Propagate(err, "failed to read mods settings at '%s'", modsSettingsFilepath)
		}
	}

	mergedData, err := claudeconfig.MergeSettings(userSettingsData, modsSettingsData, agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to merge settings")
	}

	// Only write if contents differ (preserves mtime when nothing changed)
	existingData, readErr := os.ReadFile(agencSettingsFilepath)
	if readErr == nil && bytes.Equal(existingData, mergedData) {
		return nil
	}

	if err := os.WriteFile(agencSettingsFilepath, mergedData, 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write merged settings to '%s'", agencSettingsFilepath)
	}

	return nil
}

// syncClaudeMd merges the user's CLAUDE.md with the agenc-modifications
// CLAUDE.md and writes the result to the agenc Claude config directory.
// User content comes first, agenc-modifications content is appended after.
// Only writes if content differs from the existing file.
func syncClaudeMd(userClaudeDirpath string, agencModsDirpath string, agencClaudeDirpath string) error {
	claudeMdFilename := "CLAUDE.md"
	userFilepath := filepath.Join(userClaudeDirpath, claudeMdFilename)
	modsFilepath := filepath.Join(agencModsDirpath, claudeMdFilename)
	destFilepath := filepath.Join(agencClaudeDirpath, claudeMdFilename)

	userContent, err := os.ReadFile(userFilepath)
	if err != nil && !os.IsNotExist(err) {
		return stacktrace.Propagate(err, "failed to read user CLAUDE.md at '%s'", userFilepath)
	}

	modsContent, err := os.ReadFile(modsFilepath)
	if err != nil && !os.IsNotExist(err) {
		return stacktrace.Propagate(err, "failed to read mods CLAUDE.md at '%s'", modsFilepath)
	}

	// If destFilepath is a stale symlink (left over from a previous version
	// that symlinked CLAUDE.md), remove it before we read or write. Without
	// this, ReadFile/WriteFile follow the symlink and corrupt the user's
	// source ~/.claude/CLAUDE.md.
	if err := removeSymlinkIfPresent(destFilepath); err != nil {
		return stacktrace.Propagate(err, "failed to remove stale symlink at '%s'", destFilepath)
	}

	mergedBytes := claudeconfig.MergeClaudeMd(userContent, modsContent)
	if mergedBytes == nil {
		// Both empty — remove destination if it exists
		if err := os.Remove(destFilepath); err != nil && !os.IsNotExist(err) {
			return stacktrace.Propagate(err, "failed to remove '%s'", destFilepath)
		}
		return nil
	}

	return claudeconfig.WriteIfChanged(destFilepath, mergedBytes)
}
