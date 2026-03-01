package repo

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

const gitOperationTimeout = 30 * time.Second

// RepoResolutionResult holds the outcome of resolving a repo input.
type RepoResolutionResult struct {
	RepoName       string // Canonical repo name (github.com/owner/repo)
	CloneDirpath   string // Path to the clone in $AGENC_DIRPATH/repos/
	WasNewlyCloned bool   // True if the repo was cloned as part of resolution
}

// LooksLikeRepoReference returns true if the input appears to be a repo
// reference rather than search terms. A repo reference is one of:
//   - A local filesystem path (starts with /, ., or ~)
//   - A Git SSH URL (starts with git@ or ssh://)
//   - An HTTPS URL (starts with https://)
//   - A shorthand reference (owner/repo or github.com/owner/repo)
//   - A single word (repo name) if defaultGitHubUser is set
//
// Search terms are characterized by:
//   - Multiple space-separated words (e.g., "my repo")
//   - A single word when defaultGitHubUser is not set
//
// This function calls GetDefaultGitHubUser lazily - only when needed for
// single-word input detection, avoiding unnecessary gh CLI calls.
func LooksLikeRepoReference(input string) bool {
	// Contains spaces = search terms
	if strings.Contains(input, " ") {
		return false
	}

	// Local filesystem path
	if strings.HasPrefix(input, "/") || strings.HasPrefix(input, ".") || strings.HasPrefix(input, "~") {
		return true
	}

	// Git SSH URL (git@github.com:owner/repo.git)
	if strings.HasPrefix(input, "git@") {
		return true
	}

	// SSH protocol URL (ssh://git@github.com/owner/repo.git)
	if strings.HasPrefix(input, "ssh://") {
		return true
	}

	// HTTPS URL
	if strings.HasPrefix(input, "https://") {
		return true
	}

	// HTTP URL (will be upgraded to HTTPS)
	if strings.HasPrefix(input, "http://") {
		return true
	}

	// Shorthand: owner/repo or github.com/owner/repo
	// Must have exactly 1 or 2 slashes, no spaces
	parts := strings.Split(input, "/")
	switch len(parts) {
	case 1:
		// Single word with no slashes - only a repo reference if defaultGitHubUser is set
		// LAZY: only call GetDefaultGitHubUser when we actually need it
		defaultGitHubUser := GetDefaultGitHubUser()
		return defaultGitHubUser != "" && input != ""
	case 2:
		// owner/repo format - both parts must be non-empty
		return parts[0] != "" && parts[1] != ""
	case 3:
		// github.com/owner/repo format - all parts must be non-empty
		return parts[0] != "" && parts[1] != "" && parts[2] != ""
	}

	return false
}

// IsLocalPath returns true if the string looks like a filesystem path rather
// than a repo reference (URL or shorthand).
func IsLocalPath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~")
}

// ResolveAsRepoReference resolves input that looks like a repo reference.
// Clones the repo into $AGENC_DIRPATH/repos/ if not already present.
// If defaultGitHubUser is set and input is a single word, expands to "defaultGitHubUser/input".
func ResolveAsRepoReference(agencDirpath string, input string, defaultGitHubUser string) (*RepoResolutionResult, error) {
	// Local filesystem path
	if IsLocalPath(input) {
		return resolveLocalPathRepo(agencDirpath, input)
	}

	// Repo reference (URL or shorthand)
	// Pass defaultGitHubUser for bare name expansion
	return resolveRemoteRepoReference(agencDirpath, input, defaultGitHubUser)
}

// resolveLocalPathRepo handles input that's a local filesystem path to a git repo.
func resolveLocalPathRepo(agencDirpath string, localPath string) (*RepoResolutionResult, error) {
	// Expand ~ and resolve to absolute path
	if strings.HasPrefix(localPath, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to get home directory")
		}
		localPath = filepath.Join(home, localPath[1:])
	}

	absDirpath, err := filepath.Abs(localPath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to resolve path '%s'", localPath)
	}

	// Validate it's a git repo
	if err := mission.ValidateGitRepo(absDirpath); err != nil {
		return nil, stacktrace.Propagate(err, "'%s' is not a valid git repository", absDirpath)
	}

	// Extract the GitHub repo name from the origin remote
	repoName, err := mission.ExtractGitHubRepoName(absDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to extract GitHub repo name from '%s'", absDirpath)
	}

	// Get the clone URL from the local repo (preserves SSH vs HTTPS preference)
	cloneURL, err := GetOriginRemoteURL(absDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read origin remote URL")
	}

	// Ensure the repo is cloned into the library
	cloneDirpath := config.GetRepoDirpath(agencDirpath, repoName)
	wasNewlyCloned := false
	if _, statErr := os.Stat(cloneDirpath); os.IsNotExist(statErr) {
		if _, cloneErr := mission.EnsureRepoClone(agencDirpath, repoName, cloneURL); cloneErr != nil {
			return nil, stacktrace.Propagate(cloneErr, "failed to clone '%s'", repoName)
		}
		wasNewlyCloned = true
	}

	return &RepoResolutionResult{
		RepoName:       repoName,
		CloneDirpath:   cloneDirpath,
		WasNewlyCloned: wasNewlyCloned,
	}, nil
}

// resolveRemoteRepoReference handles input that's a URL or shorthand repo reference.
// When no protocol preference can be determined non-interactively, defaults to HTTPS.
func resolveRemoteRepoReference(agencDirpath string, ref string, defaultOwner string) (*RepoResolutionResult, error) {
	// Determine protocol preference non-interactively
	preferSSH, hasPreference, err := GetProtocolPreference(agencDirpath)
	if err != nil {
		return nil, err
	}

	// When no preference can be determined, default to HTTPS
	if !hasPreference {
		preferSSH = false
	}

	// Parse the reference
	repoName, cloneURL, err := mission.ParseRepoReference(ref, preferSSH, defaultOwner)
	if err != nil {
		return nil, stacktrace.Propagate(err, "invalid repo reference '%s'", ref)
	}

	// Check if already cloned
	cloneDirpath := config.GetRepoDirpath(agencDirpath, repoName)
	wasNewlyCloned := false
	if _, statErr := os.Stat(cloneDirpath); os.IsNotExist(statErr) {
		if _, cloneErr := mission.EnsureRepoClone(agencDirpath, repoName, cloneURL); cloneErr != nil {
			return nil, stacktrace.Propagate(cloneErr, "failed to clone '%s'", repoName)
		}
		wasNewlyCloned = true
	}

	return &RepoResolutionResult{
		RepoName:       repoName,
		CloneDirpath:   cloneDirpath,
		WasNewlyCloned: wasNewlyCloned,
	}, nil
}

// GetProtocolPreference determines the user's preferred clone protocol (SSH vs HTTPS)
// using only non-interactive sources:
//  1. gh config get git_protocol (if gh CLI is available and configured)
//  2. Infer from existing repos in the library
//
// Returns hasPreference=false when no preference can be determined (no gh config
// and no existing repos), allowing callers to either prompt interactively or
// default to a fallback.
func GetProtocolPreference(agencDirpath string) (preferSSH bool, hasPreference bool, err error) {
	// First check gh config
	if ghPreferSSH, hasGhConfig := GetGhConfigProtocol(); hasGhConfig {
		return ghPreferSSH, true, nil
	}

	reposDirpath := config.GetReposDirpath(agencDirpath)

	// Check if any repos exist
	hasRepos := false
	if hosts, readErr := os.ReadDir(reposDirpath); readErr == nil {
		for _, host := range hosts {
			if !host.IsDir() {
				continue
			}
			owners, _ := os.ReadDir(filepath.Join(reposDirpath, host.Name()))
			for _, owner := range owners {
				if !owner.IsDir() {
					continue
				}
				repos, _ := os.ReadDir(filepath.Join(reposDirpath, host.Name(), owner.Name()))
				for _, r := range repos {
					if r.IsDir() {
						hasRepos = true
						break
					}
				}
				if hasRepos {
					break
				}
			}
			if hasRepos {
				break
			}
		}
	}

	if hasRepos {
		// Infer from existing repos
		return mission.DetectPreferredProtocol(agencDirpath), true, nil
	}

	// No repos exist and no gh config - caller must decide
	return false, false, nil
}

// GetOriginRemoteURL reads the origin remote URL from a local git repo.
func GetOriginRemoteURL(repoDirpath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitOperationTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = repoDirpath
	output, err := cmd.Output()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read origin remote URL")
	}
	return strings.TrimSpace(string(output)), nil
}
