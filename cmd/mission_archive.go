package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/kurtosis-tech/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var missionArchiveCmd = &cobra.Command{
	Use:   "archive <mission-id>",
	Short: "Archive a mission",
	Args:  cobra.ExactArgs(1),
	RunE:  runMissionArchive,
}

func init() {
	missionCmd.AddCommand(missionArchiveCmd)
}

func runMissionArchive(cmd *cobra.Command, args []string) error {
	missionID := args[0]

	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	// Verify mission exists
	_, err = db.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	// Move the mission directory to ARCHIVE
	missionsDirpath := config.GetMissionsDirpath(agencDirpath)
	archiveDirpath := config.GetArchiveDirpath(agencDirpath)

	srcDirpath := filepath.Join(missionsDirpath, missionID)
	destDirpath := filepath.Join(archiveDirpath, missionID)

	if _, err := os.Stat(srcDirpath); err == nil {
		if err := os.Rename(srcDirpath, destDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to move mission directory to archive")
		}
	}

	// Update status in database
	if err := db.ArchiveMission(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to archive mission in database")
	}

	fmt.Printf("Archived mission: %s\n", missionID)
	return nil
}
