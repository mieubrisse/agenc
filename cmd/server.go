package cmd

import "github.com/spf13/cobra"

var serverCmd = &cobra.Command{
	Use:   serverCmdStr,
	Short: "Manage the AgenC server",
}

func init() {
	rootCmd.AddCommand(serverCmd)
}
