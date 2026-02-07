package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var repoEditCmd = &cobra.Command{
	Use:   editCmdStr + " [search-terms...]",
	Short: "Edit a repo via a new mission with a repo copy",
	Long: `Edit a repo via a new mission with a repo copy.

Positional arguments select a repo. They can be:
  - A git reference (URL, shorthand like owner/repo, or local path)
  - Search terms to match against your repo library ("my repo")`,
	Args: cobra.ArbitraryArgs,
	RunE: runRepoEdit,
}

func init() {
	repoCmd.AddCommand(repoEditCmd)
}

func runRepoEdit(cmd *cobra.Command, args []string) error {
	ensureDaemonRunning(agencDirpath)

	repos, err := findReposOnDisk(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to scan repos directory")
	}

	if len(repos) == 0 {
		fmt.Printf("No repos in library. Add repos with: %s %s %s owner/repo\n", agencCmdStr, repoCmdStr, addCmdStr)
		return stacktrace.NewError("no repos available to edit")
	}

	result, err := Resolve(strings.Join(args, " "), Resolver[string]{
		TryCanonical: func(input string) (string, bool, error) {
			if !looksLikeRepoReference(input) {
				return "", false, nil
			}
			// For repo edit, use ResolveRepoInput which handles cloning from external sources
			resolved, err := ResolveRepoInput(agencDirpath, input, "Select repo: ")
			if err != nil {
				return "", false, err
			}
			return resolved.RepoName, true, nil
		},
		GetItems:    func() ([]string, error) { return repos, nil },
		ExtractText: func(repo string) string { return repo },
		FormatRow:   func(repo string) []string { return []string{displayGitRepo(repo)} },
		FzfPrompt:   "Select repo: ",
		FzfHeaders:  []string{"REPO"},
		MultiSelect: false,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled {
		return stacktrace.NewError("no repo selected")
	}

	if len(result.Items) == 0 {
		return stacktrace.NewError("no repo selected")
	}

	repoName := result.Items[0]

	// Print auto-select message if search matched exactly one
	if len(args) > 0 && !looksLikeRepoReference(strings.Join(args, " ")) {
		fmt.Printf("Auto-selected: %s\n", displayGitRepo(repoName))
	}

	cloneDirpath := config.GetRepoDirpath(agencDirpath, repoName)
	return createAndLaunchMission(agencDirpath, repoName, cloneDirpath, "")
}
