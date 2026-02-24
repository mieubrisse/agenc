package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var nukeForceFlag bool

var missionNukeCmd = &cobra.Command{
	Use:   nukeCmdStr,
	Short: "Stop and permanently remove ALL missions",
	Args:  cobra.NoArgs,
	RunE:  runMissionNuke,
}

func init() {
	missionNukeCmd.Flags().BoolVarP(&nukeForceFlag, forceFlagName, "f", false, "skip confirmation prompt")
	missionCmd.AddCommand(missionNukeCmd)
}

func runMissionNuke(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(true, "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No missions to remove.")
		return nil
	}

	if !nukeForceFlag {
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			return stacktrace.NewError("'%s %s %s' requires a terminal for confirmation; use --%s to skip",
				agencCmdStr, missionCmdStr, nukeCmdStr, forceFlagName,
			)
		}

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
		if err := client.DeleteMission(m.ID); err != nil {
			return stacktrace.Propagate(err, "failed to remove mission %s", database.ShortID(m.ID))
		}
		fmt.Printf("Removed mission: %s\n", database.ShortID(m.ID))
	}

	fmt.Printf("All %d mission(s) removed.\n", len(missions))
	return nil
}
