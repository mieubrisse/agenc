package cmd

import "github.com/spf13/cobra"

var stashCmd = &cobra.Command{
	Use:   stashCmdStr,
	Short: "Snapshot and restore running missions",
}

func init() {
	rootCmd.AddCommand(stashCmd)
}
