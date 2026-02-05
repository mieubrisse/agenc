package mission

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

// ForceUpdateRepo fetches from origin and resets the local default branch to
// match the remote. This ensures the repo library clone is up-to-date before
// copying into a mission workspace.
func ForceUpdateRepo(repoDirpath string) error {
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = repoDirpath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return stacktrace.Propagate(err, "git fetch failed: %s", strings.TrimSpace(string(output)))
	}

	defaultBranch, err := GetDefaultBranch(repoDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to determine default branch")
	}

	remoteRef := "origin/" + defaultBranch
	resetCmd := exec.Command("git", "reset", "--hard", remoteRef)
	resetCmd.Dir = repoDirpath
	if output, err := resetCmd.CombinedOutput(); err != nil {
		return stacktrace.Propagate(err, "git reset failed: %s", strings.TrimSpace(string(output)))
	}

	return nil
}

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

// CopyWorkspace copies an entire workspace directory from srcWorkspaceDirpath
// to dstWorkspaceDirpath using rsync. If the source directory does not exist,
// this is a no-op (empty workspace = nothing to copy).
func CopyWorkspace(srcWorkspaceDirpath string, dstWorkspaceDirpath string) error {
	if _, err := os.Stat(srcWorkspaceDirpath); os.IsNotExist(err) {
		return nil
	}

	srcPath := srcWorkspaceDirpath + "/"
	dstPath := dstWorkspaceDirpath + "/"

	if err := os.MkdirAll(dstWorkspaceDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create directory '%s'", dstWorkspaceDirpath)
	}

	cmd := exec.Command("rsync", "-a", srcPath, dstPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.Propagate(err, "failed to copy workspace: %s", strings.TrimSpace(string(output)))
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

// ParseRepoReference parses a repository reference in the format "owner/repo",
// "github.com/owner/repo", or a full GitHub URL (SSH or HTTPS) into the
// canonical repo name and a clone URL. If the host is omitted, github.com is
// assumed. Extra path segments after owner/repo in a URL are ignored.
//
// The clone URL protocol is determined as follows:
//   - If an SSH URL is provided (git@github.com:... or ssh://...), returns SSH clone URL
//   - If an HTTPS URL is provided, returns HTTPS clone URL
//   - If just "owner/repo" is provided, uses preferSSH to decide (true = SSH, false = HTTPS)
func ParseRepoReference(ref string, preferSSH bool) (repoName string, cloneURL string, err error) {
	// Check for SSH URL formats first
	if m := githubSSHRegex.FindStringSubmatch(ref); m != nil {
		owner, repo := m[1], m[2]
		repoName = fmt.Sprintf("github.com/%s/%s", owner, repo)
		cloneURL = fmt.Sprintf("git@github.com:%s/%s.git", owner, repo)
		return repoName, cloneURL, nil
	}
	if m := githubSSHProtoRegex.FindStringSubmatch(ref); m != nil {
		owner, repo := m[1], m[2]
		repoName = fmt.Sprintf("github.com/%s/%s", owner, repo)
		cloneURL = fmt.Sprintf("git@github.com:%s/%s.git", owner, repo)
		return repoName, cloneURL, nil
	}

	// Check for HTTPS URL format
	const githubURLPrefix = "https://github.com/"
	if strings.HasPrefix(ref, githubURLPrefix) {
		ref = strings.TrimPrefix(ref, "https://")
		// Strip trailing slash and any .git suffix before splitting
		ref = strings.TrimRight(ref, "/")
		ref = strings.TrimSuffix(ref, ".git")
		// Keep only github.com/owner/repo (first 3 segments)
		segments := strings.SplitN(ref, "/", 4)
		if len(segments) >= 3 {
			owner, repo := segments[1], segments[2]
			repoName = fmt.Sprintf("github.com/%s/%s", owner, repo)
			cloneURL = fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
			return repoName, cloneURL, nil
		}
	}

	// Handle shorthand formats: "owner/repo" or "github.com/owner/repo"
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
		return "", "", stacktrace.NewError("invalid repo reference '%s'; expected 'owner/repo', 'github.com/owner/repo', or a GitHub URL", ref)
	}

	if owner == "" || repo == "" {
		return "", "", stacktrace.NewError("invalid repo reference '%s'; owner and repo must be non-empty", ref)
	}

	repoName = fmt.Sprintf("github.com/%s/%s", owner, repo)
	if preferSSH {
		cloneURL = fmt.Sprintf("git@github.com:%s/%s.git", owner, repo)
	} else {
		cloneURL = fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)
	}
	return repoName, cloneURL, nil
}

// ResolveRepoCloneDirpath returns the agenc-owned clone path for a
// git repo value (stored in the git_repo DB column). Handles both
// old-format (absolute path) and new-format (github.com/owner/repo) values.
func ResolveRepoCloneDirpath(agencDirpath string, gitRepo string) string {
	if strings.HasPrefix(gitRepo, "/") {
		return gitRepo // old format: raw filesystem path
	}
	return config.GetRepoDirpath(agencDirpath, gitRepo) // new format: repo name
}

// DetectPreferredProtocol examines existing repos in ~/.agenc/repos/ to infer
// the user's preferred clone protocol. Returns true if SSH should be preferred,
// false for HTTPS. If no repos exist or protocols are mixed, defaults to false
// (HTTPS).
func DetectPreferredProtocol(agencDirpath string) bool {
	reposDirpath := config.GetReposDirpath(agencDirpath)

	var sshCount, httpsCount int

	// Walk three levels: host/owner/repo
	hosts, err := os.ReadDir(reposDirpath)
	if err != nil {
		return false // No repos dir, default to HTTPS
	}

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
			for _, repo := range repos {
				if !repo.IsDir() {
					continue
				}
				repoDirpath := filepath.Join(reposDirpath, host.Name(), owner.Name(), repo.Name())
				protocol := detectRepoProtocol(repoDirpath)
				switch protocol {
				case "ssh":
					sshCount++
				case "https":
					httpsCount++
				}
			}
		}
	}

	// If user has any SSH repos, prefer SSH (they've explicitly set it up)
	// This is more user-friendly than requiring all repos to be SSH
	return sshCount > 0 && httpsCount == 0
}

// detectRepoProtocol reads the origin remote URL from a git repo and returns
// "ssh", "https", or "" if it cannot be determined.
func detectRepoProtocol(repoDirpath string) string {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repoDirpath
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	url := strings.TrimSpace(string(output))
	if githubSSHRegex.MatchString(url) || githubSSHProtoRegex.MatchString(url) {
		return "ssh"
	}
	if githubHTTPSRegex.MatchString(url) {
		return "https"
	}
	return ""
}
