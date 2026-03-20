package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
)

var serverStopCmd = &cobra.Command{
	Use:   stopCmdStr,
	Short: "Stop the AgenC server",
	RunE:  runServerStop,
}

func init() {
	serverCmd.AddCommand(serverStopCmd)
}

func runServerStop(cmd *cobra.Command, args []string) error {
	// Use ensureConfigured directly — skip the version check since
	// we're about to stop the server anyway.
	agencDirpath, err := ensureConfigured()
	if err != nil {
		return err
	}

	pidFilepath := config.GetServerPIDFilepath(agencDirpath)

	// Always run StopServer — it handles missing/stale PID files gracefully
	// and sweeps for orphaned server processes regardless.
	if err := server.StopServer(pidFilepath); err != nil {
		return err
	}

	fmt.Println("Server stopped.")
	return nil
}
