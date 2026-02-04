package cmd

import (
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   daemonCmdStr,
	Short: "Manage the background daemon",
}

func init() {
	rootCmd.AddCommand(daemonCmd)
}
