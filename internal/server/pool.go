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
// tmux session. The window appears in the target session as a shared window.
func linkPoolWindow(poolWindowTarget string, targetSession string) error {
	cmd := exec.Command("tmux", "link-window", "-s", poolWindowTarget, "-t", targetSession+":")
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
