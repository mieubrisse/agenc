package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

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
	client, err := serverClient()
	if err != nil {
		return err
	}

	repos, err := client.ListRepos()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list repos")
	}

	if len(repos) == 0 {
		fmt.Println("No repositories in the repo library.")
		return nil
	}

	tbl := tableprinter.NewTable("REPO", "SYNCED")
	for _, r := range repos {
		synced := formatCheckmark(r.Synced)
		tbl.AddRow(displayGitRepo(r.Name), synced)
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
