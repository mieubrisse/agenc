package claudeconfig

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

//go:embed assistant_claude.md
var assistantClaudeMdContent string

// writeAssistantAgentConfig writes assistant-specific project-level config into
// the agent directory. This includes:
//   - agent/CLAUDE.md with assistant instructions
//   - agent/.claude/settings.json with assistant permissions
//
// These are project-level files that Claude Code picks up from the working
// directory, separate from the global config in claude-config/.
func writeAssistantAgentConfig(agentDirpath string, agencDirpath string) error {
	// Write assistant CLAUDE.md to agent/CLAUDE.md, replacing placeholders
	// with actual values so the env var name stays in sync with the constant.
	claudeMdContent := strings.ReplaceAll(assistantClaudeMdContent,
		"{{MISSION_UUID_ENV_VAR}}", config.MissionUUIDEnvVar)
	claudeMdFilepath := filepath.Join(agentDirpath, "CLAUDE.md")
	if err := os.WriteFile(claudeMdFilepath, []byte(claudeMdContent), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write assistant CLAUDE.md to agent directory")
	}

	// Write assistant settings.json to agent/.claude/settings.json
	claudeDirpath := filepath.Join(agentDirpath, config.UserClaudeDirname)
	if err := os.MkdirAll(claudeDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create agent .claude directory")
	}

	settingsData, err := buildAssistantProjectSettings(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to build assistant project settings")
	}

	settingsFilepath := filepath.Join(claudeDirpath, "settings.json")
	if err := os.WriteFile(settingsFilepath, settingsData, 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write assistant settings to agent directory")
	}

	return nil
}

// buildAssistantProjectSettings creates a minimal settings.json containing
// assistant-specific permissions and a SessionStart hook that runs `agenc prime`
// to inject the CLI quick reference into the agent's context.
// Claude Code merges project-level settings with global settings, so only the
// assistant additions are needed here.
func buildAssistantProjectSettings(agencDirpath string) ([]byte, error) {
	settings := map[string]interface{}{
		"permissions": map[string]interface{}{
			"allow": BuildAssistantAllowEntries(agencDirpath),
			"deny":  BuildAssistantDenyEntries(agencDirpath),
		},
		"hooks": map[string]interface{}{
			"SessionStart": []interface{}{
				map[string]interface{}{
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": "agenc prime",
						},
					},
				},
			},
		},
	}

	result, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to marshal assistant project settings")
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
