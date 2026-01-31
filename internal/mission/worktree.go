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

// GetWorktreeBranchName returns the branch name for a mission's worktree,
// derived from the first 7 characters of the mission UUID.
func GetWorktreeBranchName(missionID string) string {
	return "agenc-" + missionID[:7]
}

// ValidateWorktreeRepo checks that the given directory is a Git repository
// with a "main" branch.
func ValidateWorktreeRepo(repoDirpath string) error {
	// Check it's a git repo
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = repoDirpath
	if output, err := cmd.CombinedOutput(); err != nil {
		return stacktrace.NewError("'%s' is not a git repository: %s", repoDirpath, strings.TrimSpace(string(output)))
	}

	// Check that "main" branch exists
	cmd = exec.Command("git", "rev-parse", "--verify", "main")
	cmd.Dir = repoDirpath
	if output, err := cmd.CombinedOutput(); err != nil {
		return stacktrace.NewError("repository '%s' has no 'main' branch: %s", repoDirpath, strings.TrimSpace(string(output)))
	}

	return nil
}

// ValidateWorktreeBranch checks that the given branch name does not already
// exist in the repository.
func ValidateWorktreeBranch(repoDirpath string, branchName string) error {
	cmd := exec.Command("git", "rev-parse", "--verify", branchName)
	cmd.Dir = repoDirpath
	if err := cmd.Run(); err == nil {
		return stacktrace.NewError("branch '%s' already exists in '%s'", branchName, repoDirpath)
	}
	return nil
}

// CreateWorktree creates a new Git worktree at workspaceDirpath on a new
// branch based off "main".
func CreateWorktree(repoDirpath string, workspaceDirpath string, branchName string) error {
	cmd := exec.Command("git", "worktree", "add", "-b", branchName, workspaceDirpath, "main")
	cmd.Dir = repoDirpath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.Propagate(err, "failed to create worktree: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// RemoveWorktree removes a Git worktree. This is best-effort; errors are
// returned but callers may choose to ignore them.
func RemoveWorktree(repoDirpath string, workspaceDirpath string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", workspaceDirpath)
	cmd.Dir = repoDirpath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.Propagate(err, "failed to remove worktree: %s", strings.TrimSpace(string(output)))
	}
	return nil
}

// DeleteWorktreeBranchIfMerged deletes the branch if it has been fully merged
// into main. Returns (true, nil) if the branch was deleted, (false, nil) if
// the branch has unmerged commits and was preserved, or (false, err) on
// unexpected failure.
func DeleteWorktreeBranchIfMerged(repoDirpath string, branchName string) (bool, error) {
	cmd := exec.Command("git", "branch", "-d", branchName)
	cmd.Dir = repoDirpath
	output, err := cmd.CombinedOutput()
	if err != nil {
		outputStr := strings.TrimSpace(string(output))
		// git branch -d exits non-zero when the branch is not fully merged
		if strings.Contains(outputStr, "not fully merged") {
			return false, nil
		}
		// Branch might not exist (already deleted)
		if strings.Contains(outputStr, "not found") {
			return false, nil
		}
		return false, stacktrace.Propagate(err, "failed to delete branch '%s': %s", branchName, outputStr)
	}
	return true, nil
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

// EnsureWorktreeClone clones the repo into ~/.agenc/repos/ if not already
// present. Uses the provided cloneURL for the clone. Returns the clone
// directory path.
func EnsureWorktreeClone(agencDirpath string, repoName string, cloneURL string) (string, error) {
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

// ResolveWorktreeRepoDirpath returns the agenc-owned clone path for a
// worktree_source value. Handles both old-format (absolute path) and
// new-format (github.com/owner/repo) values.
func ResolveWorktreeRepoDirpath(agencDirpath string, worktreeSource string) string {
	if strings.HasPrefix(worktreeSource, "/") {
		return worktreeSource // old format: raw filesystem path
	}
	return config.GetRepoDirpath(agencDirpath, worktreeSource) // new format: repo name
}
