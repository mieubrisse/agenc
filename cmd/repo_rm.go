package cmd

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var repoRmCmd = &cobra.Command{
	Use:   rmCmdStr + " [repo...]",
	Short: "Remove a repository from the repo library",
	Long: `Remove one or more repositories from the repo library.

Deletes the cloned repo from $AGENC_DIRPATH/repos/ and removes it from the
syncedRepos list in config.yml if present.

Refuses to remove agent template repos. Use '` + agencCmdStr + ` ` + templateCmdStr + ` ` + rmCmdStr + `' instead.

When called without arguments, opens an interactive fzf picker.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - URL

You can also use search terms to find a repo in your library:
  agenc repo rm my repo                - searches for repos matching "my repo"`,
	RunE: runRepoRm,
}

func init() {
	repoCmd.AddCommand(repoRmCmd)
}

func runRepoRm(cmd *cobra.Command, args []string) error {
	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	repoNames, err := resolveRepoRmArgs(cfg, args)
	if err != nil {
		return err
	}
	if len(repoNames) == 0 {
		return nil
	}

	for _, repoName := range repoNames {
		if err := removeSingleRepo(cfg, cm, repoName); err != nil {
			return err
		}
	}
	return nil
}

// resolveRepoRmArgs resolves CLI arguments to canonical repo names for removal.
// When no arguments are given, opens an fzf picker that excludes agent template
// repos (since those can't be removed via repo rm).
func resolveRepoRmArgs(cfg *config.AgencConfig, args []string) ([]string, error) {
	allRepos, err := findReposOnDisk(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to scan repos directory")
	}

	// Filter out agent templates — they must be removed via 'template rm' first
	var removableRepos []string
	for _, repoName := range allRepos {
		if _, isTemplate := cfg.AgentTemplates[repoName]; !isTemplate {
			removableRepos = append(removableRepos, repoName)
		}
	}

	if len(removableRepos) == 0 {
		fmt.Println("No removable repositories in the repo library (all are agent templates).")
		return nil, nil
	}

	if len(args) == 0 {
		// No args - open fzf picker
		return selectReposWithFzf(removableRepos, "Select repos to remove (TAB to multi-select): ")
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
	matches := matchReposGlob(removableRepos, terms)

	if len(matches) == 1 {
		fmt.Printf("Auto-selected: %s\n", displayGitRepo(matches[0]))
		return matches, nil
	}

	// Multiple or no matches - use fzf with initial query
	return selectReposWithFzfAndQuery(removableRepos, "Select repos to remove (TAB to multi-select): ", searchTerms)
}

func removeSingleRepo(cfg *config.AgencConfig, cm yaml.CommentMap, repoName string) error {
	if _, isTemplate := cfg.AgentTemplates[repoName]; isTemplate {
		return stacktrace.NewError(
			"'%s' is an agent template; use '%s %s %s %s' instead",
			repoName, agencCmdStr, templateCmdStr, rmCmdStr, repoName,
		)
	}

	// Check whether the repo exists (on disk or in config) before removing
	repoDirpath := config.GetRepoDirpath(agencDirpath, repoName)
	_, statErr := os.Stat(repoDirpath)
	existsOnDisk := statErr == nil
	isSynced := slices.Contains(cfg.SyncedRepos, repoName)

	if !existsOnDisk && !isSynced {
		fmt.Printf("'%s' not found\n", repoName)
		return nil
	}

	// Synced repos get an extra confirmation since the daemon actively
	// maintains them and removing one is a more significant action.
	if isSynced {
		fmt.Printf("'%s' is a synced repo. Remove it? [y/N] ", repoName)
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			return stacktrace.Propagate(err, "failed to read confirmation")
		}
		if strings.TrimSpace(input) != "y" {
			fmt.Printf("Skipped '%s'\n", repoName)
			return nil
		}

		idx := slices.Index(cfg.SyncedRepos, repoName)
		cfg.SyncedRepos = slices.Delete(cfg.SyncedRepos, idx, idx+1)

		if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
			return stacktrace.Propagate(err, "failed to write config")
		}
	}

	// Remove cloned repo from disk
	if existsOnDisk {
		if err := os.RemoveAll(repoDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to remove repo directory '%s'", repoDirpath)
		}
	}

	fmt.Printf("Removed '%s'\n", repoName)
	return nil
}
