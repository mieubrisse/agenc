package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var missionRmCmd = &cobra.Command{
	Use:   rmCmdStr + " [mission-id...]",
	Short: "Stop and permanently remove one or more missions",
	Long: `Stop and permanently remove one or more missions.

Without arguments, opens an interactive fzf picker showing all missions.
With arguments, accepts one or more mission IDs (short 8-char hex or full UUID).`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionRm,
}

func init() {
	missionCmd.AddCommand(missionRmCmd)
}

func runMissionRm(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	// When multiple args are provided and each looks like a mission ID,
	// resolve and remove each one directly without going through the picker.
	if len(args) > 1 && allLookLikeMissionIDs(args) {
		for _, idArg := range args {
			if err := client.DeleteMission(idArg); err != nil {
				return stacktrace.Propagate(err, "failed to remove mission '%s'", idArg)
			}
			fmt.Printf("Removed mission: %s\n", idArg)
		}
		return nil
	}

	missions, err := client.ListMissions(true, "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No missions to remove.")
		return nil
	}

	entries := buildMissionPickerEntries(missions, defaultPromptMaxLen)

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
			// Find the entry in our missions list
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s not found", input)
		},
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Status, e.Session, e.Repo}
		},
		FzfPrompt:         "Select missions to remove (TAB to multi-select): ",
		FzfHeaders:        []string{"LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO"},
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
		if err := client.DeleteMission(entry.MissionID); err != nil {
			return stacktrace.Propagate(err, "failed to remove mission %s", entry.ShortID)
		}
		fmt.Printf("Removed mission: %s\n", database.ShortID(entry.MissionID))
	}
	return nil
}
