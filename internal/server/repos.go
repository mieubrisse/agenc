package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
	"github.com/odyssey/agenc/internal/repo"
)

// handlePushEvent handles POST /repos/{name...}/push-event.
// Enqueues a force-update of the repo library clone and returns 202 Accepted.
func (s *Server) handlePushEvent(w http.ResponseWriter, r *http.Request) error {
	// The repo name includes slashes (e.g., "github.com/owner/repo"), so we
	// need to extract it from the full path. The route is registered as
	// POST /repos/ and we parse the rest manually.
	repoName := strings.TrimPrefix(r.URL.Path, "/repos/")
	repoName = strings.TrimSuffix(repoName, "/push-event")

	if repoName == "" {
		return newHTTPError(http.StatusBadRequest, "repo name is required")
	}

	// Verify the repo library directory exists
	repoDirpath := config.GetRepoDirpath(s.agencDirpath, repoName)
	if _, err := os.Stat(repoDirpath); os.IsNotExist(err) {
		return newHTTPError(http.StatusNotFound, "repo not found: "+repoName)
	}

	select {
	case s.repoUpdateCh <- repoUpdateRequest{repoName: repoName}:
		s.logger.Printf("Push event: enqueued update for '%s'", repoName)
		writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
	default:
		s.logger.Printf("Push event: channel full, could not enqueue '%s'", repoName)
		return newHTTPError(http.StatusServiceUnavailable, "update queue full")
	}
	return nil
}

// handleRepoAction dispatches POST /repos/{name...}/{action} requests to the
// appropriate handler based on the URL suffix. Repo names contain slashes, so
// we use a single catch-all route and dispatch by suffix.
func (s *Server) handleRepoAction(w http.ResponseWriter, r *http.Request) error {
	path := r.URL.Path
	switch {
	case strings.HasSuffix(path, "/push-event"):
		return s.handlePushEvent(w, r)
	case strings.HasSuffix(path, "/mv"):
		return s.handleMoveRepo(w, r)
	default:
		return newHTTPError(http.StatusNotFound, "unknown repo action")
	}
}

// gitCommandTimeout is the timeout for git operations in the server package.
const gitCommandTimeout = 30 * time.Second

// handleMoveRepo handles POST /repos/{name...}/mv.
// Renames a repo in the library: moves the directory, migrates config, and
// updates the clone's origin remote URL.
func (s *Server) handleMoveRepo(w http.ResponseWriter, r *http.Request) error {
	oldName := strings.TrimPrefix(r.URL.Path, "/repos/")
	oldName = strings.TrimSuffix(oldName, "/mv")

	if oldName == "" {
		return newHTTPError(http.StatusBadRequest, "repo name is required")
	}

	var req MoveRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	newName := req.NewName
	if newName == "" {
		return newHTTPError(http.StatusBadRequest, "new_name is required")
	}

	if oldName == newName {
		return newHTTPError(http.StatusBadRequest, "new name is the same as the old name")
	}

	// Validate old repo exists on disk
	oldDirpath := config.GetRepoDirpath(s.agencDirpath, oldName)
	if _, err := os.Stat(oldDirpath); os.IsNotExist(err) {
		return newHTTPErrorf(http.StatusNotFound, "repo not found: %s", oldName)
	}

	// Validate new name does not already exist on disk
	newDirpath := config.GetRepoDirpath(s.agencDirpath, newName)
	if _, err := os.Stat(newDirpath); err == nil {
		return newHTTPErrorf(http.StatusConflict, "repo already exists at new name: %s", newName)
	}

	// Acquire config lock — we need to check config and write atomically
	release, err := config.AcquireConfigLock(s.agencDirpath)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, "failed to acquire config lock: "+err.Error())
	}
	defer release()

	cfg, cm, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, "failed to read config: "+err.Error())
	}

	// Validate new name does not exist in config
	if _, hasNewConfig := cfg.GetRepoConfig(newName); hasNewConfig {
		return newHTTPErrorf(http.StatusConflict, "repo config already exists for new name: %s", newName)
	}

	// Create parent directories for the new path
	newParentDirpath := filepath.Dir(newDirpath)
	if err := os.MkdirAll(newParentDirpath, 0755); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to create parent directories for '%s': %v", newDirpath, err)
	}

	// Rename the directory
	if err := os.Rename(oldDirpath, newDirpath); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to rename repo directory: %v", err)
	}

	// Defer rollback of the rename in case config write fails
	shouldRollbackRename := true
	defer func() {
		if shouldRollbackRename {
			if rollbackErr := os.Rename(newDirpath, oldDirpath); rollbackErr != nil {
				s.logger.Printf("ACTION REQUIRED: failed to roll back repo rename from '%s' to '%s': %v — manually move '%s' back to '%s'",
					oldName, newName, rollbackErr, newDirpath, oldDirpath)
			}
		}
	}()

	// Migrate config: copy old config to new name, delete old key
	if rc, hasOldConfig := cfg.GetRepoConfig(oldName); hasOldConfig {
		cfg.SetRepoConfig(newName, rc)
		cfg.RemoveRepoConfig(oldName)
	}

	if err := config.WriteAgencConfig(s.agencDirpath, cfg, cm); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to write config: %v", err)
	}

	// Rename + config succeeded — do NOT roll back from here on
	shouldRollbackRename = false

	// Best-effort: update the clone's origin remote URL to match the new name
	currentRemoteURL, err := repo.GetOriginRemoteURL(newDirpath)
	if err != nil {
		s.logger.Printf("Warning: failed to read origin remote URL after rename: %v", err)
	} else {
		isSSH := strings.HasPrefix(currentRemoteURL, "git@")
		_, newCloneURL, parseErr := mission.ParseRepoReference(newName, isSSH, "")
		if parseErr != nil {
			s.logger.Printf("Warning: failed to derive new clone URL for '%s': %v", newName, parseErr)
		} else {
			ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
			defer cancel()

			setURLCmd := exec.CommandContext(ctx, "git", "remote", "set-url", "origin", newCloneURL)
			setURLCmd.Dir = newDirpath
			if output, setErr := setURLCmd.CombinedOutput(); setErr != nil {
				manualCmd := fmt.Sprintf("git -C %s remote set-url origin %s", newDirpath, newCloneURL)
				return newHTTPErrorf(http.StatusInternalServerError,
					"repo renamed successfully, but failed to update origin remote URL: %s\n\nACTION REQUIRED: run manually:\n  %s",
					strings.TrimSpace(string(output)), manualCmd)
			}
		}
	}

	// Clean up empty parent directories from the old path
	reposDirpath := config.GetReposDirpath(s.agencDirpath)
	cleanupEmptyParentDirs(oldDirpath, reposDirpath)

	writeJSON(w, http.StatusOK, map[string]string{
		"old_name": oldName,
		"new_name": newName,
	})
	return nil
}

// cleanupEmptyParentDirs walks up from dirpath, removing empty directories
// until it reaches stopAtDirpath (exclusive). Stops at the first non-empty
// directory.
func cleanupEmptyParentDirs(dirpath string, stopAtDirpath string) {
	for {
		parentDirpath := filepath.Dir(dirpath)
		if parentDirpath == dirpath || parentDirpath == stopAtDirpath {
			break
		}

		entries, err := os.ReadDir(parentDirpath)
		if err != nil || len(entries) > 0 {
			break
		}

		_ = os.Remove(parentDirpath)
		dirpath = parentDirpath
	}
}

// ============================================================================
// Repo CRUD types
// ============================================================================

// RepoResponse is the JSON shape returned by GET /repos.
type RepoResponse struct {
	Name   string `json:"name"`
	Synced bool   `json:"synced"`
	Path   string `json:"path"`
}

// AddRepoRequest is the JSON body for POST /repos.
type AddRepoRequest struct {
	Reference    string  `json:"reference"`
	AlwaysSynced *bool   `json:"always_synced,omitempty"`
	Emoji        *string `json:"emoji,omitempty"`
	Title        *string `json:"title,omitempty"`
}

// AddRepoResponse is the JSON shape returned by POST /repos.
type AddRepoResponse struct {
	Name           string `json:"name"`
	WasNewlyCloned bool   `json:"was_newly_cloned"`
}

// MoveRepoRequest is the JSON body for POST /repos/{name...}/mv.
type MoveRepoRequest struct {
	NewName string `json:"new_name"`
}

// ============================================================================
// Repo CRUD handlers
// ============================================================================

// handleListRepos handles GET /repos.
func (s *Server) handleListRepos(w http.ResponseWriter, r *http.Request) error {
	reposDirpath := config.GetReposDirpath(s.agencDirpath)
	repoNames, err := repo.FindReposOnDisk(reposDirpath)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, "failed to scan repos: "+err.Error())
	}

	cfg, _, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, "failed to read config: "+err.Error())
	}

	repos := make([]RepoResponse, len(repoNames))
	for i, name := range repoNames {
		repos[i] = RepoResponse{
			Name:   name,
			Synced: cfg.IsAlwaysSynced(name),
			Path:   config.GetRepoDirpath(s.agencDirpath, name),
		}
	}

	writeJSON(w, http.StatusOK, repos)
	return nil
}

// handleAddRepo handles POST /repos.
func (s *Server) handleAddRepo(w http.ResponseWriter, r *http.Request) error {
	var req AddRepoRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	if req.Reference == "" {
		return newHTTPError(http.StatusBadRequest, "reference is required")
	}

	defaultGitHubUser := repo.GetDefaultGitHubUser()

	if !repo.LooksLikeRepoReference(req.Reference) {
		return newHTTPErrorf(http.StatusBadRequest,
			"'%s' is not a valid repo reference; expected owner/repo, a URL, or a local path", req.Reference)
	}

	result, err := repo.ResolveAsRepoReference(s.agencDirpath, req.Reference, defaultGitHubUser)
	if err != nil {
		return newHTTPErrorf(http.StatusBadRequest, "failed to resolve repo '%s': %v", req.Reference, err)
	}

	// Update config if flags were provided
	if req.AlwaysSynced != nil || req.Emoji != nil || req.Title != nil {
		release, err := config.AcquireConfigLock(s.agencDirpath)
		if err != nil {
			return newHTTPError(http.StatusInternalServerError, "failed to acquire config lock: "+err.Error())
		}
		defer release()

		cfg, cm, err := config.ReadAgencConfig(s.agencDirpath)
		if err != nil {
			return newHTTPError(http.StatusInternalServerError, "failed to read config: "+err.Error())
		}

		rc, _ := cfg.GetRepoConfig(result.RepoName)
		if req.AlwaysSynced != nil {
			rc.AlwaysSynced = *req.AlwaysSynced
		}
		if req.Emoji != nil {
			rc.Emoji = *req.Emoji
		}
		if req.Title != nil {
			rc.Title = *req.Title
		}
		cfg.SetRepoConfig(result.RepoName, rc)

		if err := config.WriteAgencConfig(s.agencDirpath, cfg, cm); err != nil {
			return newHTTPError(http.StatusInternalServerError, "failed to write config: "+err.Error())
		}
	}

	writeJSON(w, http.StatusCreated, AddRepoResponse{
		Name:           result.RepoName,
		WasNewlyCloned: result.WasNewlyCloned,
	})
	return nil
}

// handleRemoveRepo handles DELETE /repos/{name...}.
func (s *Server) handleRemoveRepo(w http.ResponseWriter, r *http.Request) error {
	repoName := strings.TrimPrefix(r.URL.Path, "/repos/")
	if repoName == "" {
		return newHTTPError(http.StatusBadRequest, "repo name is required")
	}

	repoDirpath := config.GetRepoDirpath(s.agencDirpath, repoName)
	_, statErr := os.Stat(repoDirpath)
	existsOnDisk := statErr == nil

	release, err := config.AcquireConfigLock(s.agencDirpath)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, "failed to acquire config lock: "+err.Error())
	}
	defer release()

	cfg, cm, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, "failed to read config: "+err.Error())
	}

	_, hasRepoConfig := cfg.GetRepoConfig(repoName)

	if !existsOnDisk && !hasRepoConfig {
		return newHTTPError(http.StatusNotFound, "repo not found: "+repoName)
	}

	if hasRepoConfig {
		cfg.RemoveRepoConfig(repoName)
		if err := config.WriteAgencConfig(s.agencDirpath, cfg, cm); err != nil {
			return newHTTPError(http.StatusInternalServerError, "failed to write config: "+err.Error())
		}
	}

	if existsOnDisk {
		if err := os.RemoveAll(repoDirpath); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to remove repo directory: %v", err)
		}
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}
