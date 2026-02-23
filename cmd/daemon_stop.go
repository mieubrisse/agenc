package cmd

import (
	"github.com/spf13/cobra"
)

var daemonStopCmd = &cobra.Command{
	Use:   stopCmdStr,
	Short: "Stop the background daemon (deprecated: use 'server stop')",
	RunE:  runDaemonStop,
}

func init() {
	daemonCmd.AddCommand(daemonStopCmd)
}

func runDaemonStop(cmd *cobra.Command, args []string) error {
	printDaemonDeprecation()
	return runServerStop(cmd, args)
}
