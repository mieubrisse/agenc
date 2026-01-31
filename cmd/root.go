package cmd

import (
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var agencDirpath string

var rootCmd = &cobra.Command{
	Use:   "agenc",
	Short: "The AgenC — agent mission management CLI",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		dirpath, err := config.GetAgencDirpath()
		if err != nil {
			return stacktrace.Propagate(err, "failed to get agenc directory path")
		}
		agencDirpath = dirpath

		if err := config.EnsureDirStructure(agencDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to ensure directory structure")
		}

		if !isUnderDaemonCmd(cmd) {
			checkDaemonVersion(agencDirpath)
		}

		return nil
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}
