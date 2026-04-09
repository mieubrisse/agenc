package server

import (
	"context"
	"os"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/session"
)

const (
	// idleTimeoutCheckInterval is how often the server checks for idle missions.
	idleTimeoutCheckInterval = 2 * time.Minute

	// defaultIdleTimeout is how long a mission can be idle before its wrapper
	// is stopped. Idle means the JSONL conversation log has not been modified.
	defaultIdleTimeout = 30 * time.Minute

	// staleHeartbeatThreshold is how long after the last heartbeat before a
	// mission's tmux_pane is considered stale and cleared. Set to 3x the
	// wrapper heartbeat interval (10s) to tolerate occasional delays.
	staleHeartbeatThreshold = 30 * time.Second
)

// runIdleTimeoutLoop periodically scans running missions and stops wrappers
// that have been idle longer than the timeout threshold. The wrapper is
// automatically re-spawned on the next attach (lazy start).
func (s *Server) runIdleTimeoutLoop(ctx context.Context) {
	// Initial delay to avoid racing with startup
	select {
	case <-ctx.Done():
		return
	case <-time.After(idleTimeoutCheckInterval):
		s.runIdleTimeoutCycle()
	}

	ticker := time.NewTicker(idleTimeoutCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runIdleTimeoutCycle()
		}
	}
}

// runIdleTimeoutCycle checks all running missions and stops any that have been
// idle beyond the timeout threshold.
func (s *Server) runIdleTimeoutCycle() {
	missions, err := s.db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		s.logger.Printf("Idle timeout: failed to list missions: %v", err)
		return
	}

	now := time.Now()
	s.reapStalePaneIDs(missions, now)

	linkedPaneIDs := getLinkedPaneIDs(s.getPoolSessionName())

	for _, m := range missions {
		if !s.isWrapperRunning(m.ID) {
			continue
		}

		idleDuration := s.missionIdleDuration(m, now)
		if idleDuration < defaultIdleTimeout {
			continue
		}

		// Skip missions whose tmux pane is linked into a user session
		if m.TmuxPane != nil && linkedPaneIDs[*m.TmuxPane] {
			continue
		}

		s.logger.Printf("Idle timeout: stopping mission %s (idle for %s)", database.ShortID(m.ID), idleDuration.Round(time.Second))
		if err := s.stopWrapper(m.ID); err != nil {
			s.logger.Printf("Idle timeout: failed to stop mission %s: %v", database.ShortID(m.ID), err)
			continue
		}

		// Also destroy the pool window since the wrapper exited
		if m.TmuxPane != nil {
			s.destroyPoolWindow(*m.TmuxPane)
		}
	}
}

// reapStalePaneIDs clears tmux_pane for missions whose wrapper has stopped
// heartbeating. This catches wrapper crashes and tmux restarts that happen
// during normal operation (not just at server startup).
func (s *Server) reapStalePaneIDs(missions []*database.Mission, now time.Time) {
	for _, m := range missions {
		if m.TmuxPane == nil {
			continue
		}

		isStale := m.LastHeartbeat == nil || now.Sub(*m.LastHeartbeat) > staleHeartbeatThreshold
		if !isStale {
			continue
		}

		if err := s.db.ClearTmuxPane(m.ID); err != nil {
			s.logger.Printf("Warning: failed to clear stale pane for mission %s: %v", database.ShortID(m.ID), err)
			continue
		}
		s.logger.Printf("Cleared stale tmux pane for mission %s (last heartbeat: %v)", database.ShortID(m.ID), m.LastHeartbeat)
	}
}

// isWrapperRunning checks if a mission's wrapper process is currently running.
func (s *Server) isWrapperRunning(missionID string) bool {
	pidFilepath := config.GetMissionPIDFilepath(s.agencDirpath, missionID)
	pid, err := ReadPID(pidFilepath)
	if err != nil {
		return false
	}
	return IsProcessRunning(pid)
}

// missionIdleDuration returns how long a mission has been idle by checking the
// modification time of the active JSONL conversation log. Claude Code writes to
// this file whenever it does anything (streaming, tool calls, thinking), so a
// recently modified file means Claude is actively working.
//
// Falls back to created_at if the JSONL file cannot be located (mission has no
// session yet, or the project directory doesn't exist).
func (s *Server) missionIdleDuration(m *database.Mission, now time.Time) time.Duration {
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(s.agencDirpath, m.ID)
	jsonlFilepath := session.FindActiveJSONLPath(claudeConfigDirpath, m.ID)
	if jsonlFilepath != "" {
		if info, err := os.Stat(jsonlFilepath); err == nil {
			return now.Sub(info.ModTime())
		}
	}
	return now.Sub(m.CreatedAt)
}
