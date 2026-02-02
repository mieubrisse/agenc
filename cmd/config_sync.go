package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var configSyncCmd = &cobra.Command{
	Use:   "sync <repo-locator>",
	Short: "Sync agenc config from a GitHub repository",
	Long:  "Clone a config repo and symlink ~/.agenc/config to it. The daemon will keep it updated.",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigSync,
}

func init() {
	configCmd.AddCommand(configSyncCmd)
}

func runConfigSync(cmd *cobra.Command, args []string) error {
	repoLocator := args[0]

	repoName, cloneURL, err := parseConfigRepoLocator(repoLocator)
	if err != nil {
		return stacktrace.Propagate(err, "failed to parse repo locator '%s'", repoLocator)
	}

	// Clone into the repo library (skip if already present)
	repoDirpath := config.GetRepoDirpath(agencDirpath, repoName)
	if _, statErr := os.Stat(repoDirpath); os.IsNotExist(statErr) {
		if _, cloneErr := mission.EnsureRepoClone(agencDirpath, repoName, cloneURL); cloneErr != nil {
			return stacktrace.Propagate(cloneErr, "failed to clone config repo '%s'", repoName)
		}
		fmt.Printf("Cloned config repo: %s\n", repoName)
	} else {
		fmt.Printf("Config repo already cloned: %s\n", repoName)
	}

	// Replace $AGENC_DIRPATH/config with a symlink to the cloned repo
	configDirpath := config.GetConfigDirpath(agencDirpath)
	if err := replaceWithSymlink(configDirpath, repoDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to create config symlink")
	}

	fmt.Printf("Config synced: %s -> %s\n", configDirpath, repoDirpath)
	return nil
}

// parseConfigRepoLocator parses a repo locator in the format "owner/repo",
// "github.com/owner/repo", or an HTTPS URL like "https://github.com/owner/repo".
// Returns the canonical repo name and HTTPS clone URL.
func parseConfigRepoLocator(locator string) (repoName string, cloneURL string, err error) {
	if strings.HasPrefix(locator, "https://") || strings.HasPrefix(locator, "http://") {
		return parseConfigRepoFromURL(locator)
	}
	return mission.ParseRepoReference(locator)
}

// parseConfigRepoFromURL parses an HTTPS GitHub URL into a canonical repo name
// and clone URL.
func parseConfigRepoFromURL(rawURL string) (string, string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", "", stacktrace.Propagate(err, "failed to parse URL '%s'", rawURL)
	}

	if parsed.Host != "github.com" {
		return "", "", stacktrace.NewError("unsupported host '%s'; only github.com is supported", parsed.Host)
	}

	// Path is like "/owner/repo" or "/owner/repo.git"
	trimmedPath := strings.TrimPrefix(parsed.Path, "/")
	trimmedPath = strings.TrimSuffix(trimmedPath, ".git")
	parts := strings.Split(trimmedPath, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", stacktrace.NewError("invalid GitHub URL path '%s'; expected /owner/repo", parsed.Path)
	}

	repoName := fmt.Sprintf("github.com/%s/%s", parts[0], parts[1])
	cloneURL := fmt.Sprintf("https://github.com/%s/%s.git", parts[0], parts[1])
	return repoName, cloneURL, nil
}

// replaceWithSymlink ensures that dstPath is a symlink pointing to srcPath.
// If dstPath is a directory, it is removed. If it's a wrong symlink, it's
// replaced. If it's already correct, this is a no-op.
func replaceWithSymlink(dstPath string, srcPath string) error {
	info, err := os.Lstat(dstPath)
	if err == nil {
		// Something exists at dstPath
		if info.Mode()&os.ModeSymlink != 0 {
			target, readErr := os.Readlink(dstPath)
			if readErr == nil && target == srcPath {
				return nil // Already correct
			}
		}
		// Wrong symlink target, regular dir, or regular file — remove it
		if err := os.RemoveAll(dstPath); err != nil {
			return stacktrace.Propagate(err, "failed to remove existing item at '%s'", dstPath)
		}
	} else if !os.IsNotExist(err) {
		return stacktrace.Propagate(err, "failed to stat '%s'", dstPath)
	}

	if err := os.Symlink(srcPath, dstPath); err != nil {
		return stacktrace.Propagate(err, "failed to create symlink '%s' -> '%s'", dstPath, srcPath)
	}
	return nil
}
