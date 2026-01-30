package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/mission"
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
		return stacktrace.NewError("mission '%s' is archived; cannot resume", missionID)
	}

	missionDirpath := filepath.Join(config.GetMissionsDirpath(agencDirpath), missionID)

	fmt.Printf("Resuming mission: %s\n", missionID)
	fmt.Println("Launching claude --continue...")

	return mission.ExecClaudeResume(agencDirpath, missionDirpath)
}
