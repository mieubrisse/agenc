package cmd

import (
	"github.com/spf13/cobra"
)

var tmuxWindowCmd = &cobra.Command{
	Use:   windowCmdStr,
	Short: "Manage tmux windows in the AgenC session",
}

func init() {
	tmuxCmd.AddCommand(tmuxWindowCmd)
}
