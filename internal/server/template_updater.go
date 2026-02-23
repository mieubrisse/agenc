package server

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
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

// runRepoUpdateLoop periodically fetches and fast-forwards synced repos
// and active mission repos.
func (s *Server) runRepoUpdateLoop(ctx context.Context) {
	s.runRepoUpdateCycle(ctx)

	ticker := time.NewTicker(repoUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runRepoUpdateCycle(ctx)
		}
	}
}

const (
	// refreshDefaultBranchInterval controls how often (in cycles) the daemon
	// runs "git remote set-head origin --auto" to keep origin/HEAD current.
	refreshDefaultBranchInterval = 10
)

func (s *Server) runRepoUpdateCycle(ctx context.Context) {
	s.repoUpdateCycleCount++
	refreshDefaultBranch := s.repoUpdateCycleCount%refreshDefaultBranchInterval == 0

	cfg, _, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		s.logger.Printf("Repo update: failed to read config: %v", err)
		return
	}

	// Collect all unique repos to sync: synced repos + claude config repo + active mission repos
	reposToSync := make(map[string]bool)
	for _, repo := range cfg.GetAllSyncedRepos() {
		reposToSync[repo] = true
	}

	// Include repos from missions with a recent heartbeat (active wrapper)
	now := time.Now().UTC()
	missions, err := s.db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		s.logger.Printf("Repo update: failed to list missions: %v", err)
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

	preferSSH := mission.DetectPreferredProtocol(s.agencDirpath)

	for repo := range reposToSync {
		if ctx.Err() != nil {
			return
		}

		repoName, cloneURL, err := mission.ParseRepoReference(repo, preferSSH, "")
		if err != nil {
			s.logger.Printf("Repo update: invalid repo '%s': %v", repo, err)
			continue
		}

		if err := s.ensureRepoCloned(ctx, repoName, cloneURL); err != nil {
			s.logger.Printf("Repo update: clone failed for '%s': %v", repoName, err)
			continue
		}

		s.updateRepo(ctx, repoName, refreshDefaultBranch)
	}
}

// ensureRepoCloned clones the repo if it doesn't already exist. Unlike
// mission.EnsureRepoClone, this uses CombinedOutput and logs instead of
// writing to stdout/stderr.
func (s *Server) ensureRepoCloned(ctx context.Context, repoName string, cloneURL string) error {
	cloneDirpath := config.GetRepoDirpath(s.agencDirpath, repoName)

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
		s.logger.Printf("Repo update: git clone output for '%s': %s", repoName, strings.TrimSpace(string(output)))
		return err
	}

	s.logger.Printf("Repo update: cloned '%s' from %s", repoName, cloneURL)
	return nil
}

func (s *Server) updateRepo(ctx context.Context, repoName string, refreshDefaultBranch bool) {
	repoDirpath := config.GetRepoDirpath(s.agencDirpath, repoName)

	// Periodically refresh origin/HEAD so we track the remote's default branch
	if refreshDefaultBranch {
		setHeadCmd := exec.CommandContext(ctx, "git", "remote", "set-head", "origin", "--auto")
		setHeadCmd.Dir = repoDirpath
		if output, err := setHeadCmd.CombinedOutput(); err != nil {
			s.logger.Printf("Repo update: git remote set-head failed for '%s': %v\n%s", repoName, err, string(output))
		}
	}

	if err := mission.ForceUpdateRepo(repoDirpath); err != nil {
		s.logger.Printf("Repo update: failed to update '%s': %v", repoName, err)
	}
}
