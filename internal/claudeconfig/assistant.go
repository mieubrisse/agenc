package claudeconfig

import (
	_ "embed"
	"encoding/json"
	"os"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

//go:embed assistant_claude.md
var assistantClaudeMdContent string

// buildAssistantClaudeMd builds the CLAUDE.md for assistant missions by merging
// the user's CLAUDE.md, agenc modifications, and the assistant-specific
// instructions. The assistant content is appended last so it takes precedence.
func buildAssistantClaudeMd(shadowDirpath string, agencModsDirpath string, destDirpath string) error {
	// First build the standard merged CLAUDE.md (user + agenc mods)
	if err := buildMergedClaudeMd(shadowDirpath, agencModsDirpath, destDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to build base merged CLAUDE.md for assistant")
	}

	// Now read the just-written merged CLAUDE.md and append assistant content
	destFilepath := destDirpath + "/CLAUDE.md"
	existingContent, _ := readFileOrEmpty(destFilepath)

	mergedBytes := MergeClaudeMd(existingContent, []byte(assistantClaudeMdContent))
	if mergedBytes == nil {
		mergedBytes = []byte(assistantClaudeMdContent + "\n")
	}

	return WriteIfChanged(destFilepath, mergedBytes)
}

// buildAssistantSettings builds the settings.json for assistant missions by
// performing the standard merge and then injecting assistant-specific
// permissions (allow entries for agenc operations, deny entries for other
// missions' agent dirs).
func buildAssistantSettings(shadowDirpath string, agencModsDirpath string, destDirpath string, agencDirpath string, missionID string) error {
	// Build the standard merged settings first
	if err := buildMergedSettings(shadowDirpath, agencModsDirpath, destDirpath, agencDirpath, missionID); err != nil {
		return stacktrace.Propagate(err, "failed to build base merged settings for assistant")
	}

	// Read the just-written settings and inject assistant permissions
	destFilepath := destDirpath + "/settings.json"
	settingsData, err := readFileOrError(destFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read merged settings for assistant permission injection")
	}

	injectedData, err := injectAssistantPermissions(settingsData, agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to inject assistant permissions")
	}

	return WriteIfChanged(destFilepath, injectedData)
}

// injectAssistantPermissions appends assistant-specific allow and deny entries
// to the permissions block of the given settings JSON.
func injectAssistantPermissions(settingsData []byte, agencDirpath string) ([]byte, error) {
	var settings map[string]json.RawMessage
	if err := json.Unmarshal(settingsData, &settings); err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse settings JSON for assistant permission injection")
	}

	var permsMap map[string]json.RawMessage
	if existingPerms, ok := settings["permissions"]; ok {
		if err := json.Unmarshal(existingPerms, &permsMap); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse existing permissions")
		}
	} else {
		permsMap = make(map[string]json.RawMessage)
	}

	// Inject allow entries
	var existingAllow []string
	if allowData, ok := permsMap["allow"]; ok {
		if err := json.Unmarshal(allowData, &existingAllow); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse existing allow array")
		}
	}

	mergedAllow := append(existingAllow, BuildAssistantAllowEntries(agencDirpath)...)
	allowBytes, err := json.Marshal(mergedAllow)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal merged allow array")
	}
	permsMap["allow"] = json.RawMessage(allowBytes)

	// Inject deny entries
	var existingDeny []string
	if denyData, ok := permsMap["deny"]; ok {
		if err := json.Unmarshal(denyData, &existingDeny); err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse existing deny array")
		}
	}

	mergedDeny := append(existingDeny, BuildAssistantDenyEntries(agencDirpath)...)
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

	result, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal settings with assistant permissions")
	}
	result = append(result, '\n')

	return result, nil
}

// BuildAssistantAllowEntries returns permission allow entries for assistant
// missions. These grant read/write access to the agenc data directory and
// permission to run agenc commands.
func BuildAssistantAllowEntries(agencDirpath string) []string {
	agencPattern := agencDirpath + "/**"
	readWriteTools := []string{"Read", "Write", "Edit", "Glob", "Grep"}

	entries := make([]string, 0, len(readWriteTools)+1)
	for _, tool := range readWriteTools {
		entries = append(entries, tool+"("+agencPattern+")")
	}
	entries = append(entries, "Bash(agenc:*)")

	return entries
}

// BuildAssistantDenyEntries returns permission deny entries for assistant
// missions. These prevent writing to other missions' agent directories
// while allowing read access.
func BuildAssistantDenyEntries(agencDirpath string) []string {
	agentPattern := agencDirpath + "/" + config.MissionsDirname + "/*/" + config.AgentDirname + "/**"
	writeTools := []string{"Write", "Edit"}

	entries := make([]string, 0, len(writeTools))
	for _, tool := range writeTools {
		entries = append(entries, tool+"("+agentPattern+")")
	}

	return entries
}

// readFileOrEmpty reads a file and returns its content, or nil if the file
// does not exist.
func readFileOrEmpty(filepath string) ([]byte, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, nil
	}
	return data, nil
}

// readFileOrError reads a file and returns an error if it cannot be read.
func readFileOrError(filepath string) ([]byte, error) {
	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read file '%s'", filepath)
	}
	return data, nil
}
