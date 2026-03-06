package claudeconfig

import (
	_ "embed"
	"strings"

	"github.com/odyssey/agenc/internal/config"
)

//go:embed agent_instructions.md
var agentInstructionsContent string

// GetAgentInstructions returns the hardcoded agent operating instructions with
// dynamic values substituted. These instructions are prepended to every
// mission's CLAUDE.md to give agents foundational context about AgenC, missions,
// their workspace, and how to spawn other agents.
func GetAgentInstructions(agencDirpath string) string {
	repoLibraryDirpath := config.GetReposDirpath(agencDirpath)

	r := strings.NewReplacer(
		"{{CLI_NAME}}", config.CLIName,
		"{{MISSION_UUID_ENV_VAR}}", config.MissionUUIDEnvVar,
		"{{REPO_LIBRARY_DIRPATH}}", repoLibraryDirpath,
	)

	return r.Replace(agentInstructionsContent)
}
