package server

import (
	"net/http"
	"os"
	"strings"

	"github.com/odyssey/agenc/internal/config"
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
