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
	if _, err := getAgencContext(); err != nil {
		return err
	}

	pidFilepath := config.GetServerPIDFilepath(agencDirpath)

	pid, err := server.ReadPID(pidFilepath)
	if err != nil {
		return err
	}

	if pid == 0 || !server.IsRunning(pidFilepath) {
		fmt.Println("Server is not running.")
		return nil
	}

	if err := server.StopServer(pidFilepath); err != nil {
		return err
	}

	fmt.Println("Server stopped.")
	return nil
}
