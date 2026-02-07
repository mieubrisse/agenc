package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var missionArchiveCmd = &cobra.Command{
	Use:   archiveCmdStr + " [mission-id|search-terms...]",
	Short: "Stop and archive one or more missions",
	Long: `Stop and archive one or more missions.

Without arguments, opens an interactive fzf picker showing active missions.
With arguments, accepts a mission ID (short or full UUID) or search terms to
filter the list. If exactly one mission matches search terms, it is auto-selected.`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionArchive,
}

func init() {
	missionCmd.AddCommand(missionArchiveCmd)
}

func runMissionArchive(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No active missions to archive.")
		return nil
	}

	entries, err := buildMissionPickerEntries(db, missions)
	if err != nil {
		return err
	}

	input := strings.Join(args, " ")
	result, err := Resolve(input, Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := db.ResolveMissionID(input)
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
		GetItems:    func() ([]missionPickerEntry, error) { return entries, nil },
		ExtractText: formatMissionMatchLine,
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Session, e.Repo}
		},
		FzfPrompt:   "Select missions to archive (TAB to multi-select): ",
		FzfHeaders:  []string{"LAST ACTIVE", "ID", "SESSION", "REPO"},
		MultiSelect: true,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	// Print auto-select message only if search terms (not UUID) matched exactly one
	if input != "" && !looksLikeMissionID(input) && len(result.Items) == 1 {
		fmt.Printf("Auto-selected: %s\n", result.Items[0].ShortID)
	}

	for _, entry := range result.Items {
		if err := archiveMission(db, entry.MissionID); err != nil {
			return err
		}
	}
	return nil
}

func archiveMission(db *database.DB, missionID string) error {
	if _, err := prepareMissionForAction(db, missionID); err != nil {
		return err
	}

	if err := db.ArchiveMission(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to archive mission in database")
	}

	fmt.Printf("Archived mission: %s\n", database.ShortID(missionID))
	return nil
}
