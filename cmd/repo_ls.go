package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/tableprinter"
)

var repoLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List repositories in the repo library",
	RunE:  runRepoLs,
}

func init() {
	repoCmd.AddCommand(repoLsCmd)
}

func runRepoLs(cmd *cobra.Command, args []string) error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	repoNames, err := findReposOnDisk(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to scan repos directory")
	}

	if len(repoNames) == 0 {
		fmt.Println("No repositories in the repo library.")
		return nil
	}

	tbl := tableprinter.NewTable("REPO", "SYNCED")
	for _, repoName := range repoNames {
		synced := formatCheckmark(cfg.ContainsSyncedRepo(repoName))
		tbl.AddRow(displayGitRepo(repoName), synced)
	}
	tbl.Print()

	return nil
}

// findReposOnDisk scans $AGENC_DIRPATH/repos/ for cloned repositories and returns
// their canonical names (e.g. "github.com/owner/repo"), sorted alphabetically.
// The expected directory layout is repos/<host>/<owner>/<repo>/.
func findReposOnDisk(agencDirpath string) ([]string, error) {
	reposDirpath := config.GetReposDirpath(agencDirpath)

	hosts, err := listSubdirs(reposDirpath)
	if err != nil {
		return nil, err
	}

	var repoNames []string
	for _, host := range hosts {
		hostDirpath := filepath.Join(reposDirpath, host)
		owners, err := listSubdirs(hostDirpath)
		if err != nil {
			return nil, err
		}
		for _, owner := range owners {
			ownerDirpath := filepath.Join(hostDirpath, owner)
			repos, err := listSubdirs(ownerDirpath)
			if err != nil {
				return nil, err
			}
			for _, repo := range repos {
				repoNames = append(repoNames, filepath.Join(host, owner, repo))
			}
		}
	}

	sort.Strings(repoNames)
	return repoNames, nil
}

// listSubdirs returns the names of immediate subdirectories within dirpath.
// Returns an empty slice (not an error) if dirpath does not exist.
func listSubdirs(dirpath string) ([]string, error) {
	entries, err := os.ReadDir(dirpath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, stacktrace.Propagate(err, "failed to read directory '%s'", dirpath)
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	return dirs, nil
}

// formatCheckmark returns a checkmark or dash for boolean display.
func formatCheckmark(value bool) string {
	if value {
		return "✅"
	}
	return "--"
}
