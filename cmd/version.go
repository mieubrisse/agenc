package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the agenc version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("agenc version %s\n", version.Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
