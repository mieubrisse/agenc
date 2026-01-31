package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var inspectDirFlag bool

var missionInspectCmd = &cobra.Command{
	Use:   "inspect <mission-id>",
	Short: "Print information about a mission",
	Args:  cobra.ExactArgs(1),
	RunE:  runMissionInspect,
}

func init() {
	missionInspectCmd.Flags().BoolVar(&inspectDirFlag, "dir", false, "print only the mission directory path")
	missionCmd.AddCommand(missionInspectCmd)
}

func runMissionInspect(cmd *cobra.Command, args []string) error {
	missionID := args[0]

	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	mission, err := db.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	if mission.Status == "archived" {
		missionDirpath = filepath.Join(config.GetArchiveDirpath(agencDirpath), missionID)
	}

	if inspectDirFlag {
		fmt.Println(missionDirpath)
		return nil
	}

	description, err := db.GetMissionDescription(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission description")
	}

	fmt.Printf("ID:          %s\n", mission.ID)
	fmt.Printf("Status:      %s\n", mission.Status)
	fmt.Printf("Agent:       %s\n", displayAgentTemplate(mission.AgentTemplate))
	if description != nil {
		fmt.Printf("Description: %s\n", description.Description)
	}
	fmt.Printf("Prompt:      %s\n", mission.Prompt)
	fmt.Printf("Directory:   %s\n", missionDirpath)
	fmt.Printf("Created:     %s\n", mission.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated:     %s\n", mission.UpdatedAt.Format("2006-01-02 15:04:05"))

	return nil
}
