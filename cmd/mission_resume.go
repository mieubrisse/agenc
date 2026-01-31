package cmd

import (
	"fmt"
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
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
		return stacktrace.NewError("mission '%s' is archived; cannot resume", missionID)
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
