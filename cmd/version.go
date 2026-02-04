package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   versionCmdStr,
	Short: "Print the agenc version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("%s version %s\n", agencCmdStr, version.Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
