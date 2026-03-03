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

	// List all active missions
	missions, err := s.db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return newHTTPErrorf(http.StatusInternalServerError, "failed to list missions: %s", err.Error())
	}

	// Filter to running missions only (wrapper PID alive)
	var runningMissions []*database.Mission
	for _, m := range missions {
		pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, m.ID)
		pid, pidErr := ReadPID(pidFilepath)
		if pidErr == nil && pid != 0 && IsProcessRunning(pid) {
			runningMissions = append(runningMissions, m)
		}
	}

	if len(runningMissions) == 0 {
		writeJSON(w, http.StatusOK, StashPushResponse{MissionsStashed: 0})
		return nil
	}

	// Enrich with ClaudeState concurrently
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

	// Check for non-idle missions
	if !req.Force {
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
		if len(nonIdle) > 0 {
			writeJSON(w, http.StatusConflict, StashPushConflictResponse{NonIdleMissions: nonIdle})
			return nil
		}
	}

	// Block mutating mission requests for the duration of the stash
	s.stashInProgress.Store(true)

	// Build per-pane session map
	paneSessions := getLinkedPaneSessions()

	// Build stash data
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

	// Write stash file before stopping (so partial failures still have a record)
	if err := writeStashFile(s.agencDirpath, stashID, &stashFile); err != nil {
		s.stashInProgress.Store(false)
		return newHTTPErrorf(http.StatusInternalServerError, "failed to write stash file: %s", err.Error())
	}

	// Immediately unlink all windows from user sessions for fast visual feedback.
	// This makes the missions disappear from the user's tmux session right away.
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

	// Respond immediately — the user sees windows vanish and gets the CLI back.
	s.logger.Printf("Stashed %d missions as %s", len(stashMissions), stashID)
	writeJSON(w, http.StatusOK, StashPushResponse{
		StashID:         stashID,
		MissionsStashed: len(stashMissions),
	})

	// Stop wrappers and destroy pool windows concurrently in the background.
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

	return nil
}

// handlePopStash handles POST /stash/pop.
func (s *Server) handlePopStash(w http.ResponseWriter, r *http.Request) error {
	var req StashPopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		return newHTTPError(http.StatusBadRequest, "invalid request body: "+err.Error())
	}

	stashDirpath := config.GetStashDirpath(s.agencDirpath)

	// Resolve stash ID (use most recent if not specified)
	stashID := req.StashID
	if stashID == "" {
		mostRecent, err := findMostRecentStash(stashDirpath)
		if err != nil {
			return newHTTPError(http.StatusNotFound, "no stash files found")
		}
		stashID = mostRecent
	}

	stashFilepath := filepath.Join(stashDirpath, stashID+".json")
	stashFile, err := readStashFile(stashFilepath)
	if err != nil {
		return newHTTPErrorf(http.StatusNotFound, "stash not found: %s", stashID)
	}

	// Block mutating mission requests during restore
	s.stashInProgress.Store(true)
	defer s.stashInProgress.Store(false)

	// Pre-create any tmux sessions that don't exist yet (must be serial since
	// tmux new-session is not safe to call concurrently for the same session name)
	neededSessions := make(map[string]bool)
	for _, sm := range stashFile.Missions {
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

	// Restore missions concurrently with a semaphore
	type restoreResult struct {
		missionID string
		ok        bool
	}
	results := make(chan restoreResult, len(stashFile.Missions))
	sem := make(chan struct{}, stashConcurrency)
	var wg sync.WaitGroup

	for _, sm := range stashFile.Missions {
		wg.Add(1)
		go func(sm StashMission) {
			defer wg.Done()
			sem <- struct{}{}        // acquire slot
			defer func() { <-sem }() // release slot

			shortID := database.ShortID(sm.MissionID)

			// Verify mission still exists and is active
			mission, err := s.db.GetMission(sm.MissionID)
			if err != nil || mission == nil {
				s.logger.Printf("Warning: stashed mission %s no longer exists, skipping", shortID)
				results <- restoreResult{sm.MissionID, false}
				return
			}
			if mission.Status == "archived" {
				s.logger.Printf("Warning: stashed mission %s is archived, skipping", shortID)
				results <- restoreResult{sm.MissionID, false}
				return
			}

			// Spawn wrapper in pool (this assigns a new pane ID in the DB)
			if err := s.ensureWrapperInPool(mission); err != nil {
				s.logger.Printf("Warning: failed to start wrapper for mission %s: %v", shortID, err)
				results <- restoreResult{sm.MissionID, false}
				return
			}

			// Re-read mission to get the fresh pane ID assigned by ensureWrapperInPool
			mission, err = s.db.GetMission(sm.MissionID)
			if err != nil || mission == nil {
				s.logger.Printf("Warning: failed to re-read mission %s after starting wrapper", shortID)
				results <- restoreResult{sm.MissionID, false}
				return
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
			results <- restoreResult{sm.MissionID, true}
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

	// Delete stash file on success
	if err := os.Remove(stashFilepath); err != nil {
		s.logger.Printf("Warning: failed to delete stash file %s: %v", stashFilepath, err)
	}

	s.logger.Printf("Restored %d missions from stash %s", restored, stashID)
	writeJSON(w, http.StatusOK, StashPopResponse{MissionsRestored: restored})
	return nil
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
		os.Remove(tmpFilepath)
		return fmt.Errorf("failed to write stash data: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpFilepath)
		return fmt.Errorf("failed to close temp file: %w", err)
	}
	if err := os.Rename(tmpFilepath, stashFilepath); err != nil {
		os.Remove(tmpFilepath)
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
