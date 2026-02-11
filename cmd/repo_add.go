package cmd

import (
	"fmt"

	"github.com/goccy/go-yaml"
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

For shorthand formats, the clone protocol (SSH vs HTTPS) is auto-detected
from existing repos in your library. If no repos exist, you'll be prompted
to choose.

Use --%s to keep the repo continuously synced by the daemon.`,
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
	// Validate all args are repo references (not search terms)
	for _, arg := range args {
		if !looksLikeRepoReference(arg) {
			return stacktrace.NewError("'%s' is not a valid repo reference; expected owner/repo, a URL, or a local path", arg)
		}
	}

	var cfg *config.AgencConfig
	var cm yaml.CommentMap
	if repoAddSyncFlag {
		var err error
		cfg, cm, err = readConfigWithComments()
		if err != nil {
			return err
		}
	}

	for _, arg := range args {
		result, err := resolveAsRepoReference(agencDirpath, arg)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve repo '%s'", arg)
		}

		if repoAddSyncFlag && !cfg.IsAlwaysSynced(result.RepoName) {
			cfg.SetAlwaysSynced(result.RepoName, true)
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
	}

	if repoAddSyncFlag {
		if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
			return stacktrace.Propagate(err, "failed to write config")
		}
	}

	return nil
}
