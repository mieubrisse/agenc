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
	return selectMissionsInteractive(db, missionSelectConfig{
		IncludeArchived: false,
		EmptyMessage:    "No active missions to archive.",
		Prompt:          "Select missions to archive (TAB to multi-select): ",
	})
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
