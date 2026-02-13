package cmd

import (
	"fmt"

	"github.com/goccy/go-yaml"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var repoAddCmd = &cobra.Command{
	Use:   addCmdStr + " <repo>",
	Short: "Add a repository to the repo library",
	Long: fmt.Sprintf(`Add a repository to the repo library by cloning it into $AGENC_DIRPATH/repos/.

Accepts any of these formats:
  repo                                 - shorthand (requires gh auth login)
  owner/repo                           - shorthand (e.g., mieubrisse/agenc)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - HTTPS URL
  git@github.com:owner/repo.git        - SSH URL
  /path/to/local/clone                 - local filesystem path

Tip: Single-word shorthand works automatically if you're logged into gh (gh auth login)

For shorthand formats, the clone protocol (SSH vs HTTPS) is auto-detected
from existing repos in your library. If no repos exist, you'll be prompted
to choose.

Use --%s to keep the repo continuously synced by the daemon.
Use --%s to set a custom tmux window title.`,
		repoConfigAlwaysSyncedFlagName, repoConfigWindowTitleFlagName),
	Args: cobra.MinimumNArgs(1),
	RunE: runRepoAdd,
}

func init() {
	repoAddCmd.Flags().Bool(repoConfigAlwaysSyncedFlagName, false, "keep this repo continuously synced by the daemon")
	repoAddCmd.Flags().String(repoConfigWindowTitleFlagName, "", "custom tmux window title for missions using this repo")
	repoCmd.AddCommand(repoAddCmd)
}

func runRepoAdd(cmd *cobra.Command, args []string) error {
	agencDirpath, err := getAgencContext()
	if err != nil {
		return err
	}

	// Get default GitHub user (from gh CLI or config)
	defaultGitHubUser := getDefaultGitHubUser(agencDirpath)

	// Read config for potential updates
	var cfg *config.AgencConfig
	var cm yaml.CommentMap
	cfg, cm, err = readConfigWithComments()
	if err != nil {
		return err
	}

	// Validate all args are repo references (not search terms)
	for _, arg := range args {
		if !looksLikeRepoReference(arg, defaultGitHubUser) {
			return stacktrace.NewError("'%s' is not a valid repo reference; expected owner/repo, a URL, or a local path", arg)
		}
	}

	alwaysSyncedChanged := cmd.Flags().Changed(repoConfigAlwaysSyncedFlagName)
	windowTitleChanged := cmd.Flags().Changed(repoConfigWindowTitleFlagName)

	for _, arg := range args {
		result, err := resolveAsRepoReference(agencDirpath, arg, defaultGitHubUser)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve repo '%s'", arg)
		}

		if cfg != nil {
			rc, _ := cfg.GetRepoConfig(result.RepoName)

			if alwaysSyncedChanged {
				synced, err := cmd.Flags().GetBool(repoConfigAlwaysSyncedFlagName)
				if err != nil {
					return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigAlwaysSyncedFlagName)
				}
				rc.AlwaysSynced = synced
			}

			if windowTitleChanged {
				title, err := cmd.Flags().GetString(repoConfigWindowTitleFlagName)
				if err != nil {
					return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigWindowTitleFlagName)
				}
				rc.WindowTitle = title
			}

			cfg.SetRepoConfig(result.RepoName, rc)
		}

		status := "Added"
		if !result.WasNewlyCloned {
			status = "Already exists"
		}

		fmt.Printf("%s '%s'\n", status, result.RepoName)
	}

	if cfg != nil {
		if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
			return stacktrace.Propagate(err, "failed to write config")
		}
	}

	return nil
}
