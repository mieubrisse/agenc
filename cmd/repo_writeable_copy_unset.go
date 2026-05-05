package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
	"github.com/odyssey/agenc/internal/repo"
)

var repoWriteableCopyUnsetCmd = &cobra.Command{
	Use:   unsetCmdStr + " <repo>",
	Short: "Remove a repo's writeable-copy configuration",
	Long: `Remove the writeable-copy configuration for a repo. The on-disk clone is
NOT deleted; the user can remove it manually if desired.

The repo can be in any of the formats accepted by 'agenc repo add' — shorthand
('owner/repo'), canonical name ('github.com/owner/repo'), or full URL.`,
	Args: cobra.ExactArgs(1),
	RunE: runRepoWriteableCopyUnset,
}

func init() {
	repoWriteableCopyCmd.AddCommand(repoWriteableCopyUnsetCmd)
}

func runRepoWriteableCopyUnset(cmd *cobra.Command, args []string) error {
	rawRepoArg := args[0]

	defaultOwner := repo.GetDefaultGitHubUser()
	repoName, _, err := mission.ParseRepoReference(rawRepoArg, false, defaultOwner)
	if err != nil {
		return stacktrace.Propagate(err, "invalid repo reference '%s'", rawRepoArg)
	}

	cfg, cm, release, err := readConfigWithComments()
	if err != nil {
		return err
	}
	defer release()
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}

	rc, ok := cfg.GetRepoConfig(repoName)
	if !ok || rc.WriteableCopy == "" {
		return stacktrace.NewError("no writeable copy configured for '%s'", repoName)
	}
	previousPath := rc.WriteableCopy
	rc.WriteableCopy = ""
	cfg.SetRepoConfig(repoName, rc)

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Removed writeable-copy configuration for '%s'.\n", repoName)
	fmt.Printf("The on-disk clone at %s was NOT deleted. Remove it manually if desired.\n", previousPath)
	return nil
}
