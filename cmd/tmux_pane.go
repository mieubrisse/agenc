package cmd

import (
	"github.com/spf13/cobra"
)

var tmuxPaneCmd = &cobra.Command{
	Use:        paneCmdStr,
	Short:      "Manage tmux panes in the AgenC session",
	Deprecated: "use 'tmux split-window' directly instead",
}

func init() {
	tmuxCmd.AddCommand(tmuxPaneCmd)
}
