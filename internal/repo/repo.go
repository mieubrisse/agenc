package repo

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/mieubrisse/stacktrace"
)

// FindReposOnDisk scans a repos directory for cloned repositories and returns
// their canonical names (e.g. "github.com/owner/repo"), sorted alphabetically.
// The expected directory layout is <reposDirpath>/<host>/<owner>/<repo>/.
// Returns an empty slice (not an error) if the directory does not exist.
func FindReposOnDisk(reposDirpath string) ([]string, error) {
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
