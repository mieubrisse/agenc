package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

// agencDenyPermissionTools lists the Claude Code tools to deny access for
// on the repo library directory. Used by buildRepoLibraryDenyEntries.
var agencDenyPermissionTools = []string{
	"Read",
	"Glob",
	"Grep",
	"Write",
	"Edit",
}

// buildRepoLibraryDenyEntries constructs permission deny entries that prevent
// agents from accessing the shared repo library under the given agenc dir.
func buildRepoLibraryDenyEntries(agencDirpath string) []string {
	reposPattern := agencDirpath + "/repos/**"
	entries := make([]string, 0, len(agencDenyPermissionTools))
	for _, tool := range agencDenyPermissionTools {
		entries = append(entries, tool+"("+reposPattern+")")
	}
	return entries
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

	// Parse both into maps
	var userMap map[string]json.RawMessage
	if err := json.Unmarshal(userSettingsData, &userMap); err != nil {
		return stacktrace.Propagate(err, "failed to parse user settings JSON")
	}

	var modsMap map[string]json.RawMessage
	if err := json.Unmarshal(modsSettingsData, &modsMap); err != nil {
		return stacktrace.Propagate(err, "failed to parse mods settings JSON")
	}

	// Deep-merge: user as base, agenc-modifications as overlay
	mergedMap, err := deepMergeJSON(userMap, modsMap)
	if err != nil {
		return stacktrace.Propagate(err, "failed to deep-merge settings")
	}

	// Re-serialize the merged map so we can pass it to the hooks merger
	mergedBase, err := json.Marshal(mergedMap)
	if err != nil {
		return stacktrace.Propagate(err, "failed to marshal merged settings base")
	}

	// Append agenc operational overrides (hooks + deny permissions)
	mergedData, err := mergeSettingsWithAgencOverrides(mergedBase, agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to merge settings with agenc overrides")
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

	userTrimmed := strings.TrimSpace(string(userContent))
	modsTrimmed := strings.TrimSpace(string(modsContent))

	var merged string
	switch {
	case userTrimmed != "" && modsTrimmed != "":
		merged = userTrimmed + "\n\n" + modsTrimmed
	case userTrimmed != "":
		merged = userTrimmed
	case modsTrimmed != "":
		merged = modsTrimmed
	default:
		// Both empty — remove destination if it exists
		if err := os.Remove(destFilepath); err != nil && !os.IsNotExist(err) {
			return stacktrace.Propagate(err, "failed to remove '%s'", destFilepath)
		}
		return nil
	}

	// Add trailing newline
	mergedBytes := []byte(merged + "\n")

	// Only write if contents differ
	existingData, readErr := os.ReadFile(destFilepath)
	if readErr == nil && bytes.Equal(existingData, mergedBytes) {
		return nil
	}

	if err := os.WriteFile(destFilepath, mergedBytes, 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write merged CLAUDE.md to '%s'", destFilepath)
	}

	return nil
}

// deepMergeJSON recursively merges overlay into base. Objects are merged
// recursively, arrays are concatenated (base first), and scalars from
// overlay win over base.
func deepMergeJSON(base map[string]json.RawMessage, overlay map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	result := make(map[string]json.RawMessage, len(base))
	for k, v := range base {
		result[k] = v
	}

	for k, overlayVal := range overlay {
		baseVal, exists := result[k]
		if !exists {
			result[k] = overlayVal
			continue
		}

		// Try to treat both as objects
		var baseObj map[string]json.RawMessage
		var overlayObj map[string]json.RawMessage
		baseObjErr := json.Unmarshal(baseVal, &baseObj)
		overlayObjErr := json.Unmarshal(overlayVal, &overlayObj)

		if baseObjErr == nil && overlayObjErr == nil && baseObj != nil && overlayObj != nil {
			// Both are objects — recurse
			mergedObj, err := deepMergeJSON(baseObj, overlayObj)
			if err != nil {
				return nil, stacktrace.Propagate(err, "failed to deep-merge key '%s'", k)
			}
			mergedData, err := json.Marshal(mergedObj)
			if err != nil {
				return nil, stacktrace.Propagate(err, "failed to marshal merged key '%s'", k)
			}
			result[k] = json.RawMessage(mergedData)
			continue
		}

		// Try to treat both as arrays
		var baseArr []json.RawMessage
		var overlayArr []json.RawMessage
		baseArrErr := json.Unmarshal(baseVal, &baseArr)
		overlayArrErr := json.Unmarshal(overlayVal, &overlayArr)

		if baseArrErr == nil && overlayArrErr == nil && baseArr != nil && overlayArr != nil {
			// Both are arrays — concatenate
			concatenated := append(baseArr, overlayArr...)
			concatData, err := json.Marshal(concatenated)
			if err != nil {
				return nil, stacktrace.Propagate(err, "failed to marshal concatenated arrays for key '%s'", k)
			}
			result[k] = json.RawMessage(concatData)
			continue
		}

		// Scalar or type mismatch — overlay wins
		result[k] = overlayVal
	}

	return result, nil
}

// mergeSettingsWithAgencOverrides takes raw JSON bytes of the user's settings.json,
// merges in agenc-specific hooks and deny permissions, and returns the merged JSON bytes.
// The user's existing hooks and permissions are preserved; agenc entries are appended.
func mergeSettingsWithAgencOverrides(userSettingsData []byte, agencDirpath string) ([]byte, error) {
	// Parse into map[string]json.RawMessage to preserve all fields as raw JSON
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(userSettingsData, &settings); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse user settings JSON")
	}

	// --- Hooks ---

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

	// --- Permissions (deny) ---

	// Extract existing permissions map, or create empty one
	var permsMap map[string]json.RawMessage
	if existingPerms, ok := settings["permissions"]; ok {
		if err := json.Unmarshal(existingPerms, &permsMap); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse existing permissions object")
		}
	} else {
		permsMap = make(map[string]json.RawMessage)
	}

	// Append agenc deny entries to the existing deny array
	var existingDeny []string
	if denyData, ok := permsMap["deny"]; ok {
		if err := json.Unmarshal(denyData, &existingDeny); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse existing deny array")
		}
	}

	mergedDeny := append(existingDeny, buildRepoLibraryDenyEntries(agencDirpath)...)
	denyBytes, err := json.Marshal(mergedDeny)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal merged deny array")
	}
	permsMap["deny"] = json.RawMessage(denyBytes)

	permsBytes, err := json.Marshal(permsMap)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal merged permissions")
	}
	settings["permissions"] = json.RawMessage(permsBytes)

	// --- Serialize ---

	result, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal merged settings")
	}

	// Append trailing newline
	result = append(result, '\n')

	return result, nil
}
