package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var missionReloadCmd = &cobra.Command{
	Use:   reloadCmdStr + " [mission-id...]",
	Short: "Reload one or more missions in-place (preserves tmux pane)",
	Long: `Reload one or more missions in-place (preserves tmux pane).

Stops the mission wrapper and restarts it in the same tmux pane, preserving
window position, title, and conversation state. This is useful after updating
the mission config or upgrading the agenc binary.

Without arguments, opens an interactive fzf picker showing running missions.
With arguments, accepts a mission ID (short 8-char hex or full UUID).

For missions running in tmux, uses 'remain-on-exit' and 'respawn-pane' to
preserve the exact pane position. For missions not in tmux, falls back to
stop + resume workflow.`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionReload,
}

func init() {
	missionCmd.AddCommand(missionReloadCmd)
}

func runMissionReload(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	input := strings.Join(args, " ")

	// When a mission ID is provided, resolve and reload directly without
	// calling ListMissions (which queries every wrapper over HTTP).
	if input != "" {
		if !looksLikeMissionID(input) {
			return stacktrace.NewError("not a valid mission ID: %s", input)
		}
		missionID, err := client.ResolveMissionID(input)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve mission ID")
		}
		if err := client.ReloadMission(missionID, ""); err != nil {
			return stacktrace.Propagate(err, "failed to reload mission %s", database.ShortID(missionID))
		}
		fmt.Printf("Mission '%s' reloaded\n", database.ShortID(missionID))
		return nil
	}

	// No args: list running missions and show fzf picker
	missions, err := client.ListMissions(false, "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	runningMissions := filterRunningMissions(missions)
	if len(runningMissions) == 0 {
		fmt.Println("No running missions to reload.")
		return nil
	}

	entries := buildMissionPickerEntries(runningMissions, defaultPromptMaxLen)

	result, err := Resolve(input, Resolver[missionPickerEntry]{
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Session, e.Repo}
		},
		FzfPrompt:   "Select missions to reload (TAB to multi-select): ",
		FzfHeaders:  []string{"LAST ACTIVE", "ID", "SESSION", "REPO"},
		MultiSelect: true,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	for _, entry := range result.Items {
		if err := client.ReloadMission(entry.MissionID, ""); err != nil {
			return stacktrace.Propagate(err, "failed to reload mission %s", entry.ShortID)
		}
		fmt.Printf("Mission '%s' reloaded\n", database.ShortID(entry.MissionID))
	}
	return nil
}
