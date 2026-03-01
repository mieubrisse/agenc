package server

import (
	"bytes"
	"context"
	"log"
	"os/exec"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

const (
	// postUpdateHookTimeout is the hard timeout for hook execution.
	postUpdateHookTimeout = 30 * time.Minute

	// postUpdateHookWarnThreshold is how long a hook runs before warnings start.
	postUpdateHookWarnThreshold = 5 * time.Minute

	// postUpdateHookWarnInterval is how often to log a warning once the
	// threshold is exceeded.
	postUpdateHookWarnInterval = 5 * time.Minute

	// repoUpdateChannelSize is the buffer size for the update request channel.
	repoUpdateChannelSize = 64
)

// repoUpdateRequest represents a request to force-update a repo library clone.
type repoUpdateRequest struct {
	repoName             string
	refreshDefaultBranch bool
	forceRunHook         bool
}

// runRepoUpdateWorker reads update requests from the channel and processes
// them sequentially. Each request force-pulls the repo and runs the
// postUpdateHook if HEAD changed (or forceRunHook is set).
func (s *Server) runRepoUpdateWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case req := <-s.repoUpdateCh:
			s.processRepoUpdate(ctx, req)
		}
	}
}

// processRepoUpdate handles a single repo update request.
func (s *Server) processRepoUpdate(ctx context.Context, req repoUpdateRequest) {
	repoDirpath := config.GetRepoDirpath(s.agencDirpath, req.repoName)

	// Periodically refresh origin/HEAD so we track the remote's default branch
	if req.refreshDefaultBranch {
		setHeadCmd := exec.CommandContext(ctx, "git", "remote", "set-head", "origin", "--auto")
		setHeadCmd.Dir = repoDirpath
		if output, err := setHeadCmd.CombinedOutput(); err != nil {
			s.logger.Printf("Repo update: git remote set-head failed for '%s': %v\n%s", req.repoName, err, string(output))
		}
	}

	// Capture HEAD before update
	headBefore, _ := mission.GetHEAD(repoDirpath)

	if err := mission.ForceUpdateRepo(repoDirpath); err != nil {
		s.logger.Printf("Repo update: failed to update '%s': %v", req.repoName, err)
		return
	}

	// Capture HEAD after update
	headAfter, _ := mission.GetHEAD(repoDirpath)

	// Run garbage collection when new objects were fetched. This keeps the
	// library repo compact — without it, loose objects accumulate indefinitely
	// because fetch+reset never triggers git's built-in auto-gc.
	headChanged := headBefore != headAfter && headAfter != ""
	if headChanged {
		gcCmd := exec.CommandContext(ctx, "git", "gc")
		gcCmd.Dir = repoDirpath
		if output, err := gcCmd.CombinedOutput(); err != nil {
			s.logger.Printf("Repo update: git gc failed for '%s': %v\n%s", req.repoName, err, string(output))
		}
	}

	// Run hook if HEAD changed or forceRunHook is set
	if headChanged || req.forceRunHook {
		cfg, _, err := config.ReadAgencConfig(s.agencDirpath)
		if err != nil {
			s.logger.Printf("Repo update: failed to read config for hook: %v", err)
			return
		}
		rc, ok := cfg.GetRepoConfig(req.repoName)
		if ok && rc.PostUpdateHook != "" {
			if headChanged {
				s.logger.Printf("Repo update: HEAD changed for '%s' (%s -> %s), running postUpdateHook",
					req.repoName, abbreviateSHA(headBefore), abbreviateSHA(headAfter))
			} else {
				s.logger.Printf("Repo update: running postUpdateHook for '%s' (first clone)", req.repoName)
			}
			hookCtx, hookCancel := context.WithTimeout(ctx, postUpdateHookTimeout)
			defer hookCancel()
			runPostUpdateHook(hookCtx, s.logger, req.repoName, repoDirpath, rc.PostUpdateHook)
		}
	}
}

// runPostUpdateHook executes a shell command in the repo directory. It logs
// success or failure but never returns an error — hook failures are non-fatal.
func runPostUpdateHook(ctx context.Context, logger *log.Logger, repoName string, repoDirpath string, hookCmd string) {
	cmd := exec.CommandContext(ctx, "sh", "-c", hookCmd)
	cmd.Dir = repoDirpath

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Start a goroutine to emit warnings if the hook runs too long
	done := make(chan struct{})
	go func() {
		timer := time.NewTimer(postUpdateHookWarnThreshold)
		defer timer.Stop()
		for {
			select {
			case <-done:
				return
			case <-timer.C:
				logger.Printf("Repo update: WARN postUpdateHook for '%s' has been running for >%v, still waiting...",
					repoName, postUpdateHookWarnThreshold)
				timer.Reset(postUpdateHookWarnInterval)
			}
		}
	}()

	err := cmd.Run()
	close(done)

	if err != nil {
		logger.Printf("Repo update: postUpdateHook failed for '%s': %v\nstderr: %s",
			repoName, err, strings.TrimSpace(stderr.String()))
	} else {
		logger.Printf("Repo update: postUpdateHook succeeded for '%s'", repoName)
	}
}

// abbreviateSHA returns the first 8 characters of a git SHA, or the full
// string if shorter.
func abbreviateSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
