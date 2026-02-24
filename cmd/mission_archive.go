package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var missionArchiveCmd = &cobra.Command{
	Use:   archiveCmdStr + " [mission-id...]",
	Short: "Stop and archive one or more missions",
	Long: `Stop and archive one or more missions.

Without arguments, opens an interactive fzf picker showing active missions.
With arguments, accepts a mission ID (short 8-char hex or full UUID).`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionArchive,
}

func init() {
	missionCmd.AddCommand(missionArchiveCmd)
}

func runMissionArchive(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(false, "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No active missions to archive.")
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
			// Find the entry in our active missions list
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s is not active (may be archived)", input)
		},
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Session, e.Repo}
		},
		FzfPrompt:         "Select missions to archive (TAB to multi-select): ",
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
		if err := client.ArchiveMission(entry.MissionID); err != nil {
			return stacktrace.Propagate(err, "failed to archive mission %s", entry.ShortID)
		}
		fmt.Printf("Archived mission: %s\n", entry.ShortID)
	}
	return nil
}
