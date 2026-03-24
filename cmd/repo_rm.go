package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/mission"
	"github.com/odyssey/agenc/internal/repo"
)

var repoRmCmd = &cobra.Command{
	Use:   rmCmdStr + " [repo...]",
	Short: "Remove a repository from the repo library",
	Long: `Remove one or more repositories from the repo library.

Deletes the cloned repo from $AGENC_DIRPATH/repos/ and removes it from the
repoConfig in config.yml if present.

When called without arguments, opens an interactive fzf picker.

Accepts any of these formats:
  repo                                 - shorthand (requires gh auth login)
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - URL

Tip: Single-word shorthand works automatically if you're logged into gh (gh auth login)`,
	RunE: runRepoRm,
}

func init() {
	repoCmd.AddCommand(repoRmCmd)
}

func runRepoRm(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	repoResponses, err := client.ListRepos()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list repos")
	}

	if len(repoResponses) == 0 {
		fmt.Println("No repositories in the repo library.")
		return nil
	}

	// Build lookup map for synced status
	syncedMap := make(map[string]bool, len(repoResponses))
	var repoNames []string
	for _, r := range repoResponses {
		repoNames = append(repoNames, r.Name)
		syncedMap[r.Name] = r.Synced
	}

	cfg, _ := readConfig()

	result, err := Resolve(strings.Join(args, " "), Resolver[string]{
		TryCanonical: func(input string) (string, bool, error) {
			if !repo.LooksLikeRepoReference(input) {
				return "", false, nil
			}
			defaultOwner := repo.GetDefaultGitHubUser()
			name, _, err := mission.ParseRepoReference(input, false, defaultOwner)
			if err != nil {
				return "", false, stacktrace.Propagate(err, "invalid repo reference '%s'", input)
			}
			return name, true, nil
		},
		GetItems: func() ([]string, error) { return repoNames, nil },
		FormatRow: func(repoName string) []string {
			return []string{formatRepoDisplay(repoName, false, cfg)}
		},
		FzfPrompt:         "Select repos to remove (TAB to multi-select): ",
		FzfHeaders:        []string{"REPO"},
		MultiSelect:       true,
		NotCanonicalError: "not a valid repo reference",
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	for _, repoName := range result.Items {
		if syncedMap[repoName] {
			fmt.Printf("'%s' is a synced repo. Remove it? [y/N] ", repoName)
			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				return stacktrace.Propagate(err, "failed to read confirmation")
			}
			if strings.TrimSpace(input) != "y" {
				fmt.Printf("Skipped '%s'\n", repoName)
				continue
			}
		}

		if err := client.RemoveRepo(repoName); err != nil {
			return stacktrace.Propagate(err, "failed to remove repo '%s'", repoName)
		}
		fmt.Printf("Removed '%s'\n", repoName)
	}

	return nil
}
