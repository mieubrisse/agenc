package server

import (
	"net/http"
	"strconv"
	"time"
)

// SearchMissionsResponse is a single result from mission search.
type SearchMissionsResponse struct {
	MissionID            string  `json:"mission_id"`
	ShortID              string  `json:"short_id"`
	SessionID            string  `json:"session_id"`
	Snippet              string  `json:"snippet"`
	Rank                 float64 `json:"rank"`
	GitRepo              string  `json:"git_repo"`
	Status               string  `json:"status"`
	Prompt               string  `json:"prompt"`
	ResolvedSessionTitle string  `json:"resolved_session_title"`
	LastHeartbeat        *string `json:"last_heartbeat"`
}

// handleSearchMissions handles GET /missions/search?q=<query>&limit=<n>.
func (s *Server) handleSearchMissions(w http.ResponseWriter, r *http.Request) error {
	query := r.URL.Query().Get("q")
	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if query == "" {
		writeJSON(w, http.StatusOK, []SearchMissionsResponse{})
		return nil
	}

	results, err := s.db.SearchMissions(query, limit)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}

	responses := make([]SearchMissionsResponse, 0, len(results))
	for _, sr := range results {
		resp := SearchMissionsResponse{
			MissionID: sr.MissionID,
			SessionID: sr.SessionID,
			Snippet:   sr.Snippet,
			Rank:      sr.Rank,
		}

		mission, err := s.db.GetMission(sr.MissionID)
		if err == nil && mission != nil {
			resp.ShortID = mission.ShortID
			resp.GitRepo = mission.GitRepo
			resp.Status = mission.Status
			resp.Prompt = mission.Prompt
			if activeSession, sessionErr := s.db.GetActiveSession(sr.MissionID); sessionErr == nil {
				resp.ResolvedSessionTitle = resolveSessionTitle(activeSession)
			}
			if mission.LastHeartbeat != nil {
				ts := mission.LastHeartbeat.Format(time.RFC3339)
				resp.LastHeartbeat = &ts
			}
		}

		responses = append(responses, resp)
	}

	writeJSON(w, http.StatusOK, responses)
	return nil
}
