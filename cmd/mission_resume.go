package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/wrapper"
)

var missionResumeCmd = &cobra.Command{
	Use:   "resume <mission-id>",
	Short: "Resume an existing mission with claude --continue",
	Args:  cobra.ExactArgs(1),
	RunE:  runMissionResume,
}

func init() {
	missionCmd.AddCommand(missionResumeCmd)
}

func runMissionResume(cmd *cobra.Command, args []string) error {
	missionID := args[0]

	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	missionRecord, err := db.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	if missionRecord.Status == "archived" {
		// Move directory back from archive to missions
		archiveDirpath := config.GetArchiveDirpath(agencDirpath)
		missionsDirpath := config.GetMissionsDirpath(agencDirpath)
		srcDirpath := filepath.Join(archiveDirpath, missionID)
		destDirpath := filepath.Join(missionsDirpath, missionID)

		if _, err := os.Stat(srcDirpath); err == nil {
			if err := os.Rename(srcDirpath, destDirpath); err != nil {
				return stacktrace.Propagate(err, "failed to move mission directory out of archive")
			}
		}

		if err := db.UnarchiveMission(missionID); err != nil {
			return stacktrace.Propagate(err, "failed to unarchive mission")
		}
		fmt.Printf("Unarchived mission: %s\n", missionID)
	}

	// Check if the wrapper is already running for this mission
	pidFilepath := config.GetMissionPIDFilepath(agencDirpath, missionID)
	pid, err := daemon.ReadPID(pidFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read mission PID file")
	}
	if daemon.IsProcessRunning(pid) {
		return stacktrace.NewError("mission '%s' is already running (wrapper PID %d)", missionID, pid)
	}

	// Check for old-format mission (no agent/ subdirectory)
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)
	if _, err := os.Stat(agentDirpath); os.IsNotExist(err) {
		return stacktrace.NewError(
			"mission '%s' uses the old directory format (no agent/ subdirectory); "+
				"please archive it with 'agenc mission archive %s' and create a new mission",
			missionID, missionID,
		)
	}

	fmt.Printf("Resuming mission: %s\n", missionID)
	fmt.Println("Launching claude --continue...")

	w := wrapper.NewWrapper(agencDirpath, missionID, missionRecord.AgentTemplate)
	return w.Run("", true)
}
