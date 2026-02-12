package cmd

import (
	"github.com/spf13/cobra"
)

var attachCmd = &cobra.Command{
	Use:   attachCmdStr,
	Short: "Attach to the AgenC tmux session (alias for 'agenc tmux attach')",
	Long:  tmuxAttachCmd.Long,
	Args:  cobra.NoArgs,
	RunE:  runTmuxAttach,
}

func init() {
	rootCmd.AddCommand(attachCmd)
}
