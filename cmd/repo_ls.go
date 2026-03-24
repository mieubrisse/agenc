package cmd

import (
	"fmt"
	"sort"

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

	cfg, _ := readConfig()

	type repoRow struct {
		emoji       string
		title       string
		repo        string
		synced      string
		path        string
		displayName string
		tier        int
	}

	rows := make([]repoRow, len(repos))
	for i, r := range repos {
		emoji := "--"
		title := "--"
		repoEmoji := ""
		repoTitle := ""
		if cfg != nil {
			repoEmoji = cfg.GetRepoEmoji(r.Name)
			repoTitle = cfg.GetRepoTitle(r.Name)
			if repoEmoji != "" {
				emoji = repoEmoji
			}
			if repoTitle != "" {
				title = repoTitle
			}
		}

		// Tier: 0 = title+emoji, 1 = title only, 2 = emoji only, 3 = neither
		tier := 3
		if repoTitle != "" && repoEmoji != "" {
			tier = 0
		} else if repoTitle != "" {
			tier = 1
		} else if repoEmoji != "" {
			tier = 2
		}

		displayName := plainGitRepoName(r.Name)
		if repoTitle != "" {
			displayName = repoTitle
		}

		rows[i] = repoRow{
			emoji:       emoji,
			title:       title,
			repo:        displayGitRepo(r.Name),
			synced:      formatCheckmark(r.Synced),
			path:        r.Path,
			displayName: displayName,
			tier:        tier,
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].tier != rows[j].tier {
			return rows[i].tier < rows[j].tier
		}
		return rows[i].displayName < rows[j].displayName
	})

	tbl := tableprinter.NewTable("EMOJI", "TITLE", "REPO", "SYNCED", "PATH")
	for _, row := range rows {
		tbl.AddRow(row.emoji, row.title, row.repo, row.synced, row.path)
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
