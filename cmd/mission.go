package cmd

import (
	"github.com/spf13/cobra"
)

const missionCmdName = "mission"

var missionCmd = &cobra.Command{
	Use:   missionCmdName,
	Short: "Manage agent missions",
}

func init() {
	rootCmd.AddCommand(missionCmd)
}
