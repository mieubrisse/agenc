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

//go:embed adjutant_claude.md
var adjutantClaudeMdContent string

// writeAdjutantAgentConfig writes adjutant-specific project-level config into
// the agent directory. This includes:
//   - agent/CLAUDE.md with adjutant instructions
//   - agent/.claude/settings.json with adjutant permissions
//
// These are project-level files that Claude Code picks up from the working
// directory, separate from the global config in claude-config/.
func writeAdjutantAgentConfig(agentDirpath string, agencDirpath string) error {
	// Write adjutant CLAUDE.md to agent/CLAUDE.md, replacing placeholders
	// with actual values so the env var name stays in sync with the constant.
	claudeMdContent := strings.ReplaceAll(adjutantClaudeMdContent,
		"{{MISSION_UUID_ENV_VAR}}", config.MissionUUIDEnvVar)
	claudeMdFilepath := filepath.Join(agentDirpath, "CLAUDE.md")
	if err := os.WriteFile(claudeMdFilepath, []byte(claudeMdContent), 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write adjutant CLAUDE.md to agent directory")
	}

	// Write adjutant settings.json to agent/.claude/settings.json
	claudeDirpath := filepath.Join(agentDirpath, config.UserClaudeDirname)
	if err := os.MkdirAll(claudeDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create agent .claude directory")
	}

	settingsData, err := buildAdjutantProjectSettings(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to build adjutant project settings")
	}

	settingsFilepath := filepath.Join(claudeDirpath, "settings.json")
	if err := os.WriteFile(settingsFilepath, settingsData, 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write adjutant settings to agent directory")
	}

	return nil
}

// buildAdjutantProjectSettings creates a minimal settings.json containing
// adjutant-specific permissions and a SessionStart hook that runs `agenc prime`
// to inject the CLI quick reference into the agent's context.
// Claude Code merges project-level settings with global settings, so only the
// adjutant additions are needed here.
func buildAdjutantProjectSettings(agencDirpath string) ([]byte, error) {
	settings := map[string]interface{}{
		"permissions": map[string]interface{}{
			"allow": BuildAdjutantAllowEntries(agencDirpath),
			"deny":  BuildAdjutantDenyEntries(agencDirpath),
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
		return nil, stacktrace.Propagate(err, "failed to marshal adjutant project settings")
	}
	result = append(result, '\n')

	return result, nil
}

// BuildAdjutantAllowEntries returns permission allow entries for adjutant
// missions. These grant read/write access to the agenc data directory and
// permission to run agenc commands.
func BuildAdjutantAllowEntries(agencDirpath string) []string {
	agencPattern := agencDirpath + "/**"
	readWriteTools := []string{"Read", "Write", "Edit", "Glob", "Grep"}

	entries := make([]string, 0, len(readWriteTools)+1)
	for _, tool := range readWriteTools {
		entries = append(entries, tool+"("+agencPattern+")")
	}
	entries = append(entries, "Bash(agenc:*)")
	entries = append(entries, "Bash(gh:*)")

	return entries
}

// BuildAdjutantDenyEntries returns permission deny entries for adjutant
// missions. These prevent writing to other missions' agent directories
// while allowing read access.
func BuildAdjutantDenyEntries(agencDirpath string) []string {
	agentPattern := agencDirpath + "/" + config.MissionsDirname + "/*/" + config.AgentDirname + "/**"
	writeTools := []string{"Write", "Edit"}

	entries := make([]string, 0, len(writeTools))
	for _, tool := range writeTools {
		entries = append(entries, tool+"("+agentPattern+")")
	}

	return entries
}
