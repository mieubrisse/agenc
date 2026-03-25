package cmd

import "github.com/spf13/cobra"

var cronLogsCmd = &cobra.Command{
	Use:   logsCmdStr,
	Short: "View cron job logs",
}

func init() {
	cronCmd.AddCommand(cronLogsCmd)
}
