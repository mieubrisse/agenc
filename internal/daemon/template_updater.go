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
// (agent templates and worktree source clones) referenced by running missions.
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

func (d *Daemon) runRepoUpdateCycle(ctx context.Context) {
	repoNames := d.collectRunningMissionRepos()
	for _, repoName := range repoNames {
		if ctx.Err() != nil {
			return
		}
		d.updateRepo(ctx, repoName)
	}
}

// collectRunningMissionRepos returns the distinct repo names (agent templates
// and worktree source clones) referenced by missions whose wrapper PID is
// still alive. Old-format worktree sources (absolute paths) are skipped.
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

		// Collect worktree source repo (new-format only: github.com/owner/repo)
		if m.WorktreeSource != "" && !strings.HasPrefix(m.WorktreeSource, "/") && !seen[m.WorktreeSource] {
			seen[m.WorktreeSource] = true
			repoNames = append(repoNames, m.WorktreeSource)
		}
	}
	return repoNames
}

func (d *Daemon) updateRepo(ctx context.Context, repoName string) {
	repoDirpath := config.GetRepoDirpath(d.agencDirpath, repoName)

	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	fetchCmd.Dir = repoDirpath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Repo update: git fetch failed for '%s': %v\n%s", repoName, err, string(output))
		return
	}

	localHash, err := gitRevParse(ctx, repoDirpath, "main")
	if err != nil {
		d.logger.Printf("Repo update: failed to rev-parse main for '%s': %v", repoName, err)
		return
	}

	remoteHash, err := gitRevParse(ctx, repoDirpath, "origin/main")
	if err != nil {
		d.logger.Printf("Repo update: failed to rev-parse origin/main for '%s': %v", repoName, err)
		return
	}

	if localHash == remoteHash {
		return
	}

	resetCmd := exec.CommandContext(ctx, "git", "reset", "--hard", "origin/main")
	resetCmd.Dir = repoDirpath
	if output, err := resetCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Repo update: git reset failed for '%s': %v\n%s", repoName, err, string(output))
		return
	}

	d.logger.Printf("Repo update: updated '%s' to %s", repoName, remoteHash[:8])
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
