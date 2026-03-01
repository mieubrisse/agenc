package cmd

import "github.com/spf13/cobra"

var serverLogsCmd = &cobra.Command{
	Use:   logsCmdStr,
	Short: "View server logs",
}

func init() {
	serverCmd.AddCommand(serverLogsCmd)
}
