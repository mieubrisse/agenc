package cmd

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var repoUpdateCmd = &cobra.Command{
	Use:   updateCmdStr + " [repo...]",
	Short: "Fetch and reset repos to match their remote",
	Long: fmt.Sprintf(`Update one or more repositories in the repo library by fetching from
origin and resetting the local default branch to match the remote.

When called without arguments, opens an interactive fzf picker.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - URL

You can also use search terms to find a repo in your library:
  %s %s %s my repo            - searches for repos matching "my repo"`,
		agencCmdStr, repoCmdStr, updateCmdStr),
	RunE: runRepoUpdate,
}

func init() {
	repoCmd.AddCommand(repoUpdateCmd)
}

func runRepoUpdate(cmd *cobra.Command, args []string) error {
	allRepos, err := findReposOnDisk(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to scan repos directory")
	}

	if len(allRepos) == 0 {
		fmt.Println("No repositories in the repo library.")
		return nil
	}

	result, err := Resolve(strings.Join(args, " "), Resolver[string]{
		TryCanonical: func(input string) (string, bool, error) {
			if !looksLikeRepoReference(input) {
				return "", false, nil
			}
			name, _, err := mission.ParseRepoReference(input, false)
			if err != nil {
				return "", false, stacktrace.Propagate(err, "invalid repo reference '%s'", input)
			}
			if !slices.Contains(allRepos, name) {
				return "", false, stacktrace.NewError("repo not found: %s", name)
			}
			return name, true, nil
		},
		GetItems:    func() ([]string, error) { return allRepos, nil },
		ExtractText: func(repo string) string { return repo },
		FormatRow:   func(repo string) []string { return []string{displayGitRepo(repo)} },
		FzfPrompt:   "Select repos to update (TAB to multi-select): ",
		FzfHeaders:  []string{"REPO"},
		MultiSelect: true,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	// Print auto-select message if search matched exactly one
	if len(args) > 0 && len(result.Items) == 1 && !looksLikeRepoReference(strings.Join(args, " ")) {
		fmt.Printf("Auto-selected: %s\n", displayGitRepo(result.Items[0]))
	}

	for _, repoName := range result.Items {
		repoDirpath := config.GetRepoDirpath(agencDirpath, repoName)
		fmt.Printf("Updating '%s'...\n", repoName)
		if err := mission.ForceUpdateRepo(repoDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to update '%s'", repoName)
		}
		fmt.Printf("Updated '%s'\n", repoName)
	}
	return nil
}
