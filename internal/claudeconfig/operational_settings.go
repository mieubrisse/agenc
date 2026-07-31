package claudeconfig

import (
	"encoding/json"

	"github.com/mieubrisse/stacktrace"
)

// BuildOperationalSettings returns a standalone settings.json (indented, trailing
// newline) carrying ONLY AgenC's operational plumbing, for delivery via
// `claude --settings <file>` under State Y. It does NOT merge user settings and
// does NOT rewrite paths — --settings unions with the user's ~/.claude/settings.json.
//
// hookScriptsBaseDirpath is the directory that contains the agenc-hooks/ subdir
// where the repo-library guard script lives (State Y writes it under the mission
// dir); it is used to build the PreToolUse repo-library-guard hook's absolute path.
func BuildOperationalSettings(agencDirpath string, agentDirpath string, hookScriptsBaseDirpath string) ([]byte, error) {
	settings := make(map[string]json.RawMessage)

	// Hooks: state-tracking hooks (Stop/UserPromptSubmit/Notification/PostToolUse/
	// PostToolUseFailure), SessionStart `agenc prime`, and PreToolUse repo-library guard.
	hookEntries := BuildAgencHookEntries(hookScriptsBaseDirpath)
	hooksData, err := json.Marshal(hookEntries)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal hooks entries")
	}
	settings["hooks"] = json.RawMessage(hooksData)

	// Permissions: allow agent dir access, deny repo-library writes.
	// Intentionally omits BuildClaudeConfigDenyEntries — under State Y there is
	// no per-mission claude-config snapshot to protect.
	allowEntries := BuildAgentDirAllowEntries(agentDirpath)
	allowBytes, err := json.Marshal(allowEntries)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal allow entries")
	}

	denyEntries := BuildRepoLibraryDenyEntries(agencDirpath)
	denyBytes, err := json.Marshal(denyEntries)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal deny entries")
	}

	permsMap := map[string]json.RawMessage{
		"allow": json.RawMessage(allowBytes),
		"deny":  json.RawMessage(denyBytes),
	}
	permsBytes, err := json.Marshal(permsMap)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal permissions")
	}
	settings["permissions"] = json.RawMessage(permsBytes)

	// Sandbox: add the AgenC server socket to allowUnixSockets.
	if err := mergeAgencSandbox(settings, agencDirpath); err != nil {
		return nil, stacktrace.Propagate(err, "failed to merge sandbox settings")
	}

	result, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal operational settings")
	}

	return append(result, '\n'), nil
}
