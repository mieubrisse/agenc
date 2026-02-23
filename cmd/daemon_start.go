package cmd

import (
	"github.com/spf13/cobra"
)

var daemonStartCmd = &cobra.Command{
	Use:   startCmdStr,
	Short: "Start the background daemon (deprecated: use 'server start')",
	RunE:  runDaemonStart,
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
}

func runDaemonStart(cmd *cobra.Command, args []string) error {
	printDaemonDeprecation()
	return runServerStart(cmd, args)
}
