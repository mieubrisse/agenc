package cmd

import (
	"github.com/spf13/cobra"
)

var repoCmd = &cobra.Command{
	Use:   repoCmdStr,
	Short: "Manage the repo library",
}

func init() {
	rootCmd.AddCommand(repoCmd)
}
