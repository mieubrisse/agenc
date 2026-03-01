package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/mission"
)

// MissionResponse is the JSON representation of a mission returned by the API.
type MissionResponse struct {
	ID                     string     `json:"id"`
	ShortID                string     `json:"short_id"`
	Prompt                 string     `json:"prompt"`
	Status                 string     `json:"status"`
	GitRepo                string     `json:"git_repo"`
	LastHeartbeat          *time.Time `json:"last_heartbeat"`
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

// ToMission converts a MissionResponse to a database.Mission.
func (mr *MissionResponse) ToMission() *database.Mission {
	return &database.Mission{
		ID:                     mr.ID,
		ShortID:                mr.ShortID,
		Prompt:                 mr.Prompt,
		Status:                 mr.Status,
		GitRepo:                mr.GitRepo,
		LastHeartbeat:          mr.LastHeartbeat,
		SessionName:            mr.SessionName,
		SessionNameUpdatedAt:   mr.SessionNameUpdatedAt,
		CronID:                 mr.CronID,
		CronName:               mr.CronName,
		ConfigCommit:           mr.ConfigCommit,
		TmuxPane:               mr.TmuxPane,
		PromptCount:            mr.PromptCount,
		LastSummaryPromptCount: mr.LastSummaryPromptCount,
		AISummary:              mr.AISummary,
		TmuxWindowTitle:        mr.TmuxWindowTitle,
		CreatedAt:              mr.CreatedAt,
		UpdatedAt:              mr.UpdatedAt,
	}
}

func toMissionResponse(m *database.Mission) MissionResponse {
	return MissionResponse{
		ID:                     m.ID,
		ShortID:                m.ShortID,
		Prompt:                 m.Prompt,
		Status:                 m.Status,
		GitRepo:                m.GitRepo,
		LastHeartbeat:          m.LastHeartbeat,
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

func toMissionResponses(missions []*database.Mission) []MissionResponse {
	result := make([]MissionResponse, len(missions))
	for i, m := range missions {
		result[i] = toMissionResponse(m)
	}
	return result
}

// handleListMissions handles GET /missions.
// Query params:
//   - include_archived=true — include archived missions
//   - tmux_pane=<id> — return the single mission running in the given tmux pane
func (s *Server) handleListMissions(w http.ResponseWriter, r *http.Request) error {
	// If tmux_pane is specified, return the single mission for that pane
	tmuxPane := r.URL.Query().Get("tmux_pane")
	if tmuxPane != "" {
		mission, err := s.db.GetMissionByTmuxPane(tmuxPane)
		if err != nil {
			return newHTTPError(http.StatusInternalServerError, err.Error())
		}
		if mission == nil {
			writeJSON(w, http.StatusOK, []MissionResponse{})
			return nil
		}
		writeJSON(w, http.StatusOK, []MissionResponse{toMissionResponse(mission)})
		return nil
	}

	params := database.ListMissionsParams{
		IncludeArchived: r.URL.Query().Get("include_archived") == "true",
	}
	if cronID := r.URL.Query().Get("cron_id"); cronID != "" {
		params.CronID = &cronID
	}

	missions, err := s.db.ListMissions(params)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}

	writeJSON(w, http.StatusOK, toMissionResponses(missions))
	return nil
}

// handleGetMission handles GET /missions/{id}.
func (s *Server) handleGetMission(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	mission, err := s.db.GetMission(resolvedID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}
	if mission == nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	writeJSON(w, http.StatusOK, toMissionResponse(mission))
	return nil
}

// CreateMissionRequest is the JSON body for POST /missions.
type CreateMissionRequest struct {
	Repo        string `json:"repo"`
	Prompt      string `json:"prompt"`
	TmuxSession string `json:"tmux_session"`
	Headless    bool   `json:"headless"`
	Adjutant    bool   `json:"adjutant"`
	CronID      string `json:"cron_id"`
	CronName    string `json:"cron_name"`
	Timeout     string `json:"timeout"`
	CloneFrom   string `json:"clone_from"`
}

// handleCreateMission handles POST /missions.
// Creates a mission record, sets up the mission directory, and spawns the
// wrapper process in the caller's tmux session (or headless).
func (s *Server) handleCreateMission(w http.ResponseWriter, r *http.Request) error {
	var req CreateMissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	// Build creation params
	createParams := &database.CreateMissionParams{}
	if req.CronID != "" {
		createParams.CronID = &req.CronID
	}
	if req.CronName != "" {
		createParams.CronName = &req.CronName
	}
	if commitHash := claudeconfig.GetShadowRepoCommitHash(s.agencDirpath); commitHash != "" {
		createParams.ConfigCommit = &commitHash
	}

	// Handle clone-from request
	if req.CloneFrom != "" {
		return s.handleCreateClonedMission(w, req, createParams)
	}

	// Determine git repo name and clone source
	gitRepoName := req.Repo
	var gitCloneDirpath string
	if gitRepoName != "" {
		gitCloneDirpath = config.GetRepoDirpath(s.agencDirpath, gitRepoName)
	}

	// Create database record
	missionRecord, err := s.db.CreateMission(gitRepoName, createParams)
	if err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to create mission: %s", err.Error())
	}

	// For adjutant missions, write marker file before creating mission dir
	if req.Adjutant {
		missionDirpath := config.GetMissionDirpath(s.agencDirpath, missionRecord.ID)
		if err := os.MkdirAll(missionDirpath, 0755); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to create mission directory: %s", err.Error())
		}
		markerFilepath := config.GetMissionAdjutantMarkerFilepath(s.agencDirpath, missionRecord.ID)
		if err := os.WriteFile(markerFilepath, []byte{}, 0644); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to write adjutant marker: %s", err.Error())
		}
		// Adjutant missions have no repo
		gitRepoName = ""
		gitCloneDirpath = ""
	}

	// Create mission directory structure
	if _, err := mission.CreateMissionDir(s.agencDirpath, missionRecord.ID, gitRepoName, gitCloneDirpath); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to create mission directory: %s", err.Error())
	}

	// Spawn wrapper process
	if err := s.spawnWrapper(missionRecord, req); err != nil {
		s.logger.Printf("Failed to spawn wrapper for mission %s: %v", missionRecord.ShortID, err)
		// Mission was created successfully, just the wrapper failed
		// Return the mission but log the error
	}

	writeJSON(w, http.StatusCreated, toMissionResponse(missionRecord))
	return nil
}

// handleCreateClonedMission creates a mission by cloning the agent directory
// from an existing mission. The source mission's git_repo carries over.
func (s *Server) handleCreateClonedMission(w http.ResponseWriter, req CreateMissionRequest, createParams *database.CreateMissionParams) error {
	sourceID, err := s.db.ResolveMissionID(req.CloneFrom)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "source mission not found: "+req.CloneFrom)
	}
	sourceMission, err := s.db.GetMission(sourceID)
	if err != nil || sourceMission == nil {
		return newHTTPError(http.StatusNotFound, "source mission not found: "+req.CloneFrom)
	}

	missionRecord, err := s.db.CreateMission(sourceMission.GitRepo, createParams)
	if err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to create mission: %s", err.Error())
	}

	// Create empty mission dir structure, then copy agent dir from source
	if _, err := mission.CreateMissionDir(s.agencDirpath, missionRecord.ID, "", ""); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to create mission directory: %s", err.Error())
	}

	srcAgentDirpath := config.GetMissionAgentDirpath(s.agencDirpath, sourceMission.ID)
	dstAgentDirpath := config.GetMissionAgentDirpath(s.agencDirpath, missionRecord.ID)
	if err := mission.CopyAgentDir(srcAgentDirpath, dstAgentDirpath); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to copy agent directory: %s", err.Error())
	}

	// Spawn wrapper (may fail for interactive missions without tmux_session — that's OK)
	if err := s.spawnWrapper(missionRecord, req); err != nil {
		s.logger.Printf("Failed to spawn wrapper for cloned mission %s: %v", missionRecord.ShortID, err)
	}

	writeJSON(w, http.StatusCreated, toMissionResponse(missionRecord))
	return nil
}

// spawnWrapper launches the wrapper process for a mission.
// All missions run in a pool window. If TmuxSession is provided,
// the pool window is also linked into the caller's tmux session.
func (s *Server) spawnWrapper(missionRecord *database.Mission, req CreateMissionRequest) error {
	agencBinpath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve agenc binary path: %w", err)
	}

	// Build the wrapper command for the pool window.
	// --run-wrapper tells the resume command to run the wrapper directly
	// in the current process rather than going through the attach flow.
	resumeCmd := fmt.Sprintf("'%s' mission resume --run-wrapper %s", agencBinpath, missionRecord.ID)
	if req.Prompt != "" {
		resumeCmd += fmt.Sprintf(" --prompt '%s'", strings.ReplaceAll(req.Prompt, "'", "'\\''"))
	}

	// Create the wrapper window in the pool
	poolWindowTarget, err := s.createPoolWindow(missionRecord.ID, resumeCmd)
	if err != nil {
		return fmt.Errorf("failed to create pool window: %w", err)
	}

	// Link the pool window into the caller's session (if provided)
	tmuxSession := req.TmuxSession
	if tmuxSession != "" {
		if err := linkPoolWindow(poolWindowTarget, tmuxSession); err != nil {
			s.destroyPoolWindow(missionRecord.ID)
			return fmt.Errorf("failed to link pool window: %w", err)
		}
	}

	return nil
}

// lookupWindowTitle reads the config and returns the window title for the
// given repo, or empty string if not configured or on read error.
func (s *Server) lookupWindowTitle(gitRepoName string) string {
	if gitRepoName == "" {
		return ""
	}
	cfg, _, err := config.ReadAgencConfig(s.agencDirpath)
	if err != nil {
		return ""
	}
	return cfg.GetWindowTitle(gitRepoName)
}

const (
	stopTimeout = 10 * time.Second
	stopTick    = 100 * time.Millisecond
)

// handleStopMission handles POST /missions/{id}/stop.
// Sends SIGTERM to the wrapper process, polls for exit, falls back to SIGKILL.
func (s *Server) handleStopMission(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	missionRecord, err := s.db.GetMission(resolvedID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}
	if missionRecord == nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if err := s.stopWrapper(resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to stop wrapper: %s", err.Error())
	}

	// Clean up pool window (may already be gone if wrapper exited cleanly)
	s.destroyPoolWindow(resolvedID)

	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
	return nil
}

// stopWrapper gracefully stops a mission's wrapper process. Idempotent — if the
// wrapper is already stopped, returns nil.
func (s *Server) stopWrapper(missionID string) error {
	pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, missionID)
	pid, err := ReadPID(pidFilepath)
	if err != nil {
		return fmt.Errorf("failed to read mission PID file: %w", err)
	}

	if pid == 0 || !IsProcessRunning(pid) {
		os.Remove(pidFilepath)
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find wrapper process: %w", err)
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to wrapper (PID %d): %w", pid, err)
	}

	deadline := time.Now().Add(stopTimeout)
	for time.Now().Before(deadline) {
		if !IsProcessRunning(pid) {
			os.Remove(pidFilepath)
			return nil
		}
		time.Sleep(stopTick)
	}

	// Force kill if still running
	_ = process.Signal(syscall.SIGKILL)
	os.Remove(pidFilepath)
	return nil
}

// handleDeleteMission handles DELETE /missions/{id}.
// Stops the wrapper, removes the mission directory, and deletes the DB record.
func (s *Server) handleDeleteMission(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	missionRecord, err := s.db.GetMission(resolvedID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}
	if missionRecord == nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	// Stop the wrapper if running and clean up pool window
	if err := s.stopWrapper(resolvedID); err != nil {
		s.logger.Printf("Warning: failed to stop wrapper for mission %s: %v", id, err)
	}
	s.destroyPoolWindow(resolvedID)

	// Clean up per-mission Keychain credentials from the old auth system
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(s.agencDirpath, resolvedID)
	if err := claudeconfig.DeleteKeychainCredentials(claudeConfigDirpath); err != nil {
		s.logger.Printf("Warning: failed to delete Keychain credentials for mission %s: %v", id, err)
	}

	// Remove the mission directory
	missionDirpath := config.GetMissionDirpath(s.agencDirpath, resolvedID)
	if _, statErr := os.Stat(missionDirpath); statErr == nil {
		if err := os.RemoveAll(missionDirpath); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to remove mission directory: %s", err.Error())
		}
	}

	// Delete from database
	if err := s.db.DeleteMission(resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to delete mission: %s", err.Error())
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	return nil
}

// ReloadMissionRequest is the optional JSON body for POST /missions/{id}/reload.
type ReloadMissionRequest struct {
	TmuxSession string `json:"tmux_session"`
}

// handleReloadMission handles POST /missions/{id}/reload.
// Rebuilds the per-mission config directory and restarts the wrapper.
func (s *Server) handleReloadMission(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	missionRecord, err := s.db.GetMission(resolvedID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}
	if missionRecord == nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if missionRecord.Status == "archived" {
		return newHTTPError(http.StatusBadRequest, "cannot reload archived mission")
	}

	// Check for old-format mission (no agent/ subdirectory)
	agentDirpath := config.GetMissionAgentDirpath(s.agencDirpath, resolvedID)
	if _, statErr := os.Stat(agentDirpath); os.IsNotExist(statErr) {
		return newHTTPError(http.StatusBadRequest, "mission uses old directory format; archive and create a new mission")
	}

	// Detect tmux context and reload approach
	if missionRecord.TmuxPane != nil && *missionRecord.TmuxPane != "" {
		paneID := *missionRecord.TmuxPane
		if err := s.reloadMissionInTmux(missionRecord, paneID); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to reload mission: %s", err.Error())
		}
	} else {
		// Non-tmux: just stop the wrapper (CLI will handle resume)
		if err := s.stopWrapper(resolvedID); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to stop wrapper: %s", err.Error())
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded"})
	return nil
}

// AttachRequest is the JSON body for POST /missions/{id}/attach.
type AttachRequest struct {
	TmuxSession string `json:"tmux_session"`
}

// handleAttachMission handles POST /missions/{id}/attach.
// Ensures the mission's wrapper is running in the pool (lazy start), then links
// the pool window into the caller's tmux session.
func (s *Server) handleAttachMission(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	var req AttachRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}
	if req.TmuxSession == "" {
		return newHTTPError(http.StatusBadRequest, "tmux_session is required")
	}

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	missionRecord, err := s.db.GetMission(resolvedID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}
	if missionRecord == nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if missionRecord.Status == "archived" {
		return newHTTPError(http.StatusBadRequest, "cannot attach to archived mission")
	}

	// Lazy start: ensure wrapper is running in the pool
	if err := s.ensureWrapperInPool(missionRecord); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to start wrapper: %s", err.Error())
	}

	// Link the pool window into the caller's tmux session
	poolWindowTarget := fmt.Sprintf("%s:%s", poolSessionName, database.ShortID(resolvedID))
	if err := linkPoolWindow(poolWindowTarget, req.TmuxSession); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to link window: %s", err.Error())
	}

	s.logger.Printf("Attached mission %s to session %s", database.ShortID(resolvedID), req.TmuxSession)
	writeJSON(w, http.StatusOK, map[string]string{"status": "attached"})
	return nil
}

// DetachRequest is the JSON body for POST /missions/{id}/detach.
type DetachRequest struct {
	TmuxSession string `json:"tmux_session"`
}

// handleDetachMission handles POST /missions/{id}/detach.
// Unlinks the mission's window from the caller's tmux session. The wrapper
// keeps running in the pool.
func (s *Server) handleDetachMission(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	var req DetachRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}
	if req.TmuxSession == "" {
		return newHTTPError(http.StatusBadRequest, "tmux_session is required")
	}

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if err := unlinkPoolWindow(req.TmuxSession, resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to unlink window: %s", err.Error())
	}

	s.logger.Printf("Detached mission %s from session %s", database.ShortID(resolvedID), req.TmuxSession)
	writeJSON(w, http.StatusOK, map[string]string{"status": "detached"})
	return nil
}

// ensureWrapperInPool checks if the mission's wrapper is running in the pool.
// If not, it spawns a new wrapper in a pool window. This is the "lazy start"
// mechanism — wrappers are only started when someone attaches.
func (s *Server) ensureWrapperInPool(missionRecord *database.Mission) error {
	// Check if the wrapper is already running
	pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, missionRecord.ID)
	pid, err := ReadPID(pidFilepath)
	if err == nil && IsProcessRunning(pid) {
		// Wrapper is already running — ensure pool window exists too
		if poolWindowExists(missionRecord.ID) {
			return nil
		}
		// Wrapper running but no pool window (orphan from before pool existed).
		// We can't adopt it into the pool, so just return — the link-window will
		// fail and the caller will see the error.
		return nil
	}

	// Wrapper not running — spawn it in the pool
	agencBinpath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve agenc binary path: %w", err)
	}

	resumeCmd := fmt.Sprintf("'%s' mission resume --run-wrapper %s", agencBinpath, missionRecord.ID)
	poolWindowTarget, err := s.createPoolWindow(missionRecord.ID, resumeCmd)
	if err != nil {
		return fmt.Errorf("failed to create pool window: %w", err)
	}

	s.logger.Printf("Started wrapper in pool window %s for mission %s", poolWindowTarget, database.ShortID(missionRecord.ID))
	return nil
}

// handleArchiveMission handles POST /missions/{id}/archive.
// Stops the wrapper, cleans up the pool window, and marks the mission archived.
func (s *Server) handleArchiveMission(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	missionRecord, err := s.db.GetMission(resolvedID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}
	if missionRecord == nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if missionRecord.Status == "archived" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "archived"})
		return nil
	}

	// Stop wrapper and clean up pool window
	if err := s.stopWrapper(resolvedID); err != nil {
		s.logger.Printf("Warning: failed to stop wrapper for mission %s: %v", id, err)
	}
	s.destroyPoolWindow(resolvedID)

	if err := s.db.ArchiveMission(resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to archive mission: %s", err.Error())
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "archived"})
	return nil
}

// handleUnarchiveMission handles POST /missions/{id}/unarchive.
// Sets the mission status back to active.
func (s *Server) handleUnarchiveMission(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if err := s.db.UnarchiveMission(resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to unarchive mission: %s", err.Error())
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
	return nil
}

// UpdateMissionRequest is the JSON body for PATCH /missions/{id}.
// All fields are optional; only non-nil fields are applied.
type UpdateMissionRequest struct {
	ConfigCommit    *string `json:"config_commit,omitempty"`
	SessionName     *string `json:"session_name,omitempty"`
	Prompt          *string `json:"prompt,omitempty"`
	TmuxPane        *string `json:"tmux_pane,omitempty"`
	TmuxWindowTitle *string `json:"tmux_window_title,omitempty"`
}

// handleUpdateMission handles PATCH /missions/{id}.
// Updates specific mission fields without replacing the whole record.
func (s *Server) handleUpdateMission(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	var req UpdateMissionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if req.ConfigCommit != nil {
		if err := s.db.UpdateMissionConfigCommit(resolvedID, *req.ConfigCommit); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to update config_commit: %s", err.Error())
		}
	}
	if req.SessionName != nil {
		if err := s.db.UpdateMissionSessionName(resolvedID, *req.SessionName); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to update session_name: %s", err.Error())
		}
	}
	if req.Prompt != nil {
		if err := s.db.UpdateMissionPrompt(resolvedID, *req.Prompt); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to update prompt: %s", err.Error())
		}
	}
	if req.TmuxPane != nil {
		if *req.TmuxPane == "" {
			if err := s.db.ClearTmuxPane(resolvedID); err != nil {
				return newHTTPErrorf(http.StatusInternalServerError, "failed to clear tmux_pane: %s", err.Error())
			}
		} else {
			if err := s.db.SetTmuxPane(resolvedID, *req.TmuxPane); err != nil {
				return newHTTPErrorf(http.StatusInternalServerError, "failed to set tmux_pane: %s", err.Error())
			}
		}
	}
	if req.TmuxWindowTitle != nil {
		if err := s.db.SetMissionTmuxWindowTitle(resolvedID, *req.TmuxWindowTitle); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to set tmux_window_title: %s", err.Error())
		}
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	return nil
}

// handleHeartbeat handles POST /missions/{id}/heartbeat.
// Updates the mission's last_heartbeat timestamp.
func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if err := s.db.UpdateHeartbeat(resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to update heartbeat: %s", err.Error())
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// handleRecordPrompt handles POST /missions/{id}/prompt.
// Increments the prompt count for the mission.
func (s *Server) handleRecordPrompt(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if err := s.db.IncrementPromptCount(resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to increment prompt_count: %s", err.Error())
	}

	w.WriteHeader(http.StatusNoContent)
	return nil
}

// reloadMissionInTmux performs an in-place reload using tmux primitives.
func (s *Server) reloadMissionInTmux(missionRecord *database.Mission, paneID string) error {
	targetPane := "%" + paneID

	// Verify pane still exists
	checkCmd := exec.Command("tmux", "display-message", "-p", "-t", targetPane, "#{pane_id}")
	output, err := checkCmd.Output()
	if err != nil || strings.TrimSpace(string(output)) != targetPane {
		return fmt.Errorf("tmux pane %s no longer exists", paneID)
	}

	// Resolve window ID from pane ID
	windowCmd := exec.Command("tmux", "display-message", "-p", "-t", targetPane, "#{window_id}")
	output, err = windowCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to resolve window ID: %v", err)
	}
	windowID := strings.TrimSpace(string(output))

	// Set remain-on-exit on
	setCmd := exec.Command("tmux", "set-option", "-w", "-t", windowID, "remain-on-exit", "on")
	if output, err := setCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("failed to set remain-on-exit: %v (output: %s)", err, string(output))
	}

	// Ensure cleanup
	defer func() {
		restoreCmd := exec.Command("tmux", "set-option", "-w", "-t", windowID, "remain-on-exit", "off")
		restoreCmd.Run()
	}()

	// Stop wrapper
	if err := s.stopWrapper(missionRecord.ID); err != nil {
		return fmt.Errorf("failed to stop wrapper: %w", err)
	}

	// Respawn the pane
	agencBinpath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to resolve agenc binary path: %w", err)
	}

	resumeCommand := fmt.Sprintf("'%s' mission resume --run-wrapper %s", agencBinpath, missionRecord.ID)
	respawnCmd := exec.Command("tmux", "respawn-pane", "-k", "-t", targetPane, resumeCommand)
	if output, err := respawnCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("tmux respawn-pane failed: %v (output: %s)", err, string(output))
	}

	return nil
}
