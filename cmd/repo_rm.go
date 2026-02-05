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
	Long: fmt.Sprintf(`Remove one or more repositories from the repo library.

Deletes the cloned repo from $AGENC_DIRPATH/repos/ and removes it from the
syncedRepos list in config.yml if present.

Refuses to remove agent template repos. Use '%s %s %s' instead.

When called without arguments, opens an interactive fzf picker.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - URL

You can also use search terms to find a repo in your library:
  %s %s %s my repo                - searches for repos matching "my repo"`,
		agencCmdStr, templateCmdStr, rmCmdStr,
		agencCmdStr, repoCmdStr, rmCmdStr),
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

	allRepos, err := findReposOnDisk(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to scan repos directory")
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
			// Note: canonical resolution returns the parsed name even if not in removableRepos
			// The actual validation happens in removeSingleRepo
			return name, true, nil
		},
		GetItems:    func() ([]string, error) { return removableRepos, nil },
		ExtractText: func(repo string) string { return repo },
		FormatRow:   func(repo string) []string { return []string{displayGitRepo(repo)} },
		FzfPrompt:   "Select repos to remove (TAB to multi-select): ",
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
		if err := removeSingleRepo(cfg, cm, repoName); err != nil {
			return err
		}
	}
	return nil
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
