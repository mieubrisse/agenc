package cmd

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var repoAddSyncFlag bool

var repoAddCmd = &cobra.Command{
	Use:   addCmdStr + " <repo>",
	Short: "Add a repository to the repo library",
	Long: fmt.Sprintf(`Add a repository to the repo library by cloning it into $AGENC_DIRPATH/repos/.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - HTTPS URL
  git@github.com:owner/repo.git        - SSH URL
  /path/to/local/clone                 - local filesystem path

You can also use search terms to find an existing repo in your library:
  %s %s %s my repo               - searches for repos matching "my repo"

For shorthand formats, the clone protocol (SSH vs HTTPS) is auto-detected
from existing repos in your library. If no repos exist, you'll be prompted
to choose.

Use --%s to keep the repo continuously synced by the daemon.`,
		agencCmdStr, repoCmdStr, addCmdStr,
		syncFlagName),
	Args: cobra.MinimumNArgs(1),
	RunE: runRepoAdd,
}

func init() {
	repoAddCmd.Flags().BoolVar(&repoAddSyncFlag, syncFlagName, false, "keep this repo continuously synced by the daemon")
	repoCmd.AddCommand(repoAddCmd)
}

func runRepoAdd(cmd *cobra.Command, args []string) error {
	if _, err := getAgencContext(); err != nil {
		return err
	}
	// Join args - could be a single repo ref or multiple search terms
	input := args[0]
	if len(args) > 1 {
		// Multiple args: either multiple repo refs or search terms
		// If the first arg doesn't look like a repo ref, treat all as search terms
		if !looksLikeRepoReference(args[0]) {
			input = strings.Join(args, " ")
		}
	}

	result, err := ResolveRepoInput(agencDirpath, input, "Select repo to add: ")
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve repo")
	}

	if repoAddSyncFlag {
		cfg, cm, err := readConfigWithComments()
		if err != nil {
			return err
		}

		if !slices.Contains(cfg.SyncedRepos, result.RepoName) {
			cfg.SyncedRepos = append(cfg.SyncedRepos, result.RepoName)

			if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
				return stacktrace.Propagate(err, "failed to write config")
			}
		}
	}

	status := "Added"
	if !result.WasNewlyCloned {
		status = "Already exists"
	}

	if repoAddSyncFlag {
		fmt.Printf("%s '%s' (synced)\n", status, result.RepoName)
	} else {
		fmt.Printf("%s '%s'\n", status, result.RepoName)
	}
	return nil
}
