package cmd

import (
	"fmt"
	"slices"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var repoAddSyncFlag bool

var repoAddCmd = &cobra.Command{
	Use:   addCmdStr + " <repo>",
	Short: "Add a repository to the repo library",
	Long: `Add a repository to the repo library by cloning it into ~/.agenc/repos/.

Accepts any of these formats:
  owner/repo
  github.com/owner/repo
  https://github.com/owner/repo
  git@github.com:owner/repo.git

Tip: Use the full URL to control the clone protocol. For SSH authentication,
use git@github.com:owner/repo.git. For HTTPS, use https://github.com/owner/repo.
Shorthand formats (owner/repo) inherit the protocol from existing repos in
your library, defaulting to HTTPS if none exist.

Use --sync to keep the repo continuously synced by the daemon.`,
	Args: cobra.ExactArgs(1),
	RunE: runRepoAdd,
}

func init() {
	repoAddCmd.Flags().BoolVar(&repoAddSyncFlag, "sync", false, "keep this repo continuously synced by the daemon")
	repoCmd.AddCommand(repoAddCmd)
}

func runRepoAdd(cmd *cobra.Command, args []string) error {
	preferSSH := mission.DetectPreferredProtocol(agencDirpath)
	repoName, cloneURL, err := mission.ParseRepoReference(args[0], preferSSH)
	if err != nil {
		return stacktrace.Propagate(err, "invalid repo reference")
	}

	if _, err := mission.EnsureRepoClone(agencDirpath, repoName, cloneURL); err != nil {
		return stacktrace.Propagate(err, "failed to clone repository '%s'", repoName)
	}

	if repoAddSyncFlag {
		cfg, cm, err := config.ReadAgencConfig(agencDirpath)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read config")
		}

		if !slices.Contains(cfg.SyncedRepos, repoName) {
			cfg.SyncedRepos = append(cfg.SyncedRepos, repoName)

			if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
				return stacktrace.Propagate(err, "failed to write config")
			}
		}
	}

	if repoAddSyncFlag {
		fmt.Printf("Added '%s' (synced)\n", repoName)
	} else {
		fmt.Printf("Added '%s'\n", repoName)
	}
	return nil
}
