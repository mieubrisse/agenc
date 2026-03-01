package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/server"
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
	client, err := serverClient()
	if err != nil {
		return err
	}

	for _, arg := range args {
		req := server.AddRepoRequest{
			Reference: arg,
		}

		if cmd.Flags().Changed(repoConfigAlwaysSyncedFlagName) {
			synced, err := cmd.Flags().GetBool(repoConfigAlwaysSyncedFlagName)
			if err != nil {
				return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigAlwaysSyncedFlagName)
			}
			req.AlwaysSynced = &synced
		}

		if cmd.Flags().Changed(repoConfigWindowTitleFlagName) {
			title, err := cmd.Flags().GetString(repoConfigWindowTitleFlagName)
			if err != nil {
				return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigWindowTitleFlagName)
			}
			req.WindowTitle = &title
		}

		resp, err := client.AddRepo(req)
		if err != nil {
			return stacktrace.Propagate(err, "failed to add repo '%s'", arg)
		}

		status := "Added"
		if !resp.WasNewlyCloned {
			status = "Already exists"
		}
		fmt.Printf("%s '%s'\n", status, resp.Name)
	}

	return nil
}
