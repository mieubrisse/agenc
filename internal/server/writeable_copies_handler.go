package server

import (
	"net/http"
	"os"
	"sort"
)

// WriteableCopyResponse is the JSON shape returned for each writeable copy
// from GET /writeable-copies.
type WriteableCopyResponse struct {
	RepoName       string `json:"repo_name"`
	Path           string `json:"path"`
	Status         string `json:"status"`                    // "ok" | "paused" | "missing"
	PausedReason   string `json:"paused_reason,omitempty"`   // populated when Status == "paused"
	NotificationID string `json:"notification_id,omitempty"` // pause-linked notification (when paused)
	PausedAt       string `json:"paused_at,omitempty"`
}

func (s *Server) handleListWriteableCopies(w http.ResponseWriter, r *http.Request) error {
	cfg := s.getConfig()
	all := cfg.GetAllWriteableCopies()

	pauses, err := s.db.ListPauses()
	if err != nil {
		s.logger.Printf("ListPauses failed: %v", err)
		return newHTTPErrorf(http.StatusInternalServerError, "failed to list pauses: %v", err)
	}
	pauseByRepo := make(map[string]string)
	pauseReasonByRepo := make(map[string]string)
	pauseAtByRepo := make(map[string]string)
	for _, p := range pauses {
		pauseByRepo[p.RepoName] = p.NotificationID
		pauseReasonByRepo[p.RepoName] = p.PausedReason
		pauseAtByRepo[p.RepoName] = p.PausedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}

	out := make([]WriteableCopyResponse, 0, len(all))
	for repoName, path := range all {
		entry := WriteableCopyResponse{
			RepoName: repoName,
			Path:     path,
		}
		if _, err := os.Stat(path); err != nil {
			entry.Status = "missing"
		} else if notifID, paused := pauseByRepo[repoName]; paused {
			entry.Status = "paused"
			entry.PausedReason = pauseReasonByRepo[repoName]
			entry.NotificationID = notifID
			entry.PausedAt = pauseAtByRepo[repoName]
		} else {
			entry.Status = "ok"
		}
		out = append(out, entry)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].RepoName < out[j].RepoName })

	writeJSON(w, http.StatusOK, out)
	return nil
}
