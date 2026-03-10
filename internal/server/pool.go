package server

import (
	"fmt"
	"os/exec"
	"sort"
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

// tmuxSessionExists checks whether a named tmux session exists.
// Uses tmux exact-match syntax (=name) to prevent prefix matching
// (e.g., "agenc" would otherwise match "agenc-pool").
func tmuxSessionExists(sessionName string) bool {
	return exec.Command("tmux", "has-session", "-t", "="+sessionName).Run() == nil
}

// poolSessionExists checks whether the agenc-pool tmux session exists.
func poolSessionExists() bool {
	return tmuxSessionExists(poolSessionName)
}

// createPoolWindow creates a new window in the agenc-pool session for the given
// mission. The window runs the specified command and is named with the short
// mission ID for easy identification.
func (s *Server) createPoolWindow(missionID string, command string) (string, string, error) {
	if err := s.ensurePoolSession(); err != nil {
		return "", "", err
	}

	windowName := database.ShortID(missionID)
	target := fmt.Sprintf("=%s:", poolSessionName)

	cmd := exec.Command("tmux", "new-window", "-d", "-P", "-F", "#{pane_id}", "-t", target, "-n", windowName, command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", "", stacktrace.NewError("failed to create pool window: %v (output: %s)", err, string(output))
	}

	// Parse pane ID from output (e.g. "%42") and strip the "%" prefix
	// to match the DB convention (database stores "42", not "%42").
	paneID := strings.TrimSpace(string(output))
	paneID = strings.TrimPrefix(paneID, "%")

	// Return the full target for linking: "agenc-pool:<windowName>"
	windowTarget := fmt.Sprintf("%s:%s", poolSessionName, windowName)
	s.logger.Printf("Created pool window %s (pane %s) for mission %s", windowTarget, paneID, database.ShortID(missionID))
	return windowTarget, paneID, nil
}

// unlinkPoolWindowByPane unlinks the window containing the given pane from the
// target session. Uses the pane ID (immutable) rather than the window name
// (which may have been changed by title reconciliation).
// The window continues to exist in the agenc-pool session.
func unlinkPoolWindowByPane(paneID string, targetSession string) error {
	paneTarget := "%" + paneID
	// Find the window index in the target session that contains this pane
	cmd := exec.Command("tmux", "list-panes", "-s", "-t", "="+targetSession, "-F", "#{pane_id} #{window_index}")
	output, err := cmd.Output()
	if err != nil {
		return stacktrace.NewError("failed to list panes in session %s: %v", targetSession, err)
	}
	windowIndex := ""
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
		if len(parts) == 2 && parts[0] == paneTarget {
			windowIndex = parts[1]
			break
		}
	}
	if windowIndex == "" {
		return stacktrace.NewError("pane %s not found in session %s", paneID, targetSession)
	}

	windowTarget := fmt.Sprintf("%s:%s", targetSession, windowIndex)
	unlinkCmd := exec.Command("tmux", "unlink-window", "-t", windowTarget)
	unlinkOutput, err := unlinkCmd.CombinedOutput()
	if err != nil {
		return stacktrace.NewError("failed to unlink window: %v (output: %s)", err, string(unlinkOutput))
	}
	return nil
}

// destroyPoolWindow kills the tmux window containing the given pane. Uses the
// pane ID (immutable) rather than the window name (which may have been changed
// by title reconciliation). No-op if paneID is empty.
func (s *Server) destroyPoolWindow(paneID string) {
	if paneID == "" {
		return
	}
	paneTarget := "%" + paneID
	cmd := exec.Command("tmux", "kill-window", "-t", paneTarget)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Non-fatal: window may already be gone
		s.logger.Printf("Warning: failed to destroy pool window for pane %s: %v (output: %s)", paneID, err, strings.TrimSpace(string(output)))
	}
}

// poolWindowExistsByPane checks whether the given pane exists in the agenc-pool
// session. Uses the pane ID (immutable) rather than the window name (which may
// have been changed by title reconciliation).
func poolWindowExistsByPane(paneID string) bool {
	if paneID == "" {
		return false
	}
	return isPaneInSession(paneID, poolSessionName)
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

// getLinkedPaneSessions returns a map of pane IDs to the list of tmux session
// names they are linked into (excluding the agenc-pool session). Pane IDs are
// returned without the "%" prefix to match the database convention.
//
// If the tmux command fails (e.g., no server running), returns an empty map.
func getLinkedPaneSessions() map[string][]string {
	cmd := exec.Command("tmux", "list-panes", "-a", "-F", "#{session_name} #{pane_id}")
	output, err := cmd.Output()
	if err != nil {
		return map[string][]string{}
	}

	// Collect which sessions each pane appears in (excluding pool)
	paneSessions := make(map[string]map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		sessionName := parts[0]
		if sessionName == poolSessionName {
			continue
		}
		paneID := strings.TrimPrefix(parts[1], "%")
		if paneSessions[paneID] == nil {
			paneSessions[paneID] = make(map[string]bool)
		}
		paneSessions[paneID][sessionName] = true
	}

	// Convert to sorted slices for deterministic output
	result := make(map[string][]string, len(paneSessions))
	for paneID, sessions := range paneSessions {
		names := make([]string, 0, len(sessions))
		for name := range sessions {
			names = append(names, name)
		}
		sort.Strings(names)
		result[paneID] = names
	}
	return result
}

// isPaneInSession checks whether a pane (by numeric ID without "%" prefix) is
// currently visible in the given tmux session. Returns false if the session
// doesn't exist or the tmux command fails.
func isPaneInSession(paneID string, sessionName string) bool {
	target := "%" + paneID
	cmd := exec.Command("tmux", "list-panes", "-s", "-t", "="+sessionName, "-F", "#{pane_id}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.TrimSpace(line) == target {
			return true
		}
	}
	return false
}

// linkPoolWindowByPane links the pool window containing the given pane into
// the target tmux session. Uses the pane ID (immutable) rather than the window
// name (which may have been changed by title reconciliation).
func linkPoolWindowByPane(paneID string, targetSession string) error {
	paneTarget := "%" + paneID
	cmd := exec.Command("tmux", "link-window", "-d", "-a", "-s", paneTarget, "-t", "="+targetSession+":")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.NewError("failed to link window by pane: %v (output: %s)", err, string(output))
	}
	return nil
}

// focusPaneInSession switches focus to the window containing the given pane in
// the specified tmux session. Best-effort: errors are silently ignored.
// Uses the pane ID to find the window index in the target session, which is
// reliable even after title reconciliation renames the window.
func focusPaneInSession(paneID string, sessionName string) {
	paneTarget := "%" + paneID
	// Query all panes in the session to find the window index for our pane
	cmd := exec.Command("tmux", "list-panes", "-s", "-t", "="+sessionName, "-F", "#{pane_id} #{window_index}")
	output, err := cmd.Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), " ", 2)
		if len(parts) == 2 && parts[0] == paneTarget {
			windowTarget := fmt.Sprintf("%s:%s", sessionName, parts[1])
			//nolint:errcheck // best-effort
			exec.Command("tmux", "select-window", "-t", windowTarget).Run()
			return
		}
	}
}

// listPoolPaneIDs returns the pane IDs (without "%" prefix) of all panes
// currently running in the agenc-pool tmux session. Returns an empty slice
// if the pool doesn't exist or tmux is not running.
func listPoolPaneIDs() []string {
	cmd := exec.Command("tmux", "list-panes", "-s", "-t", "="+poolSessionName, "-F", "#{pane_id}")
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

// sendKeysToPane sends keystrokes to the given tmux pane by invoking
// tmux send-keys. Keys are passed as separate arguments — no shell
// interpolation. Uses tmux's native key name syntax (Enter, C-c, Escape, etc.).
func sendKeysToPane(paneID string, keys []string) error {
	paneTarget := "%" + paneID
	args := append([]string{"send-keys", "-t", paneTarget}, keys...)
	cmd := exec.Command("tmux", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.NewError("tmux send-keys failed: %v (output: %s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}
