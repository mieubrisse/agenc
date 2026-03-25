package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/mission"
)

// MissionResponse is the JSON representation of a mission returned by the API.
type MissionResponse struct {
	ID                   string     `json:"id"`
	ShortID              string     `json:"short_id"`
	Prompt               string     `json:"prompt"`
	Status               string     `json:"status"`
	GitRepo              string     `json:"git_repo"`
	LastHeartbeat        *time.Time `json:"last_heartbeat"`
	LastUserPromptAt     *time.Time `json:"last_user_prompt_at"`
	SessionName          string     `json:"session_name"`
	SessionNameUpdatedAt *time.Time `json:"session_name_updated_at"`
	Source               *string    `json:"source"`
	SourceID             *string    `json:"source_id"`
	SourceMetadata       *string    `json:"source_metadata"`
	ConfigCommit         *string    `json:"config_commit"`
	TmuxPane             *string    `json:"tmux_pane"`
	PromptCount          int        `json:"prompt_count"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`

	// ResolvedSessionTitle is derived from the active session's title chain:
	// custom_title > agenc_custom_title > auto_summary. Empty if no session exists.
	ResolvedSessionTitle string `json:"resolved_session_title"`

	// IsAdjutant is true if the mission has a .adjutant marker file.
	IsAdjutant bool `json:"is_adjutant"`

	// ClaudeState is the current state of Claude in this mission. Nil when the
	// wrapper is not running. Possible values: "idle", "busy", "needs_attention".
	ClaudeState *string `json:"claude_state"`
}

// ToMission converts a MissionResponse to a database.Mission.
func (mr *MissionResponse) ToMission() *database.Mission {
	return &database.Mission{
		ID:                   mr.ID,
		ShortID:              mr.ShortID,
		Prompt:               mr.Prompt,
		Status:               mr.Status,
		GitRepo:              mr.GitRepo,
		LastHeartbeat:        mr.LastHeartbeat,
		LastUserPromptAt:     mr.LastUserPromptAt,
		SessionName:          mr.SessionName,
		SessionNameUpdatedAt: mr.SessionNameUpdatedAt,
		Source:               mr.Source,
		SourceID:             mr.SourceID,
		SourceMetadata:       mr.SourceMetadata,
		ConfigCommit:         mr.ConfigCommit,
		TmuxPane:             mr.TmuxPane,
		PromptCount:          mr.PromptCount,
		CreatedAt:            mr.CreatedAt,
		UpdatedAt:            mr.UpdatedAt,
		ResolvedSessionTitle: mr.ResolvedSessionTitle,
		IsAdjutant:           mr.IsAdjutant,
		ClaudeState:          mr.ClaudeState,
	}
}

func toMissionResponse(m *database.Mission) MissionResponse {
	return MissionResponse{
		ID:                   m.ID,
		ShortID:              m.ShortID,
		Prompt:               m.Prompt,
		Status:               m.Status,
		GitRepo:              m.GitRepo,
		LastHeartbeat:        m.LastHeartbeat,
		LastUserPromptAt:     m.LastUserPromptAt,
		SessionName:          m.SessionName,
		SessionNameUpdatedAt: m.SessionNameUpdatedAt,
		Source:               m.Source,
		SourceID:             m.SourceID,
		SourceMetadata:       m.SourceMetadata,
		ConfigCommit:         m.ConfigCommit,
		TmuxPane:             m.TmuxPane,
		PromptCount:          m.PromptCount,
		CreatedAt:            m.CreatedAt,
		UpdatedAt:            m.UpdatedAt,
		ResolvedSessionTitle: m.ResolvedSessionTitle,
		IsAdjutant:           m.IsAdjutant,
	}
}

func toMissionResponses(missions []*database.Mission) []MissionResponse {
	result := make([]MissionResponse, len(missions))
	for i, m := range missions {
		result[i] = toMissionResponse(m)
	}
	return result
}

// resolveSessionTitle returns the best display title from a session using
// the same priority chain as tmux reconciliation (custom_title >
// agenc_custom_title > auto_summary). Returns "" if no title is available.
func resolveSessionTitle(s *database.Session) string {
	if s == nil {
		return ""
	}
	if s.CustomTitle != "" {
		return s.CustomTitle
	}
	if s.AgencCustomTitle != "" {
		return s.AgencCustomTitle
	}
	return s.AutoSummary
}

// enrichMissionWithSessionTitle populates the ResolvedSessionTitle field
// on a mission by looking up its active session.
func (s *Server) enrichMissionWithSessionTitle(m *database.Mission) {
	activeSession, err := s.db.GetActiveSession(m.ID)
	if err != nil {
		return
	}
	m.ResolvedSessionTitle = resolveSessionTitle(activeSession)
}

const wrapperQueryTimeout = 500 * time.Millisecond

// wrapperStatusResponse mirrors the wrapper's StatusResponse for JSON decoding
// without importing the wrapper package (which would create an import cycle).
type wrapperStatusResponse struct {
	ClaudeState string `json:"claude_state"`
}

// queryWrapperClaudeState queries the wrapper for a running mission's Claude state.
// Returns nil if the wrapper is not running or unreachable.
func (s *Server) queryWrapperClaudeState(missionID string) *string {
	pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, missionID)
	pid, err := ReadPID(pidFilepath)
	if err != nil || pid == 0 || !IsProcessRunning(pid) {
		return nil
	}

	socketFilepath := config.GetMissionSocketFilepath(s.agencDirpath, missionID)
	httpClient := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.DialTimeout("unix", socketFilepath, wrapperQueryTimeout)
			},
		},
		Timeout: wrapperQueryTimeout,
	}

	resp, err := httpClient.Get("http://wrapper/status")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var status wrapperStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil
	}
	return &status.ClaudeState
}

// enrichMissionResponse populates transient fields (ClaudeState, IsAdjutant)
// by querying the running wrapper and checking the filesystem.
func (s *Server) enrichMissionResponse(resp *MissionResponse) {
	resp.ClaudeState = s.queryWrapperClaudeState(resp.ID)
	resp.IsAdjutant = config.IsMissionAdjutant(s.agencDirpath, resp.ID)
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
		s.enrichMissionWithSessionTitle(mission)
		resp := toMissionResponse(mission)
		s.enrichMissionResponse(&resp)
		writeJSON(w, http.StatusOK, []MissionResponse{resp})
		return nil
	}

	params := database.ListMissionsParams{
		IncludeArchived: r.URL.Query().Get("include_archived") == "true",
	}
	if source := r.URL.Query().Get("source"); source != "" {
		params.Source = &source
	}
	if sourceID := r.URL.Query().Get("source_id"); sourceID != "" {
		params.SourceID = &sourceID
	}

	missions, err := s.db.ListMissions(params)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}

	for _, m := range missions {
		s.enrichMissionWithSessionTitle(m)
	}

	responses := toMissionResponses(missions)

	var wg sync.WaitGroup
	for i := range responses {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.enrichMissionResponse(&responses[idx])
		}(i)
	}
	wg.Wait()

	writeJSON(w, http.StatusOK, responses)
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

	s.enrichMissionWithSessionTitle(mission)
	resp := toMissionResponse(mission)
	s.enrichMissionResponse(&resp)
	writeJSON(w, http.StatusOK, resp)
	return nil
}

// CreateMissionRequest is the JSON body for POST /missions.
type CreateMissionRequest struct {
	Repo           string `json:"repo"`
	Prompt         string `json:"prompt"`
	TmuxSession    string `json:"tmux_session"`
	Headless       bool   `json:"headless"`
	Adjutant       bool   `json:"adjutant"`
	Source         string `json:"source"`
	SourceID       string `json:"source_id"`
	SourceMetadata string `json:"source_metadata"`
	CloneFrom      string `json:"clone_from"`
	NoFocus        bool   `json:"no_focus"`
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
	if req.Source != "" {
		createParams.Source = &req.Source
	}
	if req.SourceID != "" {
		createParams.SourceID = &req.SourceID
	}
	if req.SourceMetadata != "" {
		createParams.SourceMetadata = &req.SourceMetadata
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

	// Force-pull the library clone if it hasn't been fetched recently.
	// This prevents missions from starting with a stale copy of the repo.
	if gitCloneDirpath != "" && mission.IsRepoStale(gitCloneDirpath, 24*time.Hour) {
		s.logger.Printf("Mission create: force-pulling stale repo '%s' before copy", gitRepoName)
		if err := mission.ForceUpdateRepo(gitCloneDirpath); err != nil {
			s.logger.Printf("Mission create: failed to pull stale repo '%s': %v (proceeding with stale copy)", gitRepoName, err)
		}
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
	//
	// When running with a non-default AGENC_DIRPATH (e.g. test environments),
	// we must export it into the tmux pane's shell since the pane doesn't
	// inherit the server process's environment.
	var envPrefix string
	if config.GetNamespaceSuffix(s.agencDirpath) != "" {
		envPrefix = fmt.Sprintf("export AGENC_DIRPATH='%s'; ", s.agencDirpath)
	}
	resumeCmd := fmt.Sprintf("%s'%s' mission resume --run-wrapper %s", envPrefix, agencBinpath, missionRecord.ID)
	if req.Prompt != "" {
		resumeCmd += fmt.Sprintf(" --prompt '%s'", strings.ReplaceAll(req.Prompt, "'", "'\\''"))
	}

	// Create the wrapper window in the pool
	_, paneID, err := s.createPoolWindow(missionRecord.ID, resumeCmd)
	if err != nil {
		return fmt.Errorf("failed to create pool window: %w", err)
	}

	// Store the pane ID
	if err := s.db.SetTmuxPane(missionRecord.ID, paneID); err != nil {
		s.logger.Printf("Warning: failed to store pane ID for mission %s: %v", missionRecord.ShortID, err)
	}

	// Link the pool window into the caller's session (if provided),
	// then focus and reconcile the title. Uses pane ID (immutable) for
	// targeting so that this works even after title reconciliation.
	tmuxSession := req.TmuxSession
	if tmuxSession != "" {
		if err := linkPoolWindowByPane(paneID, tmuxSession); err != nil {
			s.destroyPoolWindow(paneID)
			return fmt.Errorf("failed to link pool window: %w", err)
		}
		if !req.NoFocus {
			focusPaneInSession(paneID, tmuxSession)
		}
	}

	s.reconcileTmuxWindowTitle(missionRecord.ID)

	return nil
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
	if missionRecord.TmuxPane != nil {
		s.destroyPoolWindow(*missionRecord.TmuxPane)
	}

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
		_ = os.Remove(pidFilepath)
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
			_ = os.Remove(pidFilepath)
			return nil
		}
		time.Sleep(stopTick)
	}

	// Force kill if still running
	_ = process.Signal(syscall.SIGKILL)
	_ = os.Remove(pidFilepath)
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
	if missionRecord.TmuxPane != nil {
		s.destroyPoolWindow(*missionRecord.TmuxPane)
	}

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
	NoFocus     bool   `json:"no_focus"`
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

	// Auto-unarchive if needed
	if missionRecord.Status == "archived" {
		if err := s.db.UnarchiveMission(resolvedID); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to unarchive mission: %s", err.Error())
		}
		s.logger.Printf("Auto-unarchived mission %s during attach", database.ShortID(resolvedID))
	}

	// Lazy start: ensure wrapper is running in the pool
	if err := s.ensureWrapperInPool(missionRecord); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to start wrapper: %s", err.Error())
	}

	// Refresh the mission record to get the current pane ID (ensureWrapperInPool
	// may have created a new pool window and stored a new pane ID).
	missionRecord, err = s.db.GetMission(resolvedID)
	if err != nil || missionRecord == nil {
		return newHTTPError(http.StatusInternalServerError, "failed to refresh mission after ensuring wrapper")
	}
	if missionRecord.TmuxPane == nil || *missionRecord.TmuxPane == "" {
		return newHTTPError(http.StatusInternalServerError, "mission has no tmux pane after ensuring wrapper")
	}
	paneID := *missionRecord.TmuxPane

	// Link the pool window into the caller's session if not already there.
	// Uses the pane ID (immutable) rather than the window name (which may have
	// been changed by title reconciliation).
	if !isPaneInSession(paneID, req.TmuxSession) {
		if err := linkPoolWindowByPane(paneID, req.TmuxSession); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to link window: %s", err.Error())
		}
	}
	if !req.NoFocus {
		focusPaneInSession(paneID, req.TmuxSession)
	}
	s.reconcileTmuxWindowTitle(resolvedID)

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

	missionRecord, err := s.db.GetMission(resolvedID)
	if err != nil {
		return newHTTPError(http.StatusInternalServerError, err.Error())
	}
	if missionRecord == nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}
	if missionRecord.TmuxPane == nil || *missionRecord.TmuxPane == "" {
		return newHTTPError(http.StatusBadRequest, "mission has no tmux pane")
	}

	if err := unlinkPoolWindowByPane(*missionRecord.TmuxPane, req.TmuxSession); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to unlink window: %s", err.Error())
	}

	// Clean up any side shell panes the user created (via tmux split-window)
	// so they don't linger in the pool after detach.
	killExtraPanesInWindow(*missionRecord.TmuxPane, s.getPoolSessionName(), s.logger)

	s.logger.Printf("Detached mission %s from session %s", database.ShortID(resolvedID), req.TmuxSession)
	writeJSON(w, http.StatusOK, map[string]string{"status": "detached"})
	return nil
}

// SendKeysRequest is the JSON body for POST /missions/{id}/send-keys.
type SendKeysRequest struct {
	Keys []string `json:"keys"`
}

// handleSendKeys handles POST /missions/{id}/send-keys.
// Sends keystrokes to a running mission's tmux pane via tmux send-keys.
func (s *Server) handleSendKeys(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	var req SendKeysRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}
	if len(req.Keys) == 0 {
		return newHTTPError(http.StatusBadRequest, "keys is required and must not be empty")
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

	shortID := database.ShortID(resolvedID)

	if missionRecord.Status == "archived" {
		return newHTTPErrorf(http.StatusBadRequest,
			"cannot send keys to archived mission %s — unarchive it with: agenc mission unarchive %s",
			shortID, shortID)
	}
	if missionRecord.TmuxPane == nil || *missionRecord.TmuxPane == "" {
		return newHTTPErrorf(http.StatusBadRequest,
			"mission %s is not running — start it with: agenc mission attach %s",
			shortID, shortID)
	}

	paneID := *missionRecord.TmuxPane
	if !poolWindowExistsByPane(paneID, s.getPoolSessionName()) {
		return newHTTPErrorf(http.StatusInternalServerError,
			"mission %s has a stale pane reference — try: agenc mission reload %s",
			shortID, shortID)
	}

	if err := sendKeysToPane(paneID, req.Keys); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "%s", err.Error())
	}

	s.logger.Printf("Sent %d key(s) to mission %s (pane %s)", len(req.Keys), shortID, paneID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
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
		paneID := ""
		if missionRecord.TmuxPane != nil {
			paneID = *missionRecord.TmuxPane
		}
		if poolWindowExistsByPane(paneID, s.getPoolSessionName()) {
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
	poolWindowTarget, paneID, err := s.createPoolWindow(missionRecord.ID, resumeCmd)
	if err != nil {
		return fmt.Errorf("failed to create pool window: %w", err)
	}

	// Store the pane ID (don't reconcile title here — the caller links the
	// window by name after this returns, and reconciliation renames it).
	if err := s.db.SetTmuxPane(missionRecord.ID, paneID); err != nil {
		s.logger.Printf("Warning: failed to store pane ID for mission %s: %v", database.ShortID(missionRecord.ID), err)
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
	if missionRecord.TmuxPane != nil {
		s.destroyPoolWindow(*missionRecord.TmuxPane)
	}

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
	ConfigCommit *string `json:"config_commit,omitempty"`
	SessionName  *string `json:"session_name,omitempty"`
	Prompt       *string `json:"prompt,omitempty"`
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
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
	return nil
}

// HeartbeatRequest is the optional JSON body for the heartbeat endpoint.
type HeartbeatRequest struct {
	PaneID           string `json:"pane_id"`
	LastUserPromptAt string `json:"last_user_prompt_at"`
}

// handleHeartbeat handles POST /missions/{id}/heartbeat.
// Updates the mission's last_heartbeat timestamp and, if a pane_id is provided,
// stores it as the mission's current tmux pane.
func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")

	resolvedID, err := s.db.ResolveMissionID(id)
	if err != nil {
		return newHTTPError(http.StatusNotFound, "mission not found: "+id)
	}

	if err := s.db.UpdateHeartbeat(resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to update heartbeat: %s", err.Error())
	}

	// Decode optional pane_id from the request body. Old wrappers and headless
	// missions may send an empty body, so decode errors are ignored.
	var req HeartbeatRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}
	if req.PaneID != "" {
		if err := s.db.SetTmuxPane(resolvedID, req.PaneID); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to set tmux pane: %s", err.Error())
		}
	}

	if req.LastUserPromptAt != "" {
		if err := s.db.SetLastUserPromptAt(resolvedID, req.LastUserPromptAt); err != nil {
			return newHTTPErrorf(http.StatusInternalServerError, "failed to set last_user_prompt_at: %s", err.Error())
		}
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

	if err := s.db.UpdateLastUserPromptAt(resolvedID); err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to update last_user_prompt_at: %s", err.Error())
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
		_ = restoreCmd.Run() // best-effort cleanup; nothing to do if it fails
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
