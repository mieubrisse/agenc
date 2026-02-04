package daemon

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/config"
)

const (
	configAutoCommitInterval = 10 * time.Minute
)

// runConfigAutoCommitLoop periodically checks whether the config directory is
// a Git repository with uncommitted changes, and if so auto-commits and pushes.
func (d *Daemon) runConfigAutoCommitLoop(ctx context.Context) {
	// Run first cycle after a short delay so it doesn't race with startup I/O
	select {
	case <-ctx.Done():
		return
	case <-time.After(configAutoCommitInterval):
		d.runConfigAutoCommitCycle(ctx)
	}

	ticker := time.NewTicker(configAutoCommitInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.runConfigAutoCommitCycle(ctx)
		}
	}
}

// runConfigAutoCommitCycle performs a single auto-commit/push cycle for the
// config directory.
func (d *Daemon) runConfigAutoCommitCycle(ctx context.Context) {
	configDirpath := config.GetConfigDirpath(d.agencDirpath)

	if !isGitRepo(configDirpath) {
		return
	}

	if !hasUncommittedChanges(ctx, configDirpath) {
		return
	}

	// Stage all changes
	addCmd := exec.CommandContext(ctx, "git", "add", "-A")
	addCmd.Dir = configDirpath
	if output, err := addCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Config auto-commit: git add failed: %v\n%s", err, strings.TrimSpace(string(output)))
		return
	}

	// Commit with timestamp
	timestamp := time.Now().UTC().Format(time.RFC3339)
	commitMsg := fmt.Sprintf("%s agenc auto-commit", timestamp)
	commitCmd := exec.CommandContext(ctx, "git", "commit", "-m", commitMsg)
	commitCmd.Dir = configDirpath
	if output, err := commitCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Config auto-commit: git commit failed: %v\n%s", err, strings.TrimSpace(string(output)))
		return
	}

	d.logger.Printf("Config auto-commit: committed changes: %s", commitMsg)

	if !hasOriginRemote(ctx, configDirpath) {
		return
	}

	// Push to remote
	pushCmd := exec.CommandContext(ctx, "git", "push")
	pushCmd.Dir = configDirpath
	if output, err := pushCmd.CombinedOutput(); err != nil {
		d.logger.Printf("Config auto-commit: git push failed: %v\n%s", err, strings.TrimSpace(string(output)))
		return
	}

	d.logger.Println("Config auto-commit: pushed to remote")
}

// isGitRepo returns true if the given directory contains a .git subdirectory.
func isGitRepo(dirpath string) bool {
	gitDirpath := dirpath + "/.git"
	info, err := os.Stat(gitDirpath)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// hasOriginRemote returns true if the git repository has an "origin" remote configured.
func hasOriginRemote(ctx context.Context, repoDirpath string) bool {
	cmd := exec.CommandContext(ctx, "git", "remote", "get-url", "origin")
	cmd.Dir = repoDirpath
	return cmd.Run() == nil
}

// hasUncommittedChanges returns true if the git working tree has any staged,
// unstaged, or untracked changes.
func hasUncommittedChanges(ctx context.Context, repoDirpath string) bool {
	// git status --porcelain outputs nothing when the working tree is clean
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = repoDirpath
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(output))) > 0
}
