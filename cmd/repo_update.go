package cmd

import (
	"fmt"
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
	repoNames, err := resolveRepoUpdateArgs(args)
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

// resolveRepoUpdateArgs resolves CLI arguments to canonical repo names for updating.
// Supports repo references (URLs, shorthand) and search terms.
func resolveRepoUpdateArgs(args []string) ([]string, error) {
	allRepos, err := findReposOnDisk(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to scan repos directory")
	}

	if len(allRepos) == 0 {
		fmt.Println("No repositories in the repo library.")
		return nil, nil
	}

	if len(args) == 0 {
		// No args - open fzf picker
		return selectReposWithFzf(allRepos, "Select repos to update (TAB to multi-select): ")
	}

	// Check if the first arg looks like a repo reference
	if looksLikeRepoReference(args[0]) {
		// Each arg is a repo reference
		var repoNames []string
		for _, arg := range args {
			repoName, _, parseErr := mission.ParseRepoReference(arg, false)
			if parseErr != nil {
				return nil, stacktrace.Propagate(parseErr, "invalid repo reference '%s'", arg)
			}
			repoNames = append(repoNames, repoName)
		}
		return repoNames, nil
	}

	// Treat args as search terms
	searchTerms := strings.Join(args, " ")
	terms := strings.Fields(searchTerms)

	// Match using glob-style pattern
	matches := matchReposGlob(allRepos, terms)

	if len(matches) == 1 {
		fmt.Printf("Auto-selected: %s\n", displayGitRepo(matches[0]))
		return matches, nil
	}

	// Multiple or no matches - use fzf with initial query
	return selectReposWithFzfAndQuery(allRepos, "Select repos to update (TAB to multi-select): ", searchTerms)
}
