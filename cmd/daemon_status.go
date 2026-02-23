package cmd

import (
	"github.com/spf13/cobra"
)

var daemonStatusCmd = &cobra.Command{
	Use:   statusCmdStr,
	Short: "Show daemon status (deprecated: use 'server status')",
	RunE:  runDaemonStatus,
}

func init() {
	daemonCmd.AddCommand(daemonStatusCmd)
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	printDaemonDeprecation()
	return runServerStatus(cmd, args)
}
