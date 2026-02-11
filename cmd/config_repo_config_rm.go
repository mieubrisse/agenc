package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var configRepoConfigRmCmd = &cobra.Command{
	Use:   rmCmdStr + " <repo>",
	Short: "Remove per-repo configuration",
	Long: `Remove the configuration entry for a repository from config.yml.

This removes the entire repoConfig entry for the repo, including alwaysSynced
and windowTitle settings. The cloned repo itself is not deleted (use 'agenc repo rm'
for that).
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigRepoConfigRm,
}

func init() {
	configRepoConfigCmd.AddCommand(configRepoConfigRmCmd)
}

func runConfigRepoConfigRm(cmd *cobra.Command, args []string) error {
	repoName := args[0]

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	if !cfg.RemoveRepoConfig(repoName) {
		return stacktrace.NewError("no repo config found for '%s'", repoName)
	}

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Removed repo config for '%s'\n", repoName)
	return nil
}
