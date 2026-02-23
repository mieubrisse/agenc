package cmd

import (
	"github.com/spf13/cobra"
)

var sessionCmd = &cobra.Command{
	Use:   sessionCmdStr,
	Short: "Manage Claude Code sessions",
}

func init() {
	rootCmd.AddCommand(sessionCmd)
}
