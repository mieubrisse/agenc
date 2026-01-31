package cmd

import (
	"github.com/spf13/cobra"
)

var templateCmd = &cobra.Command{
	Use:   "template",
	Short: "Manage agent templates",
}

func init() {
	rootCmd.AddCommand(templateCmd)
}
