package server

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
)

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// gitOperationTimeout caps any single git invocation. Long-running operations
// (clone of a giant repo, slow network) will be cancelled.
const gitOperationTimeout = 10 * time.Minute

// GitCommander is a narrow interface over git invocations used by the
// writeable-copy reconcile worker. The interface allows the tick state
// machine to be unit-tested with a fake implementation.
type GitCommander interface {
	// Status reports whether the working tree is clean and lists files in a
	// conflicted state (UU/AA/etc.). A clean tree returns (true, nil, nil).
	Status(repoDirpath string) (clean bool, conflicted []string, err error)
	// HEAD returns the current HEAD commit SHA.
	HEAD(repoDirpath string) (string, error)
	// DefaultBranch returns the name of the default branch from origin/HEAD.
	DefaultBranch(repoDirpath string) (string, error)
	// CurrentBranch returns the currently checked-out branch name. Returns an
	// empty string when in detached-HEAD state.
	CurrentBranch(repoDirpath string) (string, error)
	// OriginURL returns the configured URL for the "origin" remote.
	OriginURL(repoDirpath string) (string, error)
	// Fetch fetches from origin.
	Fetch(repoDirpath string) error
	// AddAll stages every change in the working tree.
	AddAll(repoDirpath string) error
	// Commit creates a commit with the given message. Returns nil if there is
	// nothing to commit (the caller should call Status first to decide).
	Commit(repoDirpath, message string) error
	// PullRebaseAutostash runs `git pull --rebase --autostash`. Returns the
	// list of conflicted files if the rebase failed; an empty list with a nil
	// error means success.
	PullRebaseAutostash(repoDirpath string) (conflicted []string, err error)
	// RebaseAbort aborts any rebase in progress.
	RebaseAbort(repoDirpath string) error
	// MergeFFOnly fast-forwards the current branch to the named ref. Fails
	// with a non-FF error when fast-forward is not possible.
	MergeFFOnly(repoDirpath, ref string) error
	// Push pushes the current branch to origin. The returned flags indicate
	// whether the push was rejected as non-FF or failed authentication; both
	// require user intervention and are surfaced as their own pause kinds.
	Push(repoDirpath string) (pushResult, error)
	// Clone clones url into dir.
	Clone(url, dir string) error
	// IsRebaseInProgress reports whether a rebase is mid-flight (.git/rebase-* exists).
	IsRebaseInProgress(repoDirpath string) bool
	// IsMergeInProgress reports whether a merge is mid-flight (.git/MERGE_HEAD exists).
	IsMergeInProgress(repoDirpath string) bool
	// IndexLockExists reports whether .git/index.lock exists (some other git
	// command is holding the lock).
	IndexLockExists(repoDirpath string) bool
	// RevParse resolves a ref (e.g. "HEAD", "origin/main") to a SHA.
	RevParse(repoDirpath, ref string) (string, error)
	// MergeBase returns the merge-base SHA of two refs.
	MergeBase(repoDirpath, a, b string) (string, error)
}

// pushResult captures the disposition of a push attempt.
type pushResult struct {
	NonFFRejected bool
	AuthFailure   bool
}

// realGit is the production GitCommander implementation backed by the git CLI.
type realGit struct{}

func newRealGit() GitCommander {
	return &realGit{}
}

func (realGit) runGit(repoDirpath string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitOperationTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = repoDirpath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), stacktrace.Propagate(err, "git %s failed in '%v': %s", strings.Join(args, " "), repoDirpath, strings.TrimSpace(string(output)))
	}
	return string(output), nil
}

func (g realGit) Status(repoDirpath string) (bool, []string, error) {
	out, err := g.runGit(repoDirpath, "status", "--porcelain=v1")
	if err != nil {
		return false, nil, err
	}
	if strings.TrimSpace(out) == "" {
		return true, nil, nil
	}
	var conflicted []string
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 3 {
			continue
		}
		// Conflicted statuses: DD, AU, UD, UA, DU, AA, UU
		x, y := line[0], line[1]
		if (x == 'U' || y == 'U') || (x == 'A' && y == 'A') || (x == 'D' && y == 'D') {
			conflicted = append(conflicted, strings.TrimSpace(line[3:]))
		}
	}
	return false, conflicted, nil
}

func (g realGit) HEAD(repoDirpath string) (string, error) {
	out, err := g.runGit(repoDirpath, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g realGit) DefaultBranch(repoDirpath string) (string, error) {
	out, err := g.runGit(repoDirpath, "symbolic-ref", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to determine default branch (origin/HEAD not set)")
	}
	ref := strings.TrimSpace(out)
	return strings.TrimPrefix(ref, "refs/remotes/origin/"), nil
}

func (g realGit) CurrentBranch(repoDirpath string) (string, error) {
	out, err := g.runGit(repoDirpath, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		// Detached HEAD: symbolic-ref fails. Treat as empty string.
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

func (g realGit) OriginURL(repoDirpath string) (string, error) {
	out, err := g.runGit(repoDirpath, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g realGit) Fetch(repoDirpath string) error {
	_, err := g.runGit(repoDirpath, "fetch", "origin")
	return err
}

func (g realGit) AddAll(repoDirpath string) error {
	_, err := g.runGit(repoDirpath, "add", "-A")
	return err
}

func (g realGit) Commit(repoDirpath, message string) error {
	_, err := g.runGit(repoDirpath, "commit", "-m", message)
	if err != nil {
		// "nothing to commit" is a benign case (race with another commit). Detect
		// by output content; treat as a no-op.
		if strings.Contains(err.Error(), "nothing to commit") {
			return nil
		}
		return err
	}
	return nil
}

func (g realGit) PullRebaseAutostash(repoDirpath string) ([]string, error) {
	out, err := g.runGit(repoDirpath, "pull", "--rebase", "--autostash", "origin")
	if err == nil {
		return nil, nil
	}
	// On rebase conflict, git prints conflicted paths in its output.
	conflicted := extractConflictedPathsFromGitOutput(out)
	return conflicted, err
}

func (g realGit) RebaseAbort(repoDirpath string) error {
	_, err := g.runGit(repoDirpath, "rebase", "--abort")
	return err
}

func (g realGit) MergeFFOnly(repoDirpath, ref string) error {
	_, err := g.runGit(repoDirpath, "merge", "--ff-only", ref)
	return err
}

func (g realGit) Push(repoDirpath string) (pushResult, error) {
	out, err := g.runGit(repoDirpath, "push", "origin")
	if err == nil {
		return pushResult{}, nil
	}
	lower := strings.ToLower(out)
	result := pushResult{}
	switch {
	case strings.Contains(lower, "non-fast-forward") || strings.Contains(lower, "rejected"):
		result.NonFFRejected = true
	case strings.Contains(lower, "authentication") || strings.Contains(lower, "permission denied") || strings.Contains(lower, "could not read") && strings.Contains(lower, "username"):
		result.AuthFailure = true
	}
	return result, err
}

func (realGit) Clone(url, dir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), gitOperationTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "clone", url, dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.Propagate(err, "git clone of '%v' into '%v' failed: %s", url, dir, strings.TrimSpace(string(out)))
	}
	return nil
}

func (realGit) IsRebaseInProgress(repoDirpath string) bool {
	for _, name := range []string{"rebase-apply", "rebase-merge"} {
		if pathExists(filepath.Join(repoDirpath, ".git", name)) {
			return true
		}
	}
	return false
}

func (realGit) IsMergeInProgress(repoDirpath string) bool {
	return pathExists(filepath.Join(repoDirpath, ".git", "MERGE_HEAD"))
}

func (realGit) IndexLockExists(repoDirpath string) bool {
	return pathExists(filepath.Join(repoDirpath, ".git", "index.lock"))
}

func (g realGit) RevParse(repoDirpath, ref string) (string, error) {
	out, err := g.runGit(repoDirpath, "rev-parse", ref)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (g realGit) MergeBase(repoDirpath, a, b string) (string, error) {
	out, err := g.runGit(repoDirpath, "merge-base", a, b)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// extractConflictedPathsFromGitOutput parses CONFLICT lines from the output of
// `git pull --rebase`. Each line has the form: "CONFLICT (content): Merge conflict in <path>".
func extractConflictedPathsFromGitOutput(out string) []string {
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		const marker = "Merge conflict in "
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		path := strings.TrimSpace(line[idx+len(marker):])
		if path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}
