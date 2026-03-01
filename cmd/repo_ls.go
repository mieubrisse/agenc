package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/repo"
	"github.com/odyssey/agenc/internal/tableprinter"
)

var repoLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List repositories in the repo library",
	RunE:  runRepoLs,
}

func init() {
	repoCmd.AddCommand(repoLsCmd)
}

func runRepoLs(cmd *cobra.Command, args []string) error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	reposDirpath := config.GetReposDirpath(agencDirpath)
	repoNames, err := repo.FindReposOnDisk(reposDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to scan repos directory")
	}

	if len(repoNames) == 0 {
		fmt.Println("No repositories in the repo library.")
		return nil
	}

	tbl := tableprinter.NewTable("REPO", "SYNCED")
	for _, repoName := range repoNames {
		synced := formatCheckmark(cfg.IsAlwaysSynced(repoName))
		tbl.AddRow(displayGitRepo(repoName), synced)
	}
	tbl.Print()

	return nil
}

// formatCheckmark returns a checkmark or dash for boolean display.
func formatCheckmark(value bool) string {
	if value {
		return "✅"
	}
	return "--"
}
