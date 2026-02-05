package cmd

import (
	"fmt"
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var missionRmCmd = &cobra.Command{
	Use:   rmCmdStr + " [mission-id...]",
	Short: "Stop and permanently remove one or more missions",
	Args:  cobra.ArbitraryArgs,
	RunE:  runMissionRm,
}

func init() {
	missionCmd.AddCommand(missionRmCmd)
}

func runMissionRm(cmd *cobra.Command, args []string) error {
	return resolveAndRunForEachMission(args, selectMissionsToRemove, removeMission)
}

func selectMissionsToRemove(db *database.DB) ([]string, error) {
	return selectMissionsInteractive(db, missionSelectConfig{
		IncludeArchived: true,
		EmptyMessage:    "No missions to remove.",
		Prompt:          "Select missions to remove (TAB to multi-select): ",
		ShowStatus:      true,
	})
}

// removeMission tears down a mission in the reverse order of `mission new`:
// mission new creates DB record then directory, so we remove directory then DB record.
func removeMission(db *database.DB, missionID string) error {
	if _, err := prepareMissionForAction(db, missionID); err != nil {
		return err
	}

	// Remove the mission directory (workspace is just a directory copy, so RemoveAll handles it)
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	if _, err := os.Stat(missionDirpath); err == nil {
		if err := os.RemoveAll(missionDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to remove mission directory '%s'", missionDirpath)
		}
	}

	// Delete from database
	if err := db.DeleteMission(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to delete mission from database")
	}

	fmt.Printf("Removed mission: %s\n", database.ShortID(missionID))
	return nil
}
