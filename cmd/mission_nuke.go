package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var nukeForceFlag bool

var missionNukeCmd = &cobra.Command{
	Use:   "nuke",
	Short: "Stop and permanently remove ALL missions",
	Args:  cobra.NoArgs,
	RunE:  runMissionNuke,
}

func init() {
	missionNukeCmd.Flags().BoolVarP(&nukeForceFlag, "force", "f", false, "skip confirmation prompt")
	missionCmd.AddCommand(missionNukeCmd)
}

func runMissionNuke(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missions, err := db.ListMissions(true)
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No missions to remove.")
		return nil
	}

	if !nukeForceFlag {
		fmt.Printf("WARNING: This will permanently remove ALL %d mission(s).\n", len(missions))
		fmt.Print("Continue? [y/N] ")

		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			return stacktrace.Propagate(err, "failed to read confirmation")
		}

		if strings.TrimSpace(input) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	for _, m := range missions {
		if err := removeMission(db, m.ID); err != nil {
			return err
		}
	}

	fmt.Printf("All %d mission(s) removed.\n", len(missions))
	return nil
}
