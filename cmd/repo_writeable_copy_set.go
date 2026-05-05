package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var repoWriteableCopySetCmd = &cobra.Command{
	Use:   setCmdStr + " <repo> <path>",
	Short: "Configure a writeable copy for a repo",
	Long: `Configure a writeable copy of a repo at the given absolute path. The path
must be outside ~/.agenc/ and must not overlap with any other configured
writeable copy.

The repo must already be in the repo library (use 'agenc repo add' first).
Setting a writeable copy implies always-synced=true.

After this command writes the config, the AgenC server picks up the change,
clones the repo to the path if it doesn't exist, and starts the sync loop.`,
	Args: cobra.ExactArgs(2),
	RunE: runRepoWriteableCopySet,
}

func init() {
	repoWriteableCopyCmd.AddCommand(repoWriteableCopySetCmd)
}

func runRepoWriteableCopySet(cmd *cobra.Command, args []string) error {
	repoName := args[0]
	rawPath := args[1]

	if !config.IsCanonicalRepoName(repoName) {
		return stacktrace.NewError("repo must be in canonical format 'github.com/owner/repo'; got '%s'", repoName)
	}

	cfg, cm, release, err := readConfigWithComments()
	if err != nil {
		return err
	}
	defer release()
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}

	// Build the "other writeable copies" map, excluding this repo so re-setting
	// the same path is idempotent.
	otherCopies := map[string]string{}
	for name, path := range cfg.GetAllWriteableCopies() {
		if name != repoName {
			otherCopies[name] = path
		}
	}

	canonicalPath, err := config.ValidateWriteableCopyPath(rawPath, agencDirpath, otherCopies)
	if err != nil {
		return stacktrace.Propagate(err, "invalid writeable-copy path")
	}

	rc, _ := cfg.GetRepoConfig(repoName)
	rc.WriteableCopy = canonicalPath
	rc.AlwaysSynced = true
	cfg.SetRepoConfig(repoName, rc)

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Configured writeable copy for '%s' at %s\n", repoName, canonicalPath)
	fmt.Println("Implies always-synced=true (writeable copies require continuous sync).")
	fmt.Println("The AgenC server will clone the repo (if needed) and start the sync loop on its next config-watcher cycle.")
	return nil
}
