package daemon

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

const (
	repoUpdateInterval = 60 * time.Second
)

// runRepoUpdateLoop periodically fetches and fast-forwards repos
// for all agent templates listed in config.yml.
func (d *Daemon) runRepoUpdateLoop(ctx context.Context) {
	d.runRepoUpdateCycle(ctx)

	ticker := time.NewTicker(repoUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runRepoUpdateCycle(ctx)
		}
	}
}

const (
	// refreshDefaultBranchInterval controls how often (in cycles) the daemon
	// runs "git remote set-head origin --auto" to keep origin/HEAD current.
	refreshDefaultBranchInterval = 10
)

func (d *Daemon) runRepoUpdateCycle(ctx context.Context) {
	d.repoUpdateCycleCount++
	refreshDefaultBranch := d.repoUpdateCycleCount%refreshDefaultBranchInterval == 0

	cfg, err := config.ReadAgencConfig(d.agencDirpath)
	if err != nil {
		d.logger.Printf("Repo update: failed to read config: %v", err)
		return
	}

	for repo := range cfg.AgentTemplates {
		if ctx.Err() != nil {
			return
		}

		repoName, cloneURL, err := mission.ParseRepoReference(repo)
		if err != nil {
			d.logger.Printf("Repo update: invalid repo '%s': %v", repo, err)
			continue
		}

		if err := d.ensureRepoCloned(ctx, repoName, cloneURL); err != nil {
			d.logger.Printf("Repo update: clone failed for '%s': %v", repoName, err)
			continue
		}

		d.updateRepo(ctx, repoName, refreshDefaultBranch)
	}
}

// ensureRepoCloned clones the repo if it doesn't already exist. Unlike
// mission.EnsureRepoClone, this uses CombinedOutput and logs instead of
// writing to stdout/stderr.
func (d *Daemon) ensureRepoCloned(ctx context.Context, repoName string, cloneURL string) error {
	cloneDirpath := config.GetRepoDirpath(d.agencDirpath, repoName)

	if _, err := os.Stat(cloneDirpath); err == nil {
		return nil
	}

	if err := os.MkdirAll(cloneDirpath, 0755); err != nil {
		return err
	}
	if err := os.Remove(cloneDirpath); err != nil {
		return err
	}

	gitCmd := exec.CommandContext(ctx, "git", "clone", cloneURL, cloneDirpath)
	if output, err := gitCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Repo update: git clone output for '%s': %s", repoName, strings.TrimSpace(string(output)))
		return err
	}

	d.logger.Printf("Repo update: cloned '%s' from %s", repoName, cloneURL)
	return nil
}

func (d *Daemon) updateRepo(ctx context.Context, repoName string, refreshDefaultBranch bool) {
	repoDirpath := config.GetRepoDirpath(d.agencDirpath, repoName)

	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	fetchCmd.Dir = repoDirpath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Repo update: git fetch failed for '%s': %v\n%s", repoName, err, string(output))
		return
	}

	// Periodically refresh origin/HEAD so we track the remote's default branch
	if refreshDefaultBranch {
		setHeadCmd := exec.CommandContext(ctx, "git", "remote", "set-head", "origin", "--auto")
		setHeadCmd.Dir = repoDirpath
		if output, err := setHeadCmd.CombinedOutput(); err != nil {
			d.logger.Printf("Repo update: git remote set-head failed for '%s': %v\n%s", repoName, err, string(output))
		}
	}

	defaultBranch, err := getDefaultBranch(ctx, repoDirpath)
	if err != nil {
		d.logger.Printf("Repo update: failed to determine default branch for '%s': %v", repoName, err)
		return
	}

	localHash, err := gitRevParse(ctx, repoDirpath, defaultBranch)
	if err != nil {
		d.logger.Printf("Repo update: failed to rev-parse %s for '%s': %v", defaultBranch, repoName, err)
		return
	}

	remoteRef := "origin/" + defaultBranch
	remoteHash, err := gitRevParse(ctx, repoDirpath, remoteRef)
	if err != nil {
		d.logger.Printf("Repo update: failed to rev-parse %s for '%s': %v", remoteRef, repoName, err)
		return
	}

	if localHash == remoteHash {
		return
	}

	resetCmd := exec.CommandContext(ctx, "git", "reset", "--hard", remoteRef)
	resetCmd.Dir = repoDirpath
	if output, err := resetCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Repo update: git reset failed for '%s': %v\n%s", repoName, err, string(output))
		return
	}

	d.logger.Printf("Repo update: updated '%s' (%s) to %s", repoName, defaultBranch, remoteHash[:8])
}

// getDefaultBranch reads the default branch name from origin/HEAD. Returns
// just the branch name (e.g. "main", "master"), not the full ref.
func getDefaultBranch(ctx context.Context, repoDirpath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "symbolic-ref", "refs/remotes/origin/HEAD")
	cmd.Dir = repoDirpath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	// Output is like "refs/remotes/origin/main" — extract the branch name
	ref := strings.TrimSpace(string(output))
	return strings.TrimPrefix(ref, "refs/remotes/origin/"), nil
}

// gitRevParse runs `git rev-parse <ref>` in the given directory and returns
// the trimmed commit hash.
func gitRevParse(ctx context.Context, dirpath string, ref string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", ref)
	cmd.Dir = dirpath
	output, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(output)), nil
}
