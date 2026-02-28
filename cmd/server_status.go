package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
)

var serverStatusCmd = &cobra.Command{
	Use:   statusCmdStr,
	Short: "Check AgenC server status",
	RunE:  runServerStatus,
}

func init() {
	serverCmd.AddCommand(serverStatusCmd)
}

func runServerStatus(cmd *cobra.Command, args []string) error {
	// Use ensureConfigured directly — skip the version check that
	// getAgencContext performs, since server commands manage the server directly.
	if _, err := ensureConfigured(); err != nil {
		return err
	}

	pidFilepath := config.GetServerPIDFilepath(agencDirpath)
	pid, err := server.ReadPID(pidFilepath)
	if err != nil {
		return err
	}

	if pid > 0 && server.IsRunning(pidFilepath) {
		fmt.Printf("Server is running (PID %d).\n", pid)
	} else {
		fmt.Println("Server is not running.")
	}

	return nil
}
