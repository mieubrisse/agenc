package cmd

import (
	"github.com/spf13/cobra"
)

var detachCmd = &cobra.Command{
	Use:   detachCmdStr,
	Short: "Detach from the AgenC tmux session (alias for 'agenc tmux detach')",
	Args:  cobra.NoArgs,
	RunE:  runTmuxDetach,
}

func init() {
	rootCmd.AddCommand(detachCmd)
}
