package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var serverRestartCmd = &cobra.Command{
	Use:   restartCmdStr,
	Short: "Restart the AgenC server",
	RunE:  runServerRestart,
}

func init() {
	serverCmd.AddCommand(serverRestartCmd)
}

func runServerRestart(cmd *cobra.Command, args []string) error {
	if err := runServerStop(cmd, args); err != nil {
		fmt.Println("Server was not running; starting fresh.")
	}
	return runServerStart(cmd, args)
}
