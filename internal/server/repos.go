package server

import (
	"net/http"
	"strings"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

// handlePushEvent handles POST /repos/{name...}/push-event.
// Triggers a force-update of the repo library clone so it matches the remote.
func (s *Server) handlePushEvent(w http.ResponseWriter, r *http.Request) error {
	// The repo name includes slashes (e.g., "github.com/owner/repo"), so we
	// need to extract it from the full path. The route is registered as
	// POST /repos/ and we parse the rest manually.
	repoName := strings.TrimPrefix(r.URL.Path, "/repos/")
	repoName = strings.TrimSuffix(repoName, "/push-event")

	if repoName == "" {
		return newHTTPError(http.StatusBadRequest, "repo name is required")
	}

	repoDirpath := config.GetRepoDirpath(s.agencDirpath, repoName)
	if err := mission.ForceUpdateRepo(repoDirpath); err != nil {
		s.logger.Printf("Push event: failed to update repo '%s': %v", repoName, err)
		return newHTTPErrorf(http.StatusInternalServerError, "failed to update repo: %s", err.Error())
	}

	s.logger.Printf("Push event: updated repo '%s'", repoName)
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	return nil
}
