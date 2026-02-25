package server

import (
	"context"
	"time"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

const (
	// idleTimeoutCheckInterval is how often the server checks for idle missions.
	idleTimeoutCheckInterval = 2 * time.Minute

	// defaultIdleTimeout is how long a mission can be idle before its wrapper
	// is stopped. Idle means no UserPromptSubmit event (tracked via last_active).
	defaultIdleTimeout = 30 * time.Minute
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

	linkedMissionIDs := getLinkedMissionIDs()

	now := time.Now()
	for _, m := range missions {
		if !s.isWrapperRunning(m.ID) {
			continue
		}

		idleDuration := s.missionIdleDuration(m, now)
		if idleDuration < defaultIdleTimeout {
			continue
		}

		// Skip missions whose pool window is linked into a user session
		if linkedMissionIDs[database.ShortID(m.ID)] {
			s.logger.Printf("Idle timeout: skipping mission %s (linked into user session, idle for %s)", database.ShortID(m.ID), idleDuration.Round(time.Second))
			continue
		}

		s.logger.Printf("Idle timeout: stopping mission %s (idle for %s)", database.ShortID(m.ID), idleDuration.Round(time.Second))
		if err := s.stopWrapper(m.ID); err != nil {
			s.logger.Printf("Idle timeout: failed to stop mission %s: %v", database.ShortID(m.ID), err)
			continue
		}

		// Also destroy the pool window since the wrapper exited
		s.destroyPoolWindow(m.ID)
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

// missionIdleDuration returns how long a mission has been idle. It uses
// last_active (user prompt time) if available, otherwise falls back to
// last_heartbeat (wrapper liveness), and finally to created_at.
func (s *Server) missionIdleDuration(m *database.Mission, now time.Time) time.Duration {
	if m.LastActive != nil {
		return now.Sub(*m.LastActive)
	}
	if m.LastHeartbeat != nil {
		return now.Sub(*m.LastHeartbeat)
	}
	return now.Sub(m.CreatedAt)
}
