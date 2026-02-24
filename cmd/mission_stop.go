package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var missionStopCmd = &cobra.Command{
	Use:   stopCmdStr + " [mission-id...]",
	Short: "Stop one or more mission wrapper processes",
	Long: `Stop one or more mission wrapper processes.

Without arguments, opens an interactive fzf picker showing running missions.
With arguments, accepts a mission ID (short 8-char hex or full UUID).`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionStop,
}

func init() {
	missionCmd.AddCommand(missionStopCmd)
}

func runMissionStop(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(false, "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	runningMissions := filterRunningMissions(missions)
	if len(runningMissions) == 0 {
		fmt.Println("No running missions to stop.")
		return nil
	}

	entries := buildMissionPickerEntries(runningMissions, defaultPromptMaxLen)

	input := strings.Join(args, " ")
	result, err := Resolve(input, Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := client.ResolveMissionID(input)
			if err != nil {
				return missionPickerEntry{}, false, stacktrace.Propagate(err, "failed to resolve mission ID")
			}
			// Find the entry in our running missions list
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s is not running", input)
		},
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Session, e.Repo}
		},
		FzfPrompt:         "Select missions to stop (TAB to multi-select): ",
		FzfHeaders:        []string{"LAST ACTIVE", "ID", "SESSION", "REPO"},
		MultiSelect:       true,
		NotCanonicalError: "not a valid mission ID",
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	for _, entry := range result.Items {
		if err := client.StopMission(entry.MissionID); err != nil {
			return stacktrace.Propagate(err, "failed to stop mission %s", entry.ShortID)
		}
		fmt.Printf("Mission '%s' stopped.\n", database.ShortID(entry.MissionID))
	}
	return nil
}
