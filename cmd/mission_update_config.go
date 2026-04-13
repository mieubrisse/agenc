package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/devcontainer"
	"github.com/odyssey/agenc/internal/server"
)

var updateConfigAllFlag bool

var missionUpdateConfigCmd = &cobra.Command{
	Use:     reconfigCmdStr + " [mission-id]",
	Aliases: []string{updateCmdStr, updateConfigCmdStr},
	Short:   "Apply your latest ~/.claude config to a mission",
	Long: fmt.Sprintf(`Apply your latest ~/.claude config to a mission.

Each mission gets a snapshot of your ~/.claude configuration (CLAUDE.md,
settings.json, skills, hooks, etc.) at creation time. When you change
~/.claude, existing missions keep their old config until you reconfig them.

This command rebuilds a mission's config directory from your current
~/.claude state. Running missions must be restarted to pick up the changes.

Use '%s %s %s -a' to see which missions are behind and by how many commits.
Use --%s to reconfig all non-archived missions at once.`,
		agencCmdStr, missionCmdStr, lsCmdStr, allFlagName),
	Args: cobra.ArbitraryArgs,
	RunE: runMissionUpdateConfig,
}

func init() {
	missionUpdateConfigCmd.Flags().BoolVar(&updateConfigAllFlag, allFlagName, false, "reconfig all non-archived missions")
	missionCmd.AddCommand(missionUpdateConfigCmd)
}

func runMissionUpdateConfig(cmd *cobra.Command, args []string) error {
	if _, err := ensureConfigured(); err != nil {
		return err
	}

	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}

	// Ensure shadow repo is initialized
	if err := claudeconfig.EnsureShadowRepo(agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to ensure shadow repo")
	}

	newCommitHash := claudeconfig.GetShadowRepoCommitHash(agencDirpath)

	client, err := serverClient()
	if err != nil {
		return err
	}

	if updateConfigAllFlag {
		return updateConfigForAllMissions(client, newCommitHash)
	}

	input := strings.Join(args, " ")

	// When a mission ID is provided, resolve and reconfig directly without
	// calling ListMissions (which queries every wrapper over HTTP).
	if input != "" {
		if !looksLikeMissionID(input) {
			return stacktrace.NewError("not a valid mission ID: %s", input)
		}
		missionID, err := client.ResolveMissionID(input)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve mission ID")
		}
		return updateMissionConfig(client, missionID, newCommitHash)
	}

	// No args: list all missions and show fzf picker
	missions, err := client.ListMissions(false, "", "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No missions.")
		return nil
	}

	entries := buildMissionPickerEntries(missions, defaultPromptMaxLen)

	result, err := Resolve(input, Resolver[missionPickerEntry]{
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Status, e.Session, e.Repo}
		},
		FzfPrompt:  "Select mission to reconfig: ",
		FzfHeaders: []string{"LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO"},
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	return updateMissionConfig(client, result.Items[0].MissionID, newCommitHash)
}

// updateConfigForAllMissions updates the Claude config for all non-archived
// missions that have a per-mission config directory.
func updateConfigForAllMissions(client *server.Client, newCommitHash string) error {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}
	missions, err := client.ListMissions(false, "", "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	var updatedCount int
	for _, m := range missions {
		missionConfigDirpath := filepath.Join(
			config.GetMissionDirpath(agencDirpath, m.ID),
			claudeconfig.MissionClaudeConfigDirname,
		)
		if _, err := os.Stat(missionConfigDirpath); os.IsNotExist(err) {
			continue // Legacy mission without per-mission config
		}

		if err := updateMissionConfig(client, m.ID, newCommitHash); err != nil {
			fmt.Printf("  Failed to update mission %s: %v\n", m.ShortID, err)
			continue
		}
		updatedCount++
	}

	fmt.Printf("\nUpdated %d mission(s).\n", updatedCount)
	return nil
}

// updateMissionConfig rebuilds a single mission's Claude config directory
// from the shadow repo.
func updateMissionConfig(client *server.Client, missionID string, newCommitHash string) error {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}
	missionRecord, err := client.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	// Read current pinned commit from DB
	var currentCommitHash string
	if missionRecord.ConfigCommit != nil {
		currentCommitHash = *missionRecord.ConfigCommit
	}

	if currentCommitHash == newCommitHash && newCommitHash != "" {
		fmt.Printf("Mission %s: config already up to date (commit %s)\n",
			missionRecord.ShortID, shortHash(newCommitHash))
		return nil
	}

	// Show diff if we have both commits
	if currentCommitHash != "" && newCommitHash != "" {
		showShadowRepoDiff(currentCommitHash, newCommitHash)
	}

	fmt.Printf("Updating config for mission %s...\n", missionRecord.ShortID)

	// Look up MCP trust config for this repo
	var trustedMcpServers *config.TrustedMcpServers
	if missionRecord.GitRepo != "" {
		cfg, _, cfgErr := config.ReadAgencConfig(agencDirpath)
		if cfgErr == nil {
			if rc, ok := cfg.GetRepoConfig(missionRecord.GitRepo); ok {
				trustedMcpServers = rc.TrustedMcpServers
			}
		}
	}

	// Detect containerization from the mission's repo
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)
	_, isContainerized := devcontainer.DetectDevcontainer(agentDirpath)

	// Rebuild per-mission config directory from shadow repo
	if err := claudeconfig.BuildMissionConfigDir(agencDirpath, missionID, trustedMcpServers, isContainerized); err != nil {
		return stacktrace.Propagate(err, "failed to rebuild config for mission '%s'", missionID)
	}

	// Update config_commit via server
	if newCommitHash != "" {
		if err := client.UpdateMission(missionID, server.UpdateMissionRequest{
			ConfigCommit: &newCommitHash,
		}); err != nil {
			return stacktrace.Propagate(err, "failed to update config_commit")
		}
	}

	fmt.Printf("Mission %s: config updated", missionRecord.ShortID)
	if newCommitHash != "" {
		fmt.Printf(" (commit %s)", shortHash(newCommitHash))
	}
	fmt.Println()

	if isMissionRunning(getMissionStatus(missionID, missionRecord.Status, missionRecord.ClaudeState)) {
		fmt.Printf("  Note: restart the mission to pick up config changes\n")
	}

	return nil
}

// showShadowRepoDiff displays a git diff between two commits in the shadow repo.
func showShadowRepoDiff(oldCommit string, newCommit string) {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), gitOperationTimeout)
	defer cancel()

	shadowDirpath := claudeconfig.GetShadowRepoDirpath(agencDirpath)

	diffCmd := exec.CommandContext(ctx, "git", "diff", "--stat", oldCommit, newCommit)
	diffCmd.Dir = shadowDirpath
	diffCmd.Stdout = os.Stdout
	diffCmd.Stderr = os.Stderr

	fmt.Printf("\nConfig changes (%s..%s):\n", shortHash(oldCommit), shortHash(newCommit))
	if err := diffCmd.Run(); err != nil {
		fmt.Println("  (could not generate diff)")
	}
	fmt.Println()
}

// shortHash returns the first 12 characters of a commit hash, or the full
// string if shorter.
func shortHash(hash string) string {
	if len(hash) > 12 {
		return hash[:12]
	}
	return hash
}
