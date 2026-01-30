package cmd

import (
	"github.com/spf13/cobra"
)

var missionCmd = &cobra.Command{
	Use:   "mission",
	Short: "Manage agent missions",
}

func init() {
	rootCmd.AddCommand(missionCmd)
}
