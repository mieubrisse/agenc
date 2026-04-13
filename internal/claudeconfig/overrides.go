package claudeconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// claudeConfigProtectedItems lists the files and directories inside
// claude-config that agents must not read or modify. These are the
// AgenC-injected configuration files; everything else (symlinked
// runtime dirs like shell-snapshots, plugins, projects, etc.) is left
// accessible so Claude Code can operate normally.
var claudeConfigProtectedItems = TrackableItemNames

// agencHookEventNames lists the Claude hook events that agenc intercepts to
// track Claude state and update tmux pane colors.
var agencHookEventNames = []string{
	"Stop",
	"UserPromptSubmit",
	"Notification",
	"PostToolUse",
	"PostToolUseFailure",
}

// AgencHookEntries maps each hook event name to the JSON hook group that agenc
// injects into the mission's settings.json. Each hook calls
// `agenc mission send claude-update` with the event name, forwarding it to the
// wrapper's unix socket for state tracking and tmux pane coloring.
var AgencHookEntries map[string]json.RawMessage

// ContainerHookEntries maps each hook event name to the JSON hook group used
// in containerized missions. These hooks use curl to reach the wrapper socket
// (bind-mounted at $AGENC_WRAPPER_SOCKET) instead of the agenc CLI binary,
// which is not available inside the container.
var ContainerHookEntries map[string]json.RawMessage

func init() {
	AgencHookEntries = make(map[string]json.RawMessage, len(agencHookEventNames))
	for _, eventName := range agencHookEventNames {
		entry := `[{"hooks":[{"type":"command","command":"agenc mission send claude-update $AGENC_MISSION_UUID ` + eventName + `"}]}]`
		AgencHookEntries[eventName] = json.RawMessage(entry)
	}

	ContainerHookEntries = make(map[string]json.RawMessage, len(agencHookEventNames))
	for _, eventName := range agencHookEventNames {
		cmd := fmt.Sprintf(
			`curl -s --unix-socket $AGENC_WRAPPER_SOCKET -X POST http://w/claude-update/%s -H "Content-Type: application/json" -d @- -o /dev/null || true`,
			eventName,
		)
		entry := fmt.Sprintf(`[{"hooks":[{"type":"command","command":"%s"}]}]`, cmd)
		ContainerHookEntries[eventName] = json.RawMessage(entry)
	}
}

// AgencFilePermissionTools lists the Claude Code file-access tools used to
// construct both allow and deny permission entries.
var AgencFilePermissionTools = []string{
	"Read",
	"Glob",
	"Grep",
	"Write",
	"Edit",
	"NotebookEdit",
}

// AgencDenyPermissionTools is an alias preserved for readability in deny-specific
// contexts. It references the same tool list as AgencFilePermissionTools.
var AgencDenyPermissionTools = AgencFilePermissionTools

// BuildAgentDirAllowEntries returns permission allow entries that grant agents
// full read/write access to their own working directory. Generates entries
// using both relative paths (./**) and absolute path variants (absolute,
// tilde, ${HOME}) to ensure the sandbox filesystem allowlist includes the
// agent directory regardless of how paths are resolved.
func BuildAgentDirAllowEntries(agentDirpath string) []string {
	// Relative entries cover tool-level access from the working directory
	relativePattern := "./**"

	// Absolute entries ensure the Bash sandbox filesystem allowlist includes
	// the agent directory by its full path
	absolutePatterns := buildPathVariants(agentDirpath)

	allPatterns := make([]string, 0, 1+len(absolutePatterns))
	allPatterns = append(allPatterns, relativePattern)
	for _, p := range absolutePatterns {
		allPatterns = append(allPatterns, p+"/**")
	}

	entries := make([]string, 0, len(AgencFilePermissionTools)*len(allPatterns))
	for _, tool := range AgencFilePermissionTools {
		for _, pattern := range allPatterns {
			entries = append(entries, tool+"("+pattern+")")
		}
	}
	return entries
}

// AgencRepoLibraryWriteTools lists the tools denied write access to the shared
// repo library. Read-only tools (Read, Glob, Grep) are intentionally omitted
// so agents can explore code in the repo library without spawning a new mission.
var AgencRepoLibraryWriteTools = []string{
	"Write",
	"Edit",
	"NotebookEdit",
}

// BuildRepoLibraryDenyEntries constructs permission deny entries that prevent
// agents from modifying the shared repo library under the given agenc dir.
// Read-only access (Read, Glob, Grep) is allowed so agents can explore code
// in other repos without needing to spawn a new mission.
func BuildRepoLibraryDenyEntries(agencDirpath string) []string {
	reposPattern := agencDirpath + "/repos/**"
	entries := make([]string, 0, len(AgencRepoLibraryWriteTools))
	for _, tool := range AgencRepoLibraryWriteTools {
		entries = append(entries, tool+"("+reposPattern+")")
	}
	return entries
}

// BuildClaudeConfigDenyEntries constructs permission deny entries that prevent
// agents from reading or modifying the AgenC-injected configuration files
// inside their mission's claude-config directory (CLAUDE.md, settings.json,
// skills/, hooks/, commands/, agents/).
//
// Only the protected items are denied — symlinked runtime directories like
// shell-snapshots, plugins, and projects are left accessible so Claude Code
// can operate normally.
//
// Generates deny rules for three path formats (absolute, tilde, ${HOME}) to ensure
// agents cannot bypass the deny rules by using different path representations.
func BuildClaudeConfigDenyEntries(claudeConfigDirpath string) []string {
	baseVariants := buildPathVariants(claudeConfigDirpath)

	// Build the list of per-item path suffixes. Files get an exact match;
	// directories get a /** glob to cover their contents.
	var itemSuffixes []string
	for _, item := range claudeConfigProtectedItems {
		if isFileName(item) {
			itemSuffixes = append(itemSuffixes, "/"+item)
		} else {
			itemSuffixes = append(itemSuffixes, "/"+item+"/**")
		}
	}

	entries := make([]string, 0, len(AgencDenyPermissionTools)*len(baseVariants)*len(itemSuffixes))
	for _, tool := range AgencDenyPermissionTools {
		for _, base := range baseVariants {
			for _, suffix := range itemSuffixes {
				entries = append(entries, tool+"("+base+suffix+")")
			}
		}
	}
	return entries
}

// isFileName returns true if the name looks like a file (contains a dot
// indicating an extension) rather than a directory.
func isFileName(name string) bool {
	return strings.Contains(name, ".")
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
