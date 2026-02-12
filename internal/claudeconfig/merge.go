package claudeconfig

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// MergeCredentialJSON merges two Claude Code credential JSON blobs. The base
// is typically the global Keychain entry and overlay is the per-mission entry.
//
// Merge rules:
//   - Top-level keys other than "mcpOAuth": overlay wins (simple replacement)
//   - "mcpOAuth" map: per-server merge — if a server key exists in both sides,
//     the entry with the larger "expiresAt" wins; keys in only one side are
//     included unconditionally
//
// Returns the merged JSON bytes, a boolean indicating whether the result
// differs from base, and an error if JSON parsing fails.
func MergeCredentialJSON(base []byte, overlay []byte) ([]byte, bool, error) {
	var baseMap map[string]json.RawMessage
	if err := json.Unmarshal(base, &baseMap); err != nil {
		return nil, false, stacktrace.Propagate(err, "failed to parse base credential JSON")
	}

	var overlayMap map[string]json.RawMessage
	if err := json.Unmarshal(overlay, &overlayMap); err != nil {
		return nil, false, stacktrace.Propagate(err, "failed to parse overlay credential JSON")
	}

	result := make(map[string]json.RawMessage, len(baseMap))
	for k, v := range baseMap {
		result[k] = v
	}

	for k, overlayVal := range overlayMap {
		if k == "mcpOAuth" {
			// Special handling: merge per-server using expiresAt
			merged, err := mergeMcpOAuth(result[k], overlayVal)
			if err != nil {
				return nil, false, stacktrace.Propagate(err, "failed to merge mcpOAuth")
			}
			result[k] = merged
		} else {
			// Overlay wins for all other top-level keys
			result[k] = overlayVal
		}
	}

	merged, err := json.Marshal(result)
	if err != nil {
		return nil, false, stacktrace.Propagate(err, "failed to marshal merged credential JSON")
	}

	// Normalize base for comparison (re-marshal to get consistent formatting)
	normalizedBase, err := json.Marshal(baseMap)
	if err != nil {
		return nil, false, stacktrace.Propagate(err, "failed to normalize base credential JSON")
	}

	changed := !bytes.Equal(normalizedBase, merged)
	return merged, changed, nil
}

// mergeMcpOAuth merges two mcpOAuth JSON maps. For each server key, if present
// in both sides, the entry with the larger "expiresAt" value wins. Keys present
// in only one side are included unconditionally.
func mergeMcpOAuth(baseRaw json.RawMessage, overlayRaw json.RawMessage) (json.RawMessage, error) {
	var baseMap map[string]json.RawMessage
	if baseRaw != nil {
		if err := json.Unmarshal(baseRaw, &baseMap); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse base mcpOAuth")
		}
	}
	if baseMap == nil {
		baseMap = make(map[string]json.RawMessage)
	}

	var overlayMap map[string]json.RawMessage
	if err := json.Unmarshal(overlayRaw, &overlayMap); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse overlay mcpOAuth")
	}

	result := make(map[string]json.RawMessage, len(baseMap))
	for k, v := range baseMap {
		result[k] = v
	}

	for serverKey, overlayEntry := range overlayMap {
		baseEntry, exists := result[serverKey]
		if !exists {
			result[serverKey] = overlayEntry
			continue
		}

		// Both sides have this server — compare expiresAt
		baseExpiry := extractExpiresAt(baseEntry)
		overlayExpiry := extractExpiresAt(overlayEntry)

		if overlayExpiry >= baseExpiry {
			result[serverKey] = overlayEntry
		}
		// else: base has newer token, keep it
	}

	merged, err := json.Marshal(result)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal merged mcpOAuth")
	}

	return json.RawMessage(merged), nil
}

// extractExpiresAt reads the "expiresAt" field from a JSON object as a float64.
// Returns 0 if the field is missing, not a number, or unparseable.
func extractExpiresAt(raw json.RawMessage) float64 {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return 0
	}

	expiresAtRaw, ok := obj["expiresAt"]
	if !ok {
		return 0
	}

	var expiresAt float64
	if err := json.Unmarshal(expiresAtRaw, &expiresAt); err != nil {
		return 0
	}

	return expiresAt
}

// DeepMergeJSON recursively merges overlay into base. Objects are merged
// recursively, arrays are concatenated (base first), and scalars from
// overlay win over base.
func DeepMergeJSON(base map[string]json.RawMessage, overlay map[string]json.RawMessage) (map[string]json.RawMessage, error) {
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
			mergedObj, err := DeepMergeJSON(baseObj, overlayObj)
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

// MergeClaudeMd merges user CLAUDE.md content with agenc-modifications CLAUDE.md
// content. User content comes first, agenc-modifications content is appended.
// Returns the merged bytes (with trailing newline), or nil if both inputs are empty.
func MergeClaudeMd(userContent []byte, modsContent []byte) []byte {
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
		return nil
	}

	return []byte(merged + "\n")
}

// MergeSettings reads user settings and agenc-modifications settings, deep-merges
// them, and appends agenc operational overrides (hooks + deny permissions). Returns
// the final merged JSON bytes. claudeConfigDirpath is the per-mission config
// directory that should be denied from agent access.
func MergeSettings(userSettingsData []byte, modsSettingsData []byte, agencDirpath string, claudeConfigDirpath string) ([]byte, error) {
	// Default to empty objects if nil
	if userSettingsData == nil {
		userSettingsData = []byte("{}")
	}
	if modsSettingsData == nil {
		modsSettingsData = []byte("{}")
	}

	// Parse both into maps
	var userMap map[string]json.RawMessage
	if err := json.Unmarshal(userSettingsData, &userMap); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse user settings JSON")
	}

	var modsMap map[string]json.RawMessage
	if err := json.Unmarshal(modsSettingsData, &modsMap); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse mods settings JSON")
	}

	// Deep-merge: user as base, agenc-modifications as overlay
	mergedMap, err := DeepMergeJSON(userMap, modsMap)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to deep-merge settings")
	}

	// Re-serialize the merged map so we can pass it to the overrides merger
	mergedBase, err := json.Marshal(mergedMap)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal merged settings base")
	}

	// Append agenc operational overrides (hooks + deny permissions)
	mergedData, err := MergeSettingsWithAgencOverrides(mergedBase, agencDirpath, claudeConfigDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to merge settings with agenc overrides")
	}

	return mergedData, nil
}

// MergeSettingsWithAgencOverrides takes raw JSON bytes of settings, merges in
// agenc-specific hooks and deny permissions, and returns the merged JSON bytes.
// The existing hooks and permissions are preserved; agenc entries are appended.
// claudeConfigDirpath is the per-mission config directory to deny agent access to.
func MergeSettingsWithAgencOverrides(settingsData []byte, agencDirpath string, claudeConfigDirpath string) ([]byte, error) {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse settings JSON")
	}

	// --- Hooks ---

	var hooksMap map[string]json.RawMessage
	if existingHooks, ok := settings["hooks"]; ok {
		if err := json.Unmarshal(existingHooks, &hooksMap); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse existing hooks object")
		}
	} else {
		hooksMap = make(map[string]json.RawMessage)
	}

	for hookName, agencEntriesRaw := range AgencHookEntries {
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

	var permsMap map[string]json.RawMessage
	if existingPerms, ok := settings["permissions"]; ok {
		if err := json.Unmarshal(existingPerms, &permsMap); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse existing permissions object")
		}
	} else {
		permsMap = make(map[string]json.RawMessage)
	}

	var existingDeny []string
	if denyData, ok := permsMap["deny"]; ok {
		if err := json.Unmarshal(denyData, &existingDeny); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse existing deny array")
		}
	}

	mergedDeny := append(existingDeny, BuildRepoLibraryDenyEntries(agencDirpath)...)
	if claudeConfigDirpath != "" {
		mergedDeny = append(mergedDeny, BuildClaudeConfigDenyEntries(claudeConfigDirpath)...)
	}
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

	result = append(result, '\n')
	return result, nil
}

// RewriteSettingsPaths rewrites ~/.claude paths in settings JSON while
// preserving the "permissions" block unchanged. This ensures permission
// entries with user-specified paths are not rewritten, while hook commands
// and other fields get updated to point to the mission config directory.
func RewriteSettingsPaths(settingsData []byte, claudeConfigDirpath string) ([]byte, error) {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse settings JSON for path rewriting")
	}

	// Save the permissions block before rewriting
	savedPermissions, hasPermissions := settings["permissions"]

	// Re-serialize, rewrite all paths, then parse back
	intermediate, err := json.Marshal(settings)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal settings for path rewriting")
	}

	rewritten := RewriteClaudePaths(intermediate, claudeConfigDirpath)

	var rewrittenSettings map[string]json.RawMessage
	if err := json.Unmarshal(rewritten, &rewrittenSettings); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse rewritten settings JSON")
	}

	// Restore the original permissions block (un-rewritten)
	if hasPermissions {
		rewrittenSettings["permissions"] = savedPermissions
	}

	result, err := json.MarshalIndent(rewrittenSettings, "", "  ")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal final rewritten settings")
	}
	result = append(result, '\n')

	return result, nil
}

// WriteIfChanged writes data to filepath only if the contents differ from what's
// already on disk. Preserves mtime when nothing changed.
func WriteIfChanged(filepath string, data []byte) error {
	existingData, readErr := os.ReadFile(filepath)
	if readErr == nil && bytes.Equal(existingData, data) {
		return nil
	}

	if err := os.WriteFile(filepath, data, 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write '%s'", filepath)
	}
	return nil
}
