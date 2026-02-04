package cmd

import (
	"fmt"
	"os"
	"slices"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var repoRmCmd = &cobra.Command{
	Use:   "rm [repo...]",
	Short: "Remove a repository from the repo library",
	Long: `Remove one or more repositories from the repo library.

Deletes the cloned repo from ~/.agenc/repos/ and removes it from the
syncedRepos list in config.yml if present.

Refuses to remove a repo that is still registered as an agent template.
Use 'agenc template rm' first in that case.

When called without arguments, opens an interactive fzf picker.

Accepts any of these formats:
  owner/repo
  github.com/owner/repo
  https://github.com/owner/repo`,
	RunE: runRepoRm,
}

func init() {
	repoCmd.AddCommand(repoRmCmd)
}

func runRepoRm(cmd *cobra.Command, args []string) error {
	repoNames, err := resolveRepoArgs(args, "Select repos to remove (TAB to multi-select): ")
	if err != nil {
		return err
	}
	if len(repoNames) == 0 {
		return nil
	}

	for _, repoName := range repoNames {
		if err := removeSingleRepo(repoName); err != nil {
			return err
		}
	}
	return nil
}

// resolveRepoArgs converts CLI arguments to canonical repo names, or falls
// back to an fzf picker when no arguments are given.
func resolveRepoArgs(args []string, fzfPrompt string) ([]string, error) {
	if len(args) > 0 {
		var repoNames []string
		for _, arg := range args {
			repoName, _, err := mission.ParseRepoReference(arg)
			if err != nil {
				return nil, stacktrace.Propagate(err, "invalid repo reference")
			}
			repoNames = append(repoNames, repoName)
		}
		return repoNames, nil
	}

	allRepos, err := findReposOnDisk(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to scan repos directory")
	}
	if len(allRepos) == 0 {
		fmt.Println("No repositories in the repo library.")
		return nil, nil
	}

	return selectReposWithFzf(allRepos, fzfPrompt)
}

func removeSingleRepo(repoName string) error {
	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	if _, isTemplate := cfg.AgentTemplates[repoName]; isTemplate {
		return stacktrace.NewError(
			"'%s' is registered as an agent template; run 'agenc template rm %s' first",
			repoName, repoName,
		)
	}

	// Check whether the repo exists (on disk or in config) before removing
	repoDirpath := config.GetRepoDirpath(agencDirpath, repoName)
	_, statErr := os.Stat(repoDirpath)
	existsOnDisk := statErr == nil
	existsInConfig := slices.Contains(cfg.SyncedRepos, repoName)

	// Remove from syncedRepos if present
	if existsInConfig {
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

	if existsOnDisk || existsInConfig {
		fmt.Printf("Removed '%s'\n", repoName)
	} else {
		fmt.Printf("'%s' not found\n", repoName)
	}
	return nil
}
