package server

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/database"
)

const (
	poolSessionName = "agenc-pool"
)

// ensurePoolSession creates the agenc-pool tmux session if it doesn't already exist.
// The session is created detached with a single placeholder window that is
// destroyed once real mission windows are added.
func (s *Server) ensurePoolSession() error {
	if poolSessionExists() {
		return nil
	}

	cmd := exec.Command("tmux", "new-session", "-d", "-s", poolSessionName, "-x", "200", "-y", "50")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.NewError("failed to create pool session: %v (output: %s)", err, string(output))
	}

	s.logger.Printf("Created tmux pool session: %s", poolSessionName)
	return nil
}

// poolSessionExists checks whether the agenc-pool tmux session exists.
func poolSessionExists() bool {
	cmd := exec.Command("tmux", "has-session", "-t", poolSessionName)
	return cmd.Run() == nil
}

// createPoolWindow creates a new window in the agenc-pool session for the given
// mission. The window runs the specified command and is named with the short
// mission ID for easy identification.
func (s *Server) createPoolWindow(missionID string, command string) (string, error) {
	if err := s.ensurePoolSession(); err != nil {
		return "", err
	}

	windowName := database.ShortID(missionID)
	target := fmt.Sprintf("%s:", poolSessionName)

	cmd := exec.Command("tmux", "new-window", "-d", "-t", target, "-n", windowName, command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", stacktrace.NewError("failed to create pool window: %v (output: %s)", err, string(output))
	}

	// Return the full target for linking: "agenc-pool:<windowName>"
	windowTarget := fmt.Sprintf("%s:%s", poolSessionName, windowName)
	s.logger.Printf("Created pool window %s for mission %s", windowTarget, database.ShortID(missionID))
	return windowTarget, nil
}

// linkPoolWindow links a window from the agenc-pool session into the target
// tmux session. The window appears adjacent to the caller's current window
// (-a) without stealing focus (-d).
func linkPoolWindow(poolWindowTarget string, targetSession string) error {
	cmd := exec.Command("tmux", "link-window", "-d", "-a", "-s", poolWindowTarget, "-t", targetSession+":")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.NewError("failed to link window: %v (output: %s)", err, string(output))
	}
	return nil
}

// unlinkPoolWindow unlinks a mission's window from the target session. The
// window continues to exist in the agenc-pool session.
func unlinkPoolWindow(targetSession string, missionID string) error {
	windowName := database.ShortID(missionID)
	target := fmt.Sprintf("%s:%s", targetSession, windowName)

	cmd := exec.Command("tmux", "unlink-window", "-t", target)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.NewError("failed to unlink window: %v (output: %s)", err, string(output))
	}
	return nil
}

// destroyPoolWindow kills a mission's window in the agenc-pool session.
func (s *Server) destroyPoolWindow(missionID string) {
	windowName := database.ShortID(missionID)
	target := fmt.Sprintf("%s:%s", poolSessionName, windowName)

	cmd := exec.Command("tmux", "kill-window", "-t", target)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Non-fatal: window may already be gone
		s.logger.Printf("Warning: failed to destroy pool window %s: %v (output: %s)", target, err, strings.TrimSpace(string(output)))
	}
}

// poolWindowExists checks whether a window with the given mission ID exists
// in the agenc-pool session.
func poolWindowExists(missionID string) bool {
	windowName := database.ShortID(missionID)
	target := fmt.Sprintf("%s:%s", poolSessionName, windowName)

	cmd := exec.Command("tmux", "has-session", "-t", target)
	return cmd.Run() == nil
}

// getLinkedPaneIDs returns the set of tmux pane IDs (without the "%" prefix)
// that are visible in at least one tmux session besides agenc-pool. This uses
// pane IDs rather than window names because window names can be renamed by tmux
// or by the running process, making them unreliable identifiers. Pane IDs are
// immutable for the lifetime of the pane.
//
// If the tmux command fails (e.g., no server running), returns an empty map so
// the caller falls through to the existing idle-kill behavior.
func getLinkedPaneIDs() map[string]bool {
	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{session_name} #{pane_id}")
	output, err := cmd.Output()
	if err != nil {
		return map[string]bool{}
	}

	// Track which sessions each pane appears in
	paneSessions := make(map[string]map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		sessionName := parts[0]
		// Strip the "%" prefix from pane IDs to match the database format
		paneID := strings.TrimPrefix(parts[1], "%")
		if paneSessions[paneID] == nil {
			paneSessions[paneID] = make(map[string]bool)
		}
		paneSessions[paneID][sessionName] = true
	}

	// A pane is "linked" if it appears in any session besides agenc-pool
	linked := make(map[string]bool)
	for paneID, sessions := range paneSessions {
		for sessionName := range sessions {
			if sessionName != poolSessionName {
				linked[paneID] = true
				break
			}
		}
	}
	return linked
}

// listPoolPaneIDs returns the pane IDs (without "%" prefix) of all panes
// currently running in the agenc-pool tmux session. Returns an empty slice
// if the pool doesn't exist or tmux is not running.
func listPoolPaneIDs() []string {
	cmd := exec.Command("tmux", "list-panes", "-s", "-t", poolSessionName, "-F", "#{pane_id}")
	output, err := cmd.Output()
	if err != nil {
		return nil
	}

	var paneIDs []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Strip the "%" prefix — DB stores pane IDs without it
		paneIDs = append(paneIDs, strings.TrimPrefix(line, "%"))
	}
	return paneIDs
}
