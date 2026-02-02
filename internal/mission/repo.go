package mission

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

// GetDefaultBranch returns the default branch name for a repository by reading
// origin/HEAD. Returns just the branch name (e.g. "main", "master").
func GetDefaultBranch(repoDirpath string) (string, error) {
	cmd := exec.Command("git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = repoDirpath
	output, err := cmd.Output()
	if err != nil {
		return "", stacktrace.NewError("failed to determine default branch in '%s' (origin/HEAD not set)", repoDirpath)
	}
	ref := strings.TrimSpace(string(output))
	return strings.TrimPrefix(ref, "refs/remotes/origin/"), nil
}

// ValidateGitRepo checks that the given directory is a Git repository
// whose default branch (per origin/HEAD) exists locally.
func ValidateGitRepo(repoDirpath string) error {
	// Check it's a git repo
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = repoDirpath
	if output, err := cmd.CombinedOutput(); err != nil {
		return stacktrace.NewError("'%s' is not a git repository: %s", repoDirpath, strings.TrimSpace(string(output)))
	}

	// Determine the default branch from origin/HEAD
	defaultBranch, err := GetDefaultBranch(repoDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "cannot determine default branch for '%s'", repoDirpath)
	}

	// Check that the default branch exists locally
	cmd = exec.Command("git", "rev-parse", "--verify", defaultBranch)
	cmd.Dir = repoDirpath
	if output, err := cmd.CombinedOutput(); err != nil {
		return stacktrace.NewError("repository '%s' has no '%s' branch: %s", repoDirpath, defaultBranch, strings.TrimSpace(string(output)))
	}

	return nil
}

// CopyRepo copies an entire git repository from srcRepoDirpath to
// dstWorkspaceDirpath using rsync. The destination receives a full
// independent copy including the .git/ directory.
func CopyRepo(srcRepoDirpath string, dstWorkspaceDirpath string) error {
	srcPath := srcRepoDirpath + "/"
	dstPath := dstWorkspaceDirpath + "/"

	if err := os.MkdirAll(dstWorkspaceDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create directory '%s'", dstWorkspaceDirpath)
	}

	cmd := exec.Command("rsync", "-a", srcPath, dstPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.Propagate(err, "failed to copy repo: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// githubSSHRegex matches git@github.com:owner/repo or git@github.com:owner/repo.git
var githubSSHRegex = regexp.MustCompile(`^git@github\.com:([^/]+)/([^/]+?)(?:\.git)?$`)

// githubHTTPSRegex matches https://github.com/owner/repo or https://github.com/owner/repo.git
var githubHTTPSRegex = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+?)(?:\.git)?$`)

// githubSSHProtoRegex matches ssh://git@github.com/owner/repo or ssh://git@github.com/owner/repo.git
var githubSSHProtoRegex = regexp.MustCompile(`^ssh://git@github\.com/([^/]+)/([^/]+?)(?:\.git)?$`)

// ParseGitHubRemoteURL parses a GitHub remote URL (SSH or HTTPS) into
// "github.com/owner/repo" format.
func ParseGitHubRemoteURL(remoteURL string) (string, error) {
	remoteURL = strings.TrimSpace(remoteURL)

	if m := githubSSHRegex.FindStringSubmatch(remoteURL); m != nil {
		return fmt.Sprintf("github.com/%s/%s", m[1], m[2]), nil
	}
	if m := githubHTTPSRegex.FindStringSubmatch(remoteURL); m != nil {
		return fmt.Sprintf("github.com/%s/%s", m[1], m[2]), nil
	}
	if m := githubSSHProtoRegex.FindStringSubmatch(remoteURL); m != nil {
		return fmt.Sprintf("github.com/%s/%s", m[1], m[2]), nil
	}

	return "", stacktrace.NewError("remote URL '%s' is not a GitHub URL; only GitHub repositories are supported", remoteURL)
}

// ExtractGitHubRepoName reads the origin remote URL from the given git repo
// and parses it into "github.com/owner/repo" format.
// Errors if origin is not a GitHub URL.
func ExtractGitHubRepoName(repoDirpath string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repoDirpath
	output, err := cmd.Output()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to read origin remote URL from '%s'", repoDirpath)
	}

	return ParseGitHubRemoteURL(strings.TrimSpace(string(output)))
}

// EnsureRepoClone clones the repo into ~/.agenc/repos/ if not already
// present. Uses the provided cloneURL for the clone. Returns the clone
// directory path.
func EnsureRepoClone(agencDirpath string, repoName string, cloneURL string) (string, error) {
	cloneDirpath := config.GetRepoDirpath(agencDirpath, repoName)

	// If already cloned, return immediately
	if _, err := os.Stat(cloneDirpath); err == nil {
		return cloneDirpath, nil
	}

	// Create intermediate directories then remove the leaf so git clone can create it
	if err := os.MkdirAll(cloneDirpath, 0755); err != nil {
		return "", stacktrace.Propagate(err, "failed to create directory '%s'", cloneDirpath)
	}
	if err := os.Remove(cloneDirpath); err != nil {
		return "", stacktrace.Propagate(err, "failed to remove placeholder directory '%s'", cloneDirpath)
	}

	gitCmd := exec.Command("git", "clone", cloneURL, cloneDirpath)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	if err := gitCmd.Run(); err != nil {
		return "", stacktrace.Propagate(err, "failed to clone '%s'", cloneURL)
	}

	return cloneDirpath, nil
}

// ParseRepoReference parses a repository reference in the format "owner/repo"
// or "github.com/owner/repo" into the canonical repo name and an HTTPS clone
// URL. If the host is omitted, github.com is assumed.
func ParseRepoReference(ref string) (repoName string, cloneURL string, err error) {
	parts := strings.Split(ref, "/")

	var owner, repo string
	switch len(parts) {
	case 2:
		// owner/repo → github.com/owner/repo
		owner, repo = parts[0], parts[1]
	case 3:
		// github.com/owner/repo
		if parts[0] != "github.com" {
			return "", "", stacktrace.NewError("unsupported host '%s'; only github.com is supported", parts[0])
		}
		owner, repo = parts[1], parts[2]
	default:
		return "", "", stacktrace.NewError("invalid repo reference '%s'; expected 'owner/repo' or 'github.com/owner/repo'", ref)
	}

	if owner == "" || repo == "" {
		return "", "", stacktrace.NewError("invalid repo reference '%s'; owner and repo must be non-empty", ref)
	}

	repoName = fmt.Sprintf("github.com/%s/%s", owner, repo)
	cloneURL = fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
	return repoName, cloneURL, nil
}

// ResolveRepoCloneDirpath returns the agenc-owned clone path for a
// git repo value (stored in the worktree_source DB column). Handles both
// old-format (absolute path) and new-format (github.com/owner/repo) values.
func ResolveRepoCloneDirpath(agencDirpath string, gitRepo string) string {
	if strings.HasPrefix(gitRepo, "/") {
		return gitRepo // old format: raw filesystem path
	}
	return config.GetRepoDirpath(agencDirpath, gitRepo) // new format: repo name
}
