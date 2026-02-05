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
	missions, err := db.ListMissions(true)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No missions to remove.")
		return nil, nil
	}

	entries, err := buildMissionPickerEntries(db, missions)
	if err != nil {
		return nil, err
	}

	selected, err := selectMissionsFzf(entries, "Select missions to remove (TAB to multi-select): ", true, "")
	if err != nil {
		return nil, err
	}

	return extractMissionShortIDs(selected), nil
}

// removeMission tears down a mission in the reverse order of `mission new`:
// mission new creates DB record then directory, so we remove directory then DB record.
func removeMission(db *database.DB, missionID string) error {
	// Fetch mission record to confirm it exists
	if _, err := db.GetMission(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	// Stop the wrapper if running (idempotent)
	if err := stopMissionWrapper(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to stop wrapper for mission '%s'", missionID)
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
