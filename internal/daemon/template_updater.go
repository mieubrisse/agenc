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
	// heartbeatStalenessThreshold defines how recent a mission's heartbeat
	// must be for its repo to be included in the force-pull sweep.
	heartbeatStalenessThreshold = 5 * time.Minute
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

	// Always update the managed config repo (unconditionally, every cycle)
	if configRepoName := d.getManagedConfigRepoName(); configRepoName != "" {
		d.updateRepo(ctx, configRepoName, refreshDefaultBranch)
	}

	cfg, err := config.ReadAgencConfig(d.agencDirpath)
	if err != nil {
		d.logger.Printf("Repo update: failed to read config: %v", err)
		return
	}

	// Collect all unique repos to sync: agent templates + synced repos + active mission repos
	reposToSync := make(map[string]bool)
	for repo := range cfg.AgentTemplates {
		reposToSync[repo] = true
	}
	for _, repo := range cfg.SyncedRepos {
		reposToSync[repo] = true
	}

	// Include repos from missions with a recent heartbeat (active wrapper)
	now := time.Now().UTC()
	missions, err := d.db.ListMissions(false)
	if err != nil {
		d.logger.Printf("Repo update: failed to list missions: %v", err)
	} else {
		for _, m := range missions {
			if m.GitRepo == "" || m.LastHeartbeat == nil {
				continue
			}
			if now.Sub(*m.LastHeartbeat) <= heartbeatStalenessThreshold {
				reposToSync[m.GitRepo] = true
			}
		}
	}

	for repo := range reposToSync {
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

	// Periodically refresh origin/HEAD so we track the remote's default branch
	if refreshDefaultBranch {
		setHeadCmd := exec.CommandContext(ctx, "git", "remote", "set-head", "origin", "--auto")
		setHeadCmd.Dir = repoDirpath
		if output, err := setHeadCmd.CombinedOutput(); err != nil {
			d.logger.Printf("Repo update: git remote set-head failed for '%s': %v\n%s", repoName, err, string(output))
		}
	}

	if err := mission.ForceUpdateRepo(repoDirpath); err != nil {
		d.logger.Printf("Repo update: failed to update '%s': %v", repoName, err)
	}
}

// getManagedConfigRepoName returns the repo name of the managed config repo
// if $AGENC_DIRPATH/config is a symlink pointing into the repo library.
// Returns empty string if config is not a symlink or points outside the library.
func (d *Daemon) getManagedConfigRepoName() string {
	configDirpath := config.GetConfigDirpath(d.agencDirpath)

	info, err := os.Lstat(configDirpath)
	if err != nil {
		return ""
	}
	if info.Mode()&os.ModeSymlink == 0 {
		return "" // Not a symlink
	}

	target, err := os.Readlink(configDirpath)
	if err != nil {
		return ""
	}

	reposDirPrefix := config.GetReposDirpath(d.agencDirpath) + "/"
	if !strings.HasPrefix(target, reposDirPrefix) {
		return "" // Symlink points outside the repo library
	}

	return strings.TrimPrefix(target, reposDirPrefix)
}
