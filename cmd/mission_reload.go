package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var missionReloadCmd = &cobra.Command{
	Use:   reloadCmdStr + " <mission-id>",
	Short: "Stop a running mission and immediately resume it",
	Long: `Stop a running mission and immediately resume it with claude --continue.

This is equivalent to running 'mission stop' followed by 'mission resume' on
the same mission. Requires a mission ID (short or full UUID).`,
	Args: cobra.ExactArgs(1),
	RunE: runMissionReload,
}

func init() {
	missionCmd.AddCommand(missionReloadCmd)
}

func runMissionReload(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missionID, err := db.ResolveMissionID(args[0])
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve mission ID")
	}

	fmt.Printf("Reloading mission: %s\n", database.ShortID(missionID))

	if err := stopMissionWrapper(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to stop mission")
	}

	return resumeMission(db, missionID)
}
