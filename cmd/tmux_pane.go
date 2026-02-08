package cmd

import (
	"github.com/spf13/cobra"
)

var tmuxPaneCmd = &cobra.Command{
	Use:   paneCmdStr,
	Short: "Manage tmux panes in the AgenC session",
}

func init() {
	tmuxCmd.AddCommand(tmuxPaneCmd)
}
