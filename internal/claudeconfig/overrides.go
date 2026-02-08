package claudeconfig

import "encoding/json"

// AgencHookEntries defines the hook entries that agenc appends to the user's
// hooks. Keys are hook event names, values are JSON arrays of hook group objects.
var AgencHookEntries = map[string]json.RawMessage{
	"Stop":             json.RawMessage(`[{"hooks":[{"type":"command","command":"echo idle > \"$CLAUDE_PROJECT_DIR/../claude-state\""}]}]`),
	"UserPromptSubmit": json.RawMessage(`[{"hooks":[{"type":"command","command":"echo busy > \"$CLAUDE_PROJECT_DIR/../claude-state\""}]}]`),
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
