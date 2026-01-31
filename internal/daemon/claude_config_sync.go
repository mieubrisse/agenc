package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

const (
	configSyncInterval = 5 * time.Minute
	settingsFilename   = "settings.json"
)

// symlinkItemNames lists the items from ~/.claude/ to symlink into the agenc
// Claude config directory. Each may be a file or directory.
var symlinkItemNames = []string{
	"CLAUDE.md",
	"skills",
	"commands",
	"agents",
	"plugins",
}

// agencHookEntries defines the hook entries that agenc appends to the user's
// hooks. Keys are hook event names, values are JSON arrays of hook group objects.
var agencHookEntries = map[string]json.RawMessage{
	"Stop":             json.RawMessage(`[{"hooks":[{"type":"command","command":"echo idle > \"$CLAUDE_PROJECT_DIR/../claude-state\""}]}]`),
	"UserPromptSubmit": json.RawMessage(`[{"hooks":[{"type":"command","command":"echo busy > \"$CLAUDE_PROJECT_DIR/../claude-state\""}]}]`),
}

// runConfigSyncLoop periodically syncs the user's Claude config into the agenc
// Claude config directory.
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

// runConfigSyncCycle performs a single config sync: symlinks + settings merge.
func (d *Daemon) runConfigSyncCycle() {
	userClaudeDirpath, err := config.GetUserClaudeDirpath()
	if err != nil {
		d.logger.Printf("Config sync: failed to determine user claude dir: %v", err)
		return
	}

	agencClaudeDirpath := config.GetGlobalClaudeDirpath(d.agencDirpath)

	if err := syncSymlinks(userClaudeDirpath, agencClaudeDirpath); err != nil {
		d.logger.Printf("Config sync: symlink sync failed: %v", err)
	}

	if err := syncSettings(userClaudeDirpath, agencClaudeDirpath); err != nil {
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

// syncSettings reads the user's settings.json, merges in agenc hooks, and
// writes the result to the agenc Claude config dir. Only writes if the
// contents differ from the existing file.
func syncSettings(userClaudeDirpath string, agencClaudeDirpath string) error {
	userSettingsFilepath := filepath.Join(userClaudeDirpath, settingsFilename)
	agencSettingsFilepath := filepath.Join(agencClaudeDirpath, settingsFilename)

	// Read user settings -- follow symlinks transparently via ReadFile.
	// If the file doesn't exist, start with an empty object.
	userSettingsData, err := os.ReadFile(userSettingsFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			userSettingsData = []byte("{}")
		} else {
			return stacktrace.Propagate(err, "failed to read user settings at '%s'", userSettingsFilepath)
		}
	}

	mergedData, err := mergeSettingsWithAgencHooks(userSettingsData)
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

// mergeSettingsWithAgencHooks takes raw JSON bytes of the user's settings.json,
// merges in the agenc-specific hooks, and returns the merged JSON bytes.
// The user's existing hooks are preserved; agenc hooks are appended to the end
// of each relevant hook array.
func mergeSettingsWithAgencHooks(userSettingsData []byte) ([]byte, error) {
	// Parse into map[string]json.RawMessage to preserve all fields as raw JSON
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(userSettingsData, &settings); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse user settings JSON")
	}

	// Extract existing hooks map, or create empty one
	var hooksMap map[string]json.RawMessage
	if existingHooks, ok := settings["hooks"]; ok {
		if err := json.Unmarshal(existingHooks, &hooksMap); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse existing hooks object")
		}
	} else {
		hooksMap = make(map[string]json.RawMessage)
	}

	// For each agenc hook type, append entries to the existing array
	for hookName, agencEntriesRaw := range agencHookEntries {
		var agencArray []json.RawMessage
		if err := json.Unmarshal(agencEntriesRaw, &agencArray); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse agenc hook entries for '%s'", hookName)
		}

		var existingArray []json.RawMessage
		if existingData, ok := hooksMap[hookName]; ok {
			if err := json.Unmarshal(existingData, &existingArray); err != nil {
				return nil, stacktrace.Propagate(err, "failed to parse existing hook array for '%s'", hookName)
			}
		}

		mergedArray := append(existingArray, agencArray...)
		mergedData, err := json.Marshal(mergedArray)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to marshal merged hook array for '%s'", hookName)
		}
		hooksMap[hookName] = json.RawMessage(mergedData)
	}

	hooksData, err := json.Marshal(hooksMap)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal merged hooks map")
	}
	settings["hooks"] = json.RawMessage(hooksData)

	result, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal merged settings")
	}

	// Append trailing newline
	result = append(result, '\n')

	return result, nil
}
