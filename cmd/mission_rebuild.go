package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/wrapper"
)

const (
	rebuildClientTimeout = 120 * time.Second // rebuilds can take a while
)

var missionRebuildCmd = &cobra.Command{
	Use:   rebuildCmdStr + " <mission-id>",
	Short: "Rebuild the devcontainer for a containerized mission",
	Long: `Rebuild the devcontainer for a containerized mission.

Stops the current Claude process, tears down and rebuilds the container from
the latest devcontainer.json, then restarts Claude. Only works for missions
whose repository has a devcontainer.json.

Accepts a mission ID (short 8-char hex or full UUID).`,
	Args: cobra.ExactArgs(1),
	RunE: runMissionRebuild,
}

func init() {
	missionCmd.AddCommand(missionRebuildCmd)
}

func runMissionRebuild(cmd *cobra.Command, args []string) error {
	input := strings.TrimSpace(args[0])

	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine agenc directory")
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	missionID, err := client.ResolveMissionID(input)
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve mission ID")
	}

	socketFilepath := config.GetMissionSocketFilepath(agencDirpath, missionID)
	wrapperClient := wrapper.NewWrapperClient(socketFilepath, rebuildClientTimeout)

	fmt.Printf("Rebuilding devcontainer for mission '%s'...\n", database.ShortID(missionID))

	if err := wrapperClient.Rebuild(); err != nil {
		return stacktrace.Propagate(err, "failed to rebuild devcontainer")
	}

	fmt.Printf("Mission '%s' rebuilt successfully\n", database.ShortID(missionID))
	return nil
}
