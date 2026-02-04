package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var repoUpdateCmd = &cobra.Command{
	Use:   updateCmdStr + " [repo...]",
	Short: "Fetch and reset repos to match their remote",
	Long: `Update one or more repositories in the repo library by fetching from
origin and resetting the local default branch to match the remote.

When called without arguments, opens an interactive fzf picker.

Accepts any of these formats:
  owner/repo
  github.com/owner/repo
  https://github.com/owner/repo`,
	RunE: runRepoUpdate,
}

func init() {
	repoCmd.AddCommand(repoUpdateCmd)
}

func runRepoUpdate(cmd *cobra.Command, args []string) error {
	repoNames, err := resolveRepoArgs(args, "Select repos to update (TAB to multi-select): ")
	if err != nil {
		return err
	}
	if len(repoNames) == 0 {
		return nil
	}

	for _, repoName := range repoNames {
		repoDirpath := config.GetRepoDirpath(agencDirpath, repoName)
		fmt.Printf("Updating '%s'...\n", repoName)
		if err := mission.ForceUpdateRepo(repoDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to update '%s'", repoName)
		}
		fmt.Printf("Updated '%s'\n", repoName)
	}
	return nil
}
