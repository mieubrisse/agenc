package cmd

import (
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var agencDirpath string

var rootCmd = &cobra.Command{
	Use:          agencCmdStr,
	Short:        "The AgenC — agent mission management CLI",
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		dirpath, err := config.GetAgencDirpath()
		if err != nil {
			return stacktrace.Propagate(err, "failed to get agenc directory path")
		}
		agencDirpath = dirpath

		if err := handleFirstRun(agencDirpath); err != nil {
			return stacktrace.Propagate(err, "first-run setup failed")
		}

		if err := config.EnsureDirStructure(agencDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to ensure directory structure")
		}

		// Auto-onboarding: prompt for incomplete config steps.
		// Skip for 'config init' (handles it in RunE) and daemon commands.
		if cmd != configInitCmd && !isUnderDaemonCmd(cmd) {
			if err := runOnboarding(false); err != nil {
				return stacktrace.Propagate(err, "configuration check failed")
			}
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

// GetRootCmd returns the root command for documentation generation.
func GetRootCmd() *cobra.Command {
	return rootCmd
}
