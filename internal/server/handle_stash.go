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
		return newHTTPErrorf(http.StatusInternalServerError, "failed to write stash file: %s", err.Error())
	}

	// Stop all running missions
	for _, resp := range responses {
		if err := s.stopWrapper(resp.ID); err != nil {
			s.logger.Printf("Warning: failed to stop wrapper for mission %s: %v", resp.ShortID, err)
		}
		s.destroyPoolWindow(resp.ID)
	}

	s.logger.Printf("Stashed %d missions as %s", len(stashMissions), stashID)
	writeJSON(w, http.StatusOK, StashPushResponse{
		StashID:         stashID,
		MissionsStashed: len(stashMissions),
	})
	return nil
}

// handlePopStash handles POST /stash/pop.
func (s *Server) handlePopStash(w http.ResponseWriter, r *http.Request) error {
	var req StashPopRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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

	// Restore each mission
	restored := 0
	for _, sm := range stashFile.Missions {
		// Verify mission still exists and is active
		mission, err := s.db.GetMission(sm.MissionID)
		if err != nil || mission == nil {
			s.logger.Printf("Warning: stashed mission %s no longer exists, skipping", database.ShortID(sm.MissionID))
			continue
		}
		if mission.Status == "archived" {
			s.logger.Printf("Warning: stashed mission %s is archived, skipping", database.ShortID(sm.MissionID))
			continue
		}

		// Spawn wrapper in pool
		if err := s.ensureWrapperInPool(mission); err != nil {
			s.logger.Printf("Warning: failed to start wrapper for mission %s: %v", database.ShortID(sm.MissionID), err)
			continue
		}

		// Re-link into saved tmux sessions using the pane ID
		paneID := ""
		if mission.TmuxPane != nil {
			paneID = *mission.TmuxPane
		}

		for _, sessionName := range sm.LinkedSessions {
			// Create tmux session if it doesn't exist
			if !tmuxSessionExists(sessionName) {
				createCmd := exec.Command("tmux", "new-session", "-d", "-s", sessionName)
				if output, err := createCmd.CombinedOutput(); err != nil {
					s.logger.Printf("Warning: failed to create tmux session %s: %v (output: %s)", sessionName, err, string(output))
					continue
				}
				s.logger.Printf("Created tmux session %s for stash restore", sessionName)
			}

			if paneID != "" {
				if err := linkPoolWindowByPane(paneID, sessionName); err != nil {
					s.logger.Printf("Warning: failed to link mission %s to session %s: %v", database.ShortID(sm.MissionID), sessionName, err)
				}
			}
		}

		s.reconcileTmuxWindowTitle(sm.MissionID)
		restored++
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
