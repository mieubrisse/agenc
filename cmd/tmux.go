package cmd

import (
	"github.com/spf13/cobra"
)

var tmuxCmd = &cobra.Command{
	Use:   tmuxCmdStr,
	Short: "Manage the AgenC tmux session",
}

func init() {
	rootCmd.AddCommand(tmuxCmd)
}
