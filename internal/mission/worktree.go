package mission

import (
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
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
