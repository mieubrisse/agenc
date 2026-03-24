package server

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/odyssey/agenc/internal/config"
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

// ============================================================================
// Repo CRUD types
// ============================================================================

// RepoResponse is the JSON shape returned by GET /repos.
type RepoResponse struct {
	Name   string `json:"name"`
	Synced bool   `json:"synced"`
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
