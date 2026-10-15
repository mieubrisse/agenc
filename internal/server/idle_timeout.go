package server

import (
	"context"
	"errors"
	"fmt"
	"net"
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
	// is stopped. Idle means Claude's turn has ended and the JSONL conversation
	// log has not been modified since.
	defaultIdleTimeout = 30 * time.Minute

	// idleTimeoutHardCeiling bounds how long the wrapper-state veto below may
	// spare a mission. That state is hook-fed, and a hook that never fires or
	// never arrives wedges the wrapper at "busy" for the rest of its life.
	// Without a ceiling every such miss becomes a permanent leak of a live
	// Claude process and a tmux pool window. Sized to exceed any realistic
	// single tool call, so it reclaims wedged missions without interrupting
	// work that is slow but genuine.
	//
	// The dominant cause was turns dying on API errors, which fire StopFailure
	// rather than Stop — a hook AgenC did not register until KEV-71, measured
	// at 13.7% of missions. ESC-interrupted turns, long suspected, turned out
	// to be 0 of the 149 provably-wedged missions examined. The ceiling stays
	// for the residue: interrupts, undelivered hooks, and version skew. Moving
	// it needs a fresh wedge measurement over missions created after the
	// StopFailure fix shipped.
	idleTimeoutHardCeiling = 6 * time.Hour

	// reapWrapperQueryTimeout is the budget for asking a wrapper whether Claude
	// is still working. Deliberately longer than wrapperQueryTimeout, which is
	// tuned for rendering `agenc mission ls` interactively — here a timeout
	// costs a mission its work, so it is worth waiting longer to avoid one.
	reapWrapperQueryTimeout = 2 * time.Second

	// staleHeartbeatThreshold is how long after the last heartbeat before a
	// mission's tmux_pane is considered stale and cleared. Set to 3x the
	// wrapper heartbeat interval (10s) to tolerate occasional delays.
	staleHeartbeatThreshold = 30 * time.Second
)

// Claude states a mission's wrapper reports over its status socket. Both of
// these mean Claude's turn is still in flight: needs-attention is set by the
// Notification hook and cleared by Stop, so it can never mean "finished".
const (
	claudeStateBusy           = "busy"
	claudeStateNeedsAttention = "needs_attention"
)

// idleTimeoutCheckIntervalForEnvironment returns how often to scan for idle
// missions. The test environment scans far more often so an end-to-end test can
// observe a decision without waiting minutes for it.
//
// Only the interval is shortened. defaultIdleTimeout is deliberately left alone
// in the test environment: shortening it would reap the missions other E2E
// sections create and depend on, turning one new test into cross-section
// flakiness. A test that needs a mission to look idle backdates that mission
// instead.
func idleTimeoutCheckIntervalForEnvironment() time.Duration {
	if config.IsTestEnv() {
		return 10 * time.Second
	}
	return idleTimeoutCheckInterval
}

// runIdleTimeoutLoop periodically scans running missions and stops wrappers
// that have been idle longer than the timeout threshold. The wrapper is
// automatically re-spawned on the next attach (lazy start).
func (s *Server) runIdleTimeoutLoop(ctx context.Context) {
	checkInterval := idleTimeoutCheckIntervalForEnvironment()

	// Initial delay to avoid racing with startup
	select {
	case <-ctx.Done():
		return
	case <-time.After(checkInterval):
		s.runIdleTimeoutCycle()
	}

	ticker := time.NewTicker(checkInterval)
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
		isAttached := m.TmuxPane != nil && linkedPaneIDs[*m.TmuxPane]

		// Both of these are re-checked inside shouldReapMission, which owns the
		// policy. They are repeated here only to skip the wrapper round-trip
		// below for the overwhelming majority of missions, which are either
		// attached or nowhere near the threshold.
		if isAttached || idleDuration < defaultIdleTimeout {
			continue
		}

		claudeState, wrapperUnresponsive := s.probeWrapperClaudeState(m.ID)
		if !shouldReapMission(idleDuration, defaultIdleTimeout, idleTimeoutHardCeiling, isAttached, claudeState, wrapperUnresponsive) {
			// Deliberately not logged: a spared mission is re-examined every
			// check interval, and logging each pass floods the server log.
			continue
		}

		// Spell out which signal condemned the mission. "no state" and a
		// reported state are different diagnoses for whoever reads this log.
		verdict := "no wrapper state available"
		switch {
		case idleDuration >= idleTimeoutHardCeiling:
			verdict = fmt.Sprintf("past the %s ceiling, wrapper state %q ignored", idleTimeoutHardCeiling, claudeState)
		case claudeState != "":
			verdict = fmt.Sprintf("wrapper reported %q", claudeState)
		}
		s.logger.Printf(
			"Idle timeout: stopping mission %s (idle for %s, %s)",
			database.ShortID(m.ID), idleDuration.Round(time.Second), verdict,
		)
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

// shouldReapMission decides whether a mission's wrapper should be stopped. It
// is the single statement of the reaping policy, kept pure so that every case
// below is testable without a server, tmux, or a live wrapper socket.
//
// idleDuration comes from the conversation log, which is only appended at turn
// and tool-call boundaries — so on its own it cannot tell a finished mission
// from one sitting inside a single long tool call. That is what claudeState is
// for: it is what the mission's wrapper reports, or "" when no state could be
// read at all. wrapperUnresponsive means a live wrapper failed to answer in
// time, the one case where the decision is deferred rather than made.
func shouldReapMission(
	idleDuration time.Duration,
	timeout time.Duration,
	ceiling time.Duration,
	isAttached bool,
	claudeState string,
	wrapperUnresponsive bool,
) bool {
	// A human is looking at this mission. Never reap it, whatever it reports.
	if isAttached {
		return false
	}
	if idleDuration < timeout {
		return false
	}
	// The backstop for a hook-fed signal that can be wrong. Past the ceiling,
	// nothing the wrapper reports — or fails to report — spares the mission.
	if idleDuration >= ceiling {
		return true
	}
	// A wrapper that is alive but slow to answer is far more likely to be busy
	// than finished: it holds its state lock across tmux calls while hooks
	// fire, which is exactly when Claude is active. Wait for the next cycle
	// rather than destroy work on the strength of a timeout.
	if wrapperUnresponsive {
		return false
	}
	if claudeState == claudeStateBusy || claudeState == claudeStateNeedsAttention {
		return false
	}
	// "idle", an empty state from a wrapper too old to report one, or no
	// wrapper state at all: fall through to the conversation log's verdict,
	// which the idleDuration check above has already delivered.
	return true
}

// probeWrapperClaudeState asks a mission's wrapper what Claude is doing.
// It returns the reported state, or unresponsive=true when a live wrapper
// failed to answer within the budget. Every other failure — no wrapper
// process, no socket to dial, an undecodable body — yields an empty state,
// which leaves the mission judged on its conversation log alone exactly as it
// was before the wrapper-state veto existed.
func (s *Server) probeWrapperClaudeState(missionID string) (state string, unresponsive bool) {
	status, err := s.queryWrapperStatus(missionID, reapWrapperQueryTimeout)
	if err == nil {
		return status.ClaudeState, false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "", true
	}
	return "", false
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

// missionIdleDuration returns how long it has been since a mission's active
// JSONL conversation log was last modified.
//
// Claude Code appends to that log at turn and tool-call boundaries, not
// continuously, so a recently modified file does mean Claude was recently
// active — but a stale one does NOT mean Claude is finished. A mission sitting
// inside a single long tool call, or driving a subagent, writes nothing for the
// duration. Treating that as idleness is the bug shouldReapMission's
// wrapper-state veto exists to prevent; this value is only half the picture.
//
// Falls back to created_at if the JSONL file cannot be located (mission has no
// session yet, or the project directory doesn't exist).
func (s *Server) missionIdleDuration(m *database.Mission, now time.Time) time.Duration {
	projectDirpath, err := claudeconfig.GetMissionProjectDirpath(s.agencDirpath, m.ID)
	if err != nil {
		return now.Sub(m.CreatedAt)
	}
	jsonlFilepath := session.FindActiveJSONLPath(projectDirpath)
	if jsonlFilepath != "" {
		if info, err := os.Stat(jsonlFilepath); err == nil {
			return now.Sub(info.ModTime())
		}
	}
	return now.Sub(m.CreatedAt)
}
