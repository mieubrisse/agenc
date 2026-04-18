package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/mission"
	"github.com/odyssey/agenc/internal/repo"
)

var repoMvCmd = &cobra.Command{
	Use:   mvCmdStr + " <old-name> <new-name>",
	Short: "Rename a repository in the repo library",
	Long: `Rename a repository in the repo library after a GitHub rename or ownership transfer.

Moves the cloned repo directory, migrates per-repo config (emoji, title,
always-synced, etc.), and updates the clone's origin remote URL.

Does NOT update existing missions that reference the old name.

Accepts any of these formats for both arguments:
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - URL
  git@github.com:owner/repo.git        - SSH URL

Example:
  agenc repo mv old-owner/my-repo new-owner/my-repo`,
	Args: cobra.ExactArgs(2),
	RunE: runRepoMv,
}

func init() {
	repoCmd.AddCommand(repoMvCmd)
}

func runRepoMv(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	defaultOwner := repo.GetDefaultGitHubUser()

	oldName, _, err := mission.ParseRepoReference(args[0], false, defaultOwner)
	if err != nil {
		return stacktrace.Propagate(err, "invalid old repo reference '%s'", args[0])
	}

	newName, _, err := mission.ParseRepoReference(args[1], false, defaultOwner)
	if err != nil {
		return stacktrace.Propagate(err, "invalid new repo reference '%s'", args[1])
	}

	if err := client.MoveRepo(oldName, newName); err != nil {
		return stacktrace.Propagate(err, "failed to move repo '%s' to '%s'", oldName, newName)
	}

	fmt.Printf("Moved '%s' -> '%s'\n", oldName, newName)
	return nil
}
