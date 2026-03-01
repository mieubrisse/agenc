package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/odyssey/agenc/internal/database"
)

// SessionResponse is the JSON representation of a session returned by the API.
type SessionResponse struct {
	ID               string    `json:"id"`
	MissionID        string    `json:"mission_id"`
	CustomTitle      string    `json:"custom_title"`
	AgencCustomTitle string    `json:"agenc_custom_title"`
	AutoSummary      string    `json:"auto_summary"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func toSessionResponse(s *database.Session) SessionResponse {
	return SessionResponse{
		ID:               s.ID,
		MissionID:        s.MissionID,
		CustomTitle:      s.CustomTitle,
		AgencCustomTitle: s.AgencCustomTitle,
		AutoSummary:      s.AutoSummary,
		CreatedAt:        s.CreatedAt,
		UpdatedAt:        s.UpdatedAt,
	}
}

func toSessionResponses(sessions []*database.Session) []SessionResponse {
	result := make([]SessionResponse, len(sessions))
	for i, s := range sessions {
		result[i] = toSessionResponse(s)
	}
	return result
}

// handleListSessions handles GET /sessions?mission_id={id}.
// Returns sessions for a mission, ordered by updated_at descending.
func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) error {
	missionID := r.URL.Query().Get("mission_id")
	if missionID == "" {
		return newHTTPError(http.StatusBadRequest, "mission_id query parameter is required")
	}

	resolvedID, err := s.db.ResolveMissionID(missionID)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+missionID)
	}

	sessions, err := s.db.ListSessionsByMission(resolvedID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}

	writeJSON(w, http.StatusOK, toSessionResponses(sessions))
	return nil
}

// UpdateSessionRequest is the JSON body for PATCH /sessions/{id}.
type UpdateSessionRequest struct {
	AgencCustomTitle *string `json:"agenc_custom_title,omitempty"`
}

// handleUpdateSession handles PATCH /sessions/{id}.
// Updates session fields and triggers tmux window title reconciliation.
func (s *Server) handleUpdateSession(w http.ResponseWriter, r *http.Request) error {
	sessionID := r.PathValue("id")

	var req UpdateSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	// Look up the session to verify it exists and get its mission_id
	session, err := s.db.GetSession(sessionID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}
	if session == nil {
		return newHTTPError(http.StatusNotFound, "session not found: "+sessionID)
	}

	if req.AgencCustomTitle != nil {
		if err := s.db.UpdateSessionAgencCustomTitle(sessionID, *req.AgencCustomTitle); err != nil {
			return newHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	// Trigger tmux window title reconciliation for the owning mission
	s.reconcileTmuxWindowTitle(session.MissionID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	return nil
}
