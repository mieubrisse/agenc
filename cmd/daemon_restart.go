package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var daemonRestartCmd = &cobra.Command{
	Use:   restartCmdStr,
	Short: "Restart the background daemon (deprecated: use 'server stop' then 'server start')",
	RunE:  runDaemonRestart,
}

func init() {
	daemonCmd.AddCommand(daemonRestartCmd)
}

func runDaemonRestart(cmd *cobra.Command, args []string) error {
	printDaemonDeprecation()
	if err := runServerStop(cmd, args); err != nil {
		fmt.Println("Server was not running; starting fresh.")
	}
	return runServerStart(cmd, args)
}
