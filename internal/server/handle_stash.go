package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

// ============================================================================
// Stash file data structures
// ============================================================================

// StashFile is the JSON structure persisted to disk.
type StashFile struct {
	CreatedAt time.Time      `json:"created_at"`
	Missions  []StashMission `json:"missions"`
}

// StashMission records a single mission's state at time of stash.
type StashMission struct {
	MissionID      string   `json:"mission_id"`
	LinkedSessions []string `json:"linked_sessions"`
}

// ============================================================================
// API request/response types
// ============================================================================

// StashPushRequest is the JSON body for POST /stash/push.
type StashPushRequest struct {
	Force bool `json:"force"`
}

// StashPushResponse is the JSON response for a successful push.
type StashPushResponse struct {
	StashID         string `json:"stash_id"`
	MissionsStashed int    `json:"missions_stashed"`
}

// NonIdleMissionInfo describes a non-idle mission for the 409 response.
type NonIdleMissionInfo struct {
	MissionID   string `json:"mission_id"`
	ShortID     string `json:"short_id"`
	ClaudeState string `json:"claude_state"`
	SessionName string `json:"session_name"`
}

// StashPushConflictResponse is the 409 response when non-idle missions exist.
type StashPushConflictResponse struct {
	NonIdleMissions []NonIdleMissionInfo `json:"non_idle_missions"`
}

// StashPopRequest is the JSON body for POST /stash/pop.
type StashPopRequest struct {
	StashID string `json:"stash_id"`
}

// StashPopResponse is the JSON response for a successful pop.
type StashPopResponse struct {
	MissionsRestored int `json:"missions_restored"`
}

// StashListEntry is a single entry in the GET /stash response.
type StashListEntry struct {
	StashID      string    `json:"stash_id"`
	CreatedAt    time.Time `json:"created_at"`
	MissionCount int       `json:"mission_count"`
}

// ============================================================================
// Handlers
// ============================================================================

// handleListStashes handles GET /stash.
func (s *Server) handleListStashes(w http.ResponseWriter, r *http.Request) error {
	stashDirpath := config.GetStashDirpath(s.agencDirpath)

	entries, err := os.ReadDir(stashDirpath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusOK, []StashListEntry{})
			return nil
		}
		return newHTTPErrorf(http.StatusInternalServerError, "failed to read stash directory: %s", err.Error())
	}

	var result []StashListEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		stashFilepath := filepath.Join(stashDirpath, entry.Name())
		stashFile, err := readStashFile(stashFilepath)
		if err != nil {
			s.logger.Printf("Warning: skipping unreadable stash file %s: %v", entry.Name(), err)
			continue
		}

		stashID := strings.TrimSuffix(entry.Name(), ".json")
		result = append(result, StashListEntry{
			StashID:      stashID,
			CreatedAt:    stashFile.CreatedAt,
			MissionCount: len(stashFile.Missions),
		})
	}

	// Sort by creation time, most recent first
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	writeJSON(w, http.StatusOK, result)
	return nil
}

// stashConcurrency controls how many missions are stopped/started in parallel.
const stashConcurrency = 5

// handlePushStash handles POST /stash/push.
func (s *Server) handlePushStash(w http.ResponseWriter, r *http.Request) error {
	var req StashPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	responses, err := s.findRunningMissionResponses()
	if err != nil {
		return stacktrace.Propagate(err, "failed to find running missions for stash")
	}

	if len(responses) == 0 {
		writeJSON(w, http.StatusOK, StashPushResponse{MissionsStashed: 0})
		return nil
	}

	// Check for non-idle missions (unless force is set)
	if !req.Force {
		nonIdle := s.findNonIdleMissions(responses)
		if len(nonIdle) > 0 {
			writeJSON(w, http.StatusConflict, StashPushConflictResponse{NonIdleMissions: nonIdle})
			return nil
		}
	}

	// Block mutating mission requests for the duration of the stash
	s.stashInProgress.Store(true)

	stashID, stashMissions, err := s.buildAndWriteStash(responses)
	if err != nil {
		s.stashInProgress.Store(false)
		return stacktrace.Propagate(err, "failed to build and write stash")
	}

	// Immediately unlink all windows from user sessions for fast visual feedback.
	s.unlinkStashedWindows(stashMissions, responses)

	// Respond immediately — the user sees windows vanish and gets the CLI back.
	s.logger.Printf("Stashed %d missions as %s", len(stashMissions), stashID)
	writeJSON(w, http.StatusOK, StashPushResponse{
		StashID:         stashID,
		MissionsStashed: len(stashMissions),
	})

	// Stop wrappers and destroy pool windows concurrently in the background.
	s.stopStashedMissionsAsync(stashID, responses)

	return nil
}

// findRunningMissionResponses lists all active missions, filters to those with a
// running wrapper process, and enriches them with ClaudeState concurrently.
func (s *Server) findRunningMissionResponses() ([]MissionResponse, error) {
	missions, err := s.db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return nil, newHTTPErrorf(http.StatusInternalServerError, "failed to list missions: %s", err.Error())
	}

	var runningMissions []*database.Mission
	for _, m := range missions {
		pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, m.ID)
		pid, pidErr := ReadPID(pidFilepath)
		if pidErr == nil && pid != 0 && IsProcessRunning(pid) {
			runningMissions = append(runningMissions, m)
		}
	}

	if len(runningMissions) == 0 {
		return nil, nil
	}

	responses := toMissionResponses(runningMissions)
	var wg sync.WaitGroup
	for i := range responses {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			s.enrichMissionResponse(&responses[idx])
		}(i)
	}
	wg.Wait()

	return responses, nil
}

// findNonIdleMissions returns info about missions that are not idle.
func (s *Server) findNonIdleMissions(responses []MissionResponse) []NonIdleMissionInfo {
	var nonIdle []NonIdleMissionInfo
	for _, resp := range responses {
		if resp.ClaudeState != nil && *resp.ClaudeState != "idle" {
			sessionName := ""
			if activeSession, err := s.db.GetActiveSession(resp.ID); err == nil {
				sessionName = resolveSessionTitle(activeSession)
			}
			nonIdle = append(nonIdle, NonIdleMissionInfo{
				MissionID:   resp.ID,
				ShortID:     resp.ShortID,
				ClaudeState: *resp.ClaudeState,
				SessionName: sessionName,
			})
		}
	}
	return nonIdle
}

// buildAndWriteStash builds the stash data from enriched mission responses,
// writes the stash file to disk, and returns the stash ID and mission entries.
func (s *Server) buildAndWriteStash(responses []MissionResponse) (string, []StashMission, error) {
	paneSessions := getLinkedPaneSessions()

	now := time.Now().UTC()
	stashID := now.Format("2006-01-02T15-04-05")

	var stashMissions []StashMission
	for _, resp := range responses {
		var linkedSessions []string
		if resp.TmuxPane != nil && *resp.TmuxPane != "" {
			linkedSessions = paneSessions[*resp.TmuxPane]
		}
		stashMissions = append(stashMissions, StashMission{
			MissionID:      resp.ID,
			LinkedSessions: linkedSessions,
		})
	}

	stashFile := StashFile{
		CreatedAt: now,
		Missions:  stashMissions,
	}

	if err := writeStashFile(s.agencDirpath, stashID, &stashFile); err != nil {
		return "", nil, newHTTPErrorf(http.StatusInternalServerError, "failed to write stash file: %s", err.Error())
	}

	return stashID, stashMissions, nil
}

// unlinkStashedWindows unlinks all stashed missions' tmux windows from user sessions
// for fast visual feedback, making them disappear immediately.
func (s *Server) unlinkStashedWindows(stashMissions []StashMission, responses []MissionResponse) {
	for _, sm := range stashMissions {
		paneID := ""
		for _, resp := range responses {
			if resp.ID == sm.MissionID && resp.TmuxPane != nil {
				paneID = *resp.TmuxPane
				break
			}
		}
		if paneID == "" {
			continue
		}
		for _, sessionName := range sm.LinkedSessions {
			if err := unlinkPoolWindowByPane(paneID, sessionName); err != nil {
				s.logger.Printf("Warning: failed to unlink mission %s from session %s: %v",
					database.ShortID(sm.MissionID), sessionName, err)
			}
		}
	}
}

// stopStashedMissionsAsync stops wrappers and destroys pool windows concurrently
// in the background. It clears the stashInProgress flag when done.
func (s *Server) stopStashedMissionsAsync(stashID string, responses []MissionResponse) {
	go func() {
		defer s.stashInProgress.Store(false)

		sem := make(chan struct{}, stashConcurrency)
		var stopWg sync.WaitGroup
		for _, resp := range responses {
			stopWg.Add(1)
			sem <- struct{}{} // acquire slot
			paneID := ""
			if resp.TmuxPane != nil {
				paneID = *resp.TmuxPane
			}
			go func(missionID, shortID, paneID string) {
				defer stopWg.Done()
				defer func() { <-sem }() // release slot
				if err := s.stopWrapper(missionID); err != nil {
					s.logger.Printf("Warning: failed to stop wrapper for mission %s: %v", shortID, err)
				}
				s.destroyPoolWindow(paneID)
			}(resp.ID, resp.ShortID, paneID)
		}
		stopWg.Wait()
		s.logger.Printf("Stash %s: all %d missions stopped", stashID, len(responses))
	}()
}

// handlePopStash handles POST /stash/pop.
func (s *Server) handlePopStash(w http.ResponseWriter, r *http.Request) error {
	var req StashPopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	stashID, stashFilepath, stashFile, err := s.resolveStash(req.StashID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve stash")
	}

	// Block mutating mission requests during restore
	s.stashInProgress.Store(true)
	defer s.stashInProgress.Store(false)

	s.ensureTmuxSessionsForRestore(stashFile.Missions)

	restored := s.restoreMissionsConcurrently(stashFile.Missions)

	// Delete stash file on success
	if err := os.Remove(stashFilepath); err != nil {
		s.logger.Printf("Warning: failed to delete stash file %s: %v", stashFilepath, err)
	}

	s.logger.Printf("Restored %d missions from stash %s", restored, stashID)
	writeJSON(w, http.StatusOK, StashPopResponse{MissionsRestored: restored})
	return nil
}

// resolveStash resolves the stash ID (using the most recent if empty), reads the
// stash file, and returns the resolved ID, file path, and parsed stash data.
func (s *Server) resolveStash(stashID string) (string, string, *StashFile, error) {
	stashDirpath := config.GetStashDirpath(s.agencDirpath)

	if stashID == "" {
		mostRecent, err := findMostRecentStash(stashDirpath)
		if err != nil {
			return "", "", nil, newHTTPError(http.StatusNotFound, "no stash files found")
		}
		stashID = mostRecent
	}

	stashFilepath := filepath.Join(stashDirpath, stashID+".json")
	stashFile, err := readStashFile(stashFilepath)
	if err != nil {
		return "", "", nil, newHTTPErrorf(http.StatusNotFound, "stash not found: %s", stashID)
	}

	return stashID, stashFilepath, stashFile, nil
}

// ensureTmuxSessionsForRestore pre-creates any tmux sessions referenced by stashed
// missions that don't currently exist. This must run serially because tmux
// new-session is not safe to call concurrently for the same session name.
func (s *Server) ensureTmuxSessionsForRestore(missions []StashMission) {
	neededSessions := make(map[string]bool)
	for _, sm := range missions {
		for _, sessionName := range sm.LinkedSessions {
			neededSessions[sessionName] = true
		}
	}
	for sessionName := range neededSessions {
		if !tmuxSessionExists(sessionName) {
			createCmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName)
			if output, err := createCmd.CombinedOutput(); err != nil {
				s.logger.Printf("Warning: failed to create tmux session %s: %v (output: %s)", sessionName, err, string(output))
			} else {
				s.logger.Printf("Created tmux session %s for stash restore", sessionName)
			}
		}
	}
}

// restoreMissionsConcurrently spawns wrappers and re-links tmux windows for each
// stashed mission, using a bounded semaphore for concurrency. Returns the count
// of successfully restored missions.
func (s *Server) restoreMissionsConcurrently(missions []StashMission) int {
	type restoreResult struct {
		ok bool
	}
	results := make(chan restoreResult, len(missions))
	sem := make(chan struct{}, stashConcurrency)
	var wg sync.WaitGroup

	for _, sm := range missions {
		wg.Add(1)
		go func(sm StashMission) {
			defer wg.Done()
			sem <- struct{}{}        // acquire slot
			defer func() { <-sem }() // release slot
			results <- restoreResult{ok: s.restoreSingleMission(sm)}
		}(sm)
	}

	wg.Wait()
	close(results)

	restored := 0
	for res := range results {
		if res.ok {
			restored++
		}
	}
	return restored
}

// restoreSingleMission restores a single stashed mission by verifying it still
// exists, spawning its wrapper, and re-linking its tmux windows.
func (s *Server) restoreSingleMission(sm StashMission) bool {
	shortID := database.ShortID(sm.MissionID)

	// Verify mission still exists and is active
	mission, err := s.db.GetMission(sm.MissionID)
	if err != nil || mission == nil {
		s.logger.Printf("Warning: stashed mission %s no longer exists, skipping", shortID)
		return false
	}
	if mission.Status == "archived" {
		s.logger.Printf("Warning: stashed mission %s is archived, skipping", shortID)
		return false
	}

	// Spawn wrapper in pool (this assigns a new pane ID in the DB)
	if err := s.ensureWrapperInPool(mission); err != nil {
		s.logger.Printf("Warning: failed to start wrapper for mission %s: %v", shortID, err)
		return false
	}

	// Re-read mission to get the fresh pane ID assigned by ensureWrapperInPool
	mission, err = s.db.GetMission(sm.MissionID)
	if err != nil || mission == nil {
		s.logger.Printf("Warning: failed to re-read mission %s after starting wrapper", shortID)
		return false
	}

	paneID := ""
	if mission.TmuxPane != nil {
		paneID = *mission.TmuxPane
	}

	for _, sessionName := range sm.LinkedSessions {
		if paneID != "" {
			if err := linkPoolWindowByPane(paneID, sessionName); err != nil {
				s.logger.Printf("Warning: failed to link mission %s to session %s: %v", shortID, sessionName, err)
			}
		}
	}

	s.reconcileTmuxWindowTitle(sm.MissionID)
	return true
}

// ============================================================================
// Helpers
// ============================================================================

// readStashFile reads and parses a stash JSON file.
func readStashFile(stashFilepath string) (*StashFile, error) {
	data, err := os.ReadFile(stashFilepath)
	if err != nil {
		return nil, err
	}
	var stash StashFile
	if err := json.Unmarshal(data, &stash); err != nil {
		return nil, err
	}
	return &stash, nil
}

// writeStashFile creates the stash directory if needed and atomically writes the stash file.
// It writes to a temporary file first, then renames to the final path to prevent
// partial writes from leaving a corrupted stash file on disk.
func writeStashFile(agencDirpath string, stashID string, stash *StashFile) error {
	stashDirpath := config.GetStashDirpath(agencDirpath)
	if err := os.MkdirAll(stashDirpath, 0755); err != nil {
		return fmt.Errorf("failed to create stash directory: %w", err)
	}

	data, err := json.MarshalIndent(stash, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal stash: %w", err)
	}

	stashFilepath := filepath.Join(stashDirpath, stashID+".json")

	// Atomic write: write to temp file in same directory, then rename
	tmpFile, err := os.CreateTemp(stashDirpath, ".stash-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpFilepath := tmpFile.Name()

	if _, err := tmpFile.Write(data); err != nil {
		tmpFile.Close()
		_ = os.Remove(tmpFilepath)
		return fmt.Errorf("failed to write stash data: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFilepath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Rename(tmpFilepath, stashFilepath); err != nil {
		_ = os.Remove(tmpFilepath)
		return fmt.Errorf("failed to rename stash file: %w", err)
	}
	return nil
}

// findMostRecentStash returns the stash ID of the most recently created stash file.
// It uses the CreatedAt field from the stash JSON (not filesystem mtime) for consistency
// with handleListStashes.
func findMostRecentStash(stashDirpath string) (string, error) {
	entries, err := os.ReadDir(stashDirpath)
	if err != nil {
		return "", err
	}

	var newest string
	var newestTime time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		stashFilepath := filepath.Join(stashDirpath, entry.Name())
		stash, err := readStashFile(stashFilepath)
		if err != nil {
			continue
		}
		if newest == "" || stash.CreatedAt.After(newestTime) {
			newest = strings.TrimSuffix(entry.Name(), ".json")
			newestTime = stash.CreatedAt
		}
	}

	if newest == "" {
		return "", fmt.Errorf("no stash files found")
	}
	return newest, nil
}
