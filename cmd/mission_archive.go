package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var missionArchiveCmd = &cobra.Command{
	Use:   archiveCmdStr + " [mission-id...]",
	Short: "Stop and archive one or more missions",
	Args:  cobra.ArbitraryArgs,
	RunE:  runMissionArchive,
}

func init() {
	missionCmd.AddCommand(missionArchiveCmd)
}

func runMissionArchive(cmd *cobra.Command, args []string) error {
	return resolveAndRunForEachMission(args, selectMissionsToArchive, archiveMission)
}

func selectMissionsToArchive(db *database.DB) ([]string, error) {
	missions, err := db.ListMissions(false)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list active missions")
	}

	if len(missions) == 0 {
		fmt.Println("No active missions to archive.")
		return nil, nil
	}

	entries, err := buildMissionPickerEntries(db, missions)
	if err != nil {
		return nil, err
	}

	selected, err := selectMissionsFzf(entries, "Select missions to archive (TAB to multi-select): ", true, "")
	if err != nil {
		return nil, err
	}

	return extractMissionShortIDs(selected), nil
}

func archiveMission(db *database.DB, missionID string) error {
	// Verify mission exists
	_, err := db.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	// Stop the wrapper if running (idempotent)
	if err := stopMissionWrapper(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to stop wrapper for mission '%s'", missionID)
	}

	if err := db.ArchiveMission(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to archive mission in database")
	}

	fmt.Printf("Archived mission: %s\n", database.ShortID(missionID))
	return nil
}
