package server

import (
	"net/http"
	"time"

	"github.com/odyssey/agenc/internal/database"
)

// missionResponse is the JSON representation of a mission returned by the API.
type missionResponse struct {
	ID                     string     `json:"id"`
	ShortID                string     `json:"short_id"`
	Prompt                 string     `json:"prompt"`
	Status                 string     `json:"status"`
	GitRepo                string     `json:"git_repo"`
	LastHeartbeat          *time.Time `json:"last_heartbeat"`
	LastActive             *time.Time `json:"last_active"`
	SessionName            string     `json:"session_name"`
	SessionNameUpdatedAt   *time.Time `json:"session_name_updated_at"`
	CronID                 *string    `json:"cron_id"`
	CronName               *string    `json:"cron_name"`
	ConfigCommit           *string    `json:"config_commit"`
	TmuxPane               *string    `json:"tmux_pane"`
	PromptCount            int        `json:"prompt_count"`
	LastSummaryPromptCount int        `json:"last_summary_prompt_count"`
	AISummary              string     `json:"ai_summary"`
	TmuxWindowTitle        string     `json:"tmux_window_title"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

func toMissionResponse(m *database.Mission) missionResponse {
	return missionResponse{
		ID:                     m.ID,
		ShortID:                m.ShortID,
		Prompt:                 m.Prompt,
		Status:                 m.Status,
		GitRepo:                m.GitRepo,
		LastHeartbeat:          m.LastHeartbeat,
		LastActive:             m.LastActive,
		SessionName:            m.SessionName,
		SessionNameUpdatedAt:   m.SessionNameUpdatedAt,
		CronID:                 m.CronID,
		CronName:               m.CronName,
		ConfigCommit:           m.ConfigCommit,
		TmuxPane:               m.TmuxPane,
		PromptCount:            m.PromptCount,
		LastSummaryPromptCount: m.LastSummaryPromptCount,
		AISummary:              m.AISummary,
		TmuxWindowTitle:        m.TmuxWindowTitle,
		CreatedAt:              m.CreatedAt,
		UpdatedAt:              m.UpdatedAt,
	}
}

func toMissionResponses(missions []*database.Mission) []missionResponse {
	result := make([]missionResponse, len(missions))
	for i, m := range missions {
		result[i] = toMissionResponse(m)
	}
	return result
}

// handleListMissions handles GET /missions.
// Query params:
//   - include_archived=true — include archived missions
//   - tmux_pane=<id> — return the single mission running in the given tmux pane
func (s *Server) handleListMissions(w http.ResponseWriter, r *http.Request) {
	// If tmux_pane is specified, return the single mission for that pane
	tmuxPane := r.URL.Query().Get("tmux_pane")
	if tmuxPane != "" {
		mission, err := s.db.GetMissionByTmuxPane(tmuxPane)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if mission == nil {
			writeJSON(w, http.StatusOK, []missionResponse{})
			return
		}
		writeJSON(w, http.StatusOK, []missionResponse{toMissionResponse(mission)})
		return
	}

	params := database.ListMissionsParams{
		IncludeArchived: r.URL.Query().Get("include_archived") == "true",
	}
	if cronID := r.URL.Query().Get("cron_id"); cronID != "" {
		params.CronID = &cronID
	}

	missions, err := s.db.ListMissions(params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, toMissionResponses(missions))
}

// handleGetMission handles GET /missions/{id}.
func (s *Server) handleGetMission(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "mission not found: "+id)
		return
	}

	mission, err := s.db.GetMission(resolvedID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if mission == nil {
		writeError(w, http.StatusNotFound, "mission not found: "+id)
		return
	}

	writeJSON(w, http.StatusOK, toMissionResponse(mission))
}
