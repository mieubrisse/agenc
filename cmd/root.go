package cmd

import (
	"github.com/spf13/cobra"
)

var agencDirpath string

var rootCmd = &cobra.Command{
	Use:          agencCmdStr,
	Short:        "The AgenC — agent mission management CLI",
	SilenceUsage: true,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

// GetRootCmd returns the root command for documentation generation.
func GetRootCmd() *cobra.Command {
	return rootCmd
}
