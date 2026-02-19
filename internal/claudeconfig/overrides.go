package claudeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// AgencHookEntries defines the hook entries that agenc appends to the user's
// hooks. Keys are hook event names, values are JSON arrays of hook group objects.
// Each hook calls `agenc mission send claude-update` which sends the event to
// the wrapper's unix socket for state tracking and tmux pane coloring.
var AgencHookEntries = map[string]json.RawMessage{
	"Stop":               json.RawMessage(`[{"hooks":[{"type":"command","command":"agenc mission send claude-update $AGENC_MISSION_UUID Stop"}]}]`),
	"UserPromptSubmit":   json.RawMessage(`[{"hooks":[{"type":"command","command":"agenc mission send claude-update $AGENC_MISSION_UUID UserPromptSubmit"}]}]`),
	"Notification":       json.RawMessage(`[{"hooks":[{"type":"command","command":"agenc mission send claude-update $AGENC_MISSION_UUID Notification"}]}]`),
	"PostToolUse":        json.RawMessage(`[{"hooks":[{"type":"command","command":"agenc mission send claude-update $AGENC_MISSION_UUID PostToolUse"}]}]`),
	"PostToolUseFailure": json.RawMessage(`[{"hooks":[{"type":"command","command":"agenc mission send claude-update $AGENC_MISSION_UUID PostToolUseFailure"}]}]`),
}

// AgencDenyPermissionTools lists the Claude Code tools to deny access for
// on the repo library directory.
var AgencDenyPermissionTools = []string{
	"Read",
	"Glob",
	"Grep",
	"Write",
	"Edit",
}

// BuildRepoLibraryDenyEntries constructs permission deny entries that prevent
// agents from accessing the shared repo library under the given agenc dir.
func BuildRepoLibraryDenyEntries(agencDirpath string) []string {
	reposPattern := agencDirpath + "/repos/**"
	entries := make([]string, 0, len(AgencDenyPermissionTools))
	for _, tool := range AgencDenyPermissionTools {
		entries = append(entries, tool+"("+reposPattern+")")
	}
	return entries
}

// BuildClaudeConfigDenyEntries constructs permission deny entries that prevent
// agents from editing their own mission's claude-config directory. This stops
// the agent from modifying the settings, hooks, CLAUDE.md, and other config
// that AgenC injects to control agent behavior.
//
// Generates deny rules for three path formats (absolute, tilde, ${HOME}) to ensure
// agents cannot bypass the deny rules by using different path representations.
func BuildClaudeConfigDenyEntries(claudeConfigDirpath string) []string {
	// Convert absolute path to all three variants
	patterns := buildPathVariants(claudeConfigDirpath)

	// Generate deny entries for each tool × each path variant
	entries := make([]string, 0, len(AgencDenyPermissionTools)*len(patterns))
	for _, tool := range AgencDenyPermissionTools {
		for _, pattern := range patterns {
			entries = append(entries, tool+"("+pattern+"/**)")
		}
	}
	return entries
}

// buildPathVariants converts an absolute path to all three Claude Code path formats:
// absolute, tilde-prefixed, and ${HOME}-prefixed. This ensures permission rules
// work regardless of which path format the agent uses.
func buildPathVariants(absolutePath string) []string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Can't determine home — only return absolute path
		return []string{absolutePath}
	}

	variants := []string{absolutePath} // Always include absolute path

	// Check if path is under home directory
	if strings.HasPrefix(absolutePath, homeDir+string(filepath.Separator)) {
		relPath, err := filepath.Rel(homeDir, absolutePath)
		if err == nil {
			// Add tilde variant: ~/.agenc/missions/...
			variants = append(variants, filepath.Join("~", relPath))
			// Add ${HOME} variant: ${HOME}/.agenc/missions/...
			variants = append(variants, filepath.Join("${HOME}", relPath))
		}
	}

	return variants
}
