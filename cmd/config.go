package cmd

import (
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   configCmdStr,
	Short: "Manage agenc configuration",
}

func init() {
	rootCmd.AddCommand(configCmd)
}
