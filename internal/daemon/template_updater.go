package daemon

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/config"
)

const (
	templateUpdateInterval = 60 * time.Second
)

// runTemplateUpdateLoop periodically fetches and fast-forwards agent template
// repos that are referenced by running missions.
func (d *Daemon) runTemplateUpdateLoop(ctx context.Context) {
	d.runTemplateUpdateCycle(ctx)

	ticker := time.NewTicker(templateUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runTemplateUpdateCycle(ctx)
		}
	}
}

func (d *Daemon) runTemplateUpdateCycle(ctx context.Context) {
	repoNames := d.collectRunningMissionRepos()
	for _, repoName := range repoNames {
		if ctx.Err() != nil {
			return
		}
		d.updateTemplate(ctx, repoName)
	}
}

// collectRunningMissionRepos returns the distinct repo names (agent templates)
// referenced by missions whose wrapper PID is still alive.
func (d *Daemon) collectRunningMissionRepos() []string {
	missions, err := d.db.ListMissions(false)
	if err != nil {
		d.logger.Printf("Template update: failed to list missions: %v", err)
		return nil
	}

	seen := make(map[string]bool)
	var repoNames []string
	for _, m := range missions {
		if m.AgentTemplate == "" {
			continue
		}
		if seen[m.AgentTemplate] {
			continue
		}

		pidFilepath := config.GetMissionPIDFilepath(d.agencDirpath, m.ID)
		pid, err := ReadPID(pidFilepath)
		if err != nil || pid == 0 {
			continue
		}
		if !IsProcessRunning(pid) {
			continue
		}

		seen[m.AgentTemplate] = true
		repoNames = append(repoNames, m.AgentTemplate)
	}
	return repoNames
}

func (d *Daemon) updateTemplate(ctx context.Context, repoName string) {
	repoDirpath := config.GetRepoDirpath(d.agencDirpath, repoName)

	fetchCmd := exec.CommandContext(ctx, "git", "fetch", "origin")
	fetchCmd.Dir = repoDirpath
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Template update: git fetch failed for '%s': %v\n%s", repoName, err, string(output))
		return
	}

	localHash, err := gitRevParse(ctx, repoDirpath, "main")
	if err != nil {
		d.logger.Printf("Template update: failed to rev-parse main for '%s': %v", repoName, err)
		return
	}

	remoteHash, err := gitRevParse(ctx, repoDirpath, "origin/main")
	if err != nil {
		d.logger.Printf("Template update: failed to rev-parse origin/main for '%s': %v", repoName, err)
		return
	}

	if localHash == remoteHash {
		return
	}

	resetCmd := exec.CommandContext(ctx, "git", "reset", "--hard", "origin/main")
	resetCmd.Dir = repoDirpath
	if output, err := resetCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Template update: git reset failed for '%s': %v\n%s", repoName, err, string(output))
		return
	}

	d.logger.Printf("Template update: updated '%s' to %s", repoName, remoteHash[:8])
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
