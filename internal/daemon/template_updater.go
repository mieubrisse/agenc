package daemon

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/config"
)

const (
	repoUpdateInterval = 60 * time.Second
)

// runRepoUpdateLoop periodically fetches and fast-forwards repos
// (agent templates and git repo clones) referenced by running missions.
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

	repoNames := d.collectRunningMissionRepos()
	for _, repoName := range repoNames {
		if ctx.Err() != nil {
			return
		}
		d.updateRepo(ctx, repoName, refreshDefaultBranch)
	}
}

// collectRunningMissionRepos returns the distinct repo names (agent templates
// and git repo clones) referenced by missions whose wrapper PID is still
// alive. Old-format git repo values (absolute paths) are skipped.
func (d *Daemon) collectRunningMissionRepos() []string {
	missions, err := d.db.ListMissions(false)
	if err != nil {
		d.logger.Printf("Repo update: failed to list missions: %v", err)
		return nil
	}

	seen := make(map[string]bool)
	var repoNames []string
	for _, m := range missions {
		// Check if the mission's wrapper is running before collecting repos
		pidFilepath := config.GetMissionPIDFilepath(d.agencDirpath, m.ID)
		pid, err := ReadPID(pidFilepath)
		if err != nil || pid == 0 {
			continue
		}
		if !IsProcessRunning(pid) {
			continue
		}

		// Collect agent template repo
		if m.AgentTemplate != "" && !seen[m.AgentTemplate] {
			seen[m.AgentTemplate] = true
			repoNames = append(repoNames, m.AgentTemplate)
		}

		// Collect git repo clone (new-format only: github.com/owner/repo)
		if m.GitRepo != "" && !strings.HasPrefix(m.GitRepo, "/") && !seen[m.GitRepo] {
			seen[m.GitRepo] = true
			repoNames = append(repoNames, m.GitRepo)
		}
	}
	return repoNames
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
