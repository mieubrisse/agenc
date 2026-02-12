package claudeconfig

import "encoding/json"

// AgencHookEntries defines the hook entries that agenc appends to the user's
// hooks. Keys are hook event names, values are JSON arrays of hook group objects.
// Each hook calls `agenc mission send claude-update` which sends the event to
// the wrapper's unix socket for state tracking and tmux pane coloring.
var AgencHookEntries = map[string]json.RawMessage{
	"Stop":             json.RawMessage(`[{"hooks":[{"type":"command","command":"agenc mission send claude-update $AGENC_MISSION_UUID Stop"}]}]`),
	"UserPromptSubmit": json.RawMessage(`[{"hooks":[{"type":"command","command":"agenc mission send claude-update $AGENC_MISSION_UUID UserPromptSubmit"}]}]`),
	"Notification":     json.RawMessage(`[{"hooks":[{"type":"command","command":"agenc mission send claude-update $AGENC_MISSION_UUID Notification"}]}]`),
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
func BuildClaudeConfigDenyEntries(claudeConfigDirpath string) []string {
	configPattern := claudeConfigDirpath + "/**"
	entries := make([]string, 0, len(AgencDenyPermissionTools))
	for _, tool := range AgencDenyPermissionTools {
		entries = append(entries, tool+"("+configPattern+")")
	}
	return entries
}
