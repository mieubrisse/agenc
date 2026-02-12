package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var missionDescribeCmd = &cobra.Command{
	Use:   describeCmdStr + " <mission-id> <description>",
	Short: "Set or update a mission's description",
	Long: `Set or update a mission's description.

The description is a human-readable label that appears in mission listings,
making it easy to identify what each mission is working on.

To clear a description, pass an empty string: agenc mission describe <id> ""`,
	Args: cobra.MinimumNArgs(1),
	RunE: runMissionDescribe,
}

func init() {
	missionCmd.AddCommand(missionDescribeCmd)
}

func runMissionDescribe(cmd *cobra.Command, args []string) error {
	if len(args) < 2 {
		return stacktrace.NewError("usage: %s %s %s <mission-id> <description>", agencCmdStr, missionCmdStr, describeCmdStr)
	}

	missionIDArg := args[0]
	description := strings.Join(args[1:], " ")

	return resolveAndRunForMission(missionIDArg, func(db *database.DB, missionID string) error {
		if err := db.UpdateMissionDescription(missionID, description); err != nil {
			return stacktrace.Propagate(err, "failed to update description")
		}

		shortID := database.ShortID(missionID)
		if description == "" {
			fmt.Printf("Cleared description for mission %s\n", shortID)
		} else {
			fmt.Printf("Updated description for mission %s: %s\n", shortID, description)
		}
		return nil
	})
}
