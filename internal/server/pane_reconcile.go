package server

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

// reconcilePaneIDs clears all stored tmux pane IDs and repopulates them by
// matching running wrapper PIDs against the actual panes in the agenc-pool
// tmux session. This runs synchronously on server startup before the HTTP
// server begins accepting requests.
//
// After this function returns, every active mission's tmux_pane is either:
//   - set to the correct pane ID (wrapper is alive and has a pool pane), or
//   - NULL (wrapper is not running, or pool session doesn't exist)
func (s *Server) reconcilePaneIDs() {
	// Step 1: Clear all stored pane IDs
	if err := s.db.ClearAllTmuxPanes(); err != nil {
		s.logger.Printf("Warning: failed to clear tmux panes on startup: %v", err)
		return
	}

	// Step 2: Query agenc-pool for (paneID, panePID) pairs
	poolPanes, err := listPoolPanesWithPIDs(s.getPoolSessionName())
	if err != nil {
		s.logger.Printf("Pane reconciliation: pool session not available (%v), skipping", err)
		return
	}

	if len(poolPanes) == 0 {
		s.logger.Printf("Pane reconciliation: no panes in pool, nothing to reconcile")
		return
	}

	// Step 3: Build PID -> paneID lookup from tmux
	pidToPaneID := make(map[int]string, len(poolPanes))
	for _, pp := range poolPanes {
		pidToPaneID[pp.pid] = pp.paneID
	}

	// Step 4: For each active mission, check if its wrapper PID matches a pool pane
	missions, err := s.db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		s.logger.Printf("Warning: failed to list missions for pane reconciliation: %v", err)
		return
	}

	reconciled := 0
	for _, m := range missions {
		pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, m.ID)
		pid, err := ReadPID(pidFilepath)
		if err != nil || pid == 0 || !IsProcessRunning(pid) {
			continue
		}

		paneID, found := pidToPaneID[pid]
		if !found {
			continue
		}

		if err := s.db.SetTmuxPane(m.ID, paneID); err != nil {
			s.logger.Printf("Warning: failed to set pane for mission %s: %v", database.ShortID(m.ID), err)
			continue
		}
		reconciled++
	}

	s.logger.Printf("Pane reconciliation: matched %d missions to pool panes", reconciled)
}

// poolPaneInfo holds a pane ID and the PID of the process running in that pane.
type poolPaneInfo struct {
	paneID string
	pid    int
}

// listPoolPanesWithPIDs queries tmux for all panes in the agenc-pool session
// and returns their pane IDs (without "%" prefix) and process PIDs.
func listPoolPanesWithPIDs(poolSessionName string) ([]poolPaneInfo, error) {
	cmd := exec.Command("tmux", "list-panes", "-s", "-t", "="+poolSessionName, "-F", "#{pane_id} #{pane_pid}")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tmux list-panes failed: %w", err)
	}

	var result []poolPaneInfo
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		paneID := strings.TrimPrefix(parts[0], "%")
		pid := 0
		_, _ = fmt.Sscanf(parts[1], "%d", &pid) // parse failure leaves pid=0, filtered by subsequent check
		if pid > 0 {
			result = append(result, poolPaneInfo{paneID: paneID, pid: pid})
		}
	}
	return result, nil
}
