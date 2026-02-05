package cmd

import (
	"github.com/spf13/cobra"
)

var cronDisableCmd = &cobra.Command{
	Use:   disableCmdStr + " <name>",
	Short: "Disable a cron job",
	Args:  cobra.ExactArgs(1),
	RunE:  runCronDisable,
}

func init() {
	cronCmd.AddCommand(cronDisableCmd)
}

func runCronDisable(cmd *cobra.Command, args []string) error {
	return setCronEnabled(args[0], false)
}
