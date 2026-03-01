package server

import (
	"os/exec"
	"strings"
	"unicode/utf8"

	"github.com/odyssey/agenc/internal/database"
)

const (
	// maxTmuxWindowTitleLen is the maximum character length for tmux window titles.
	maxTmuxWindowTitleLen = 30
)

// reconcileTmuxWindowTitle examines all available data for a mission and
// converges the tmux window to the correct title. This function is idempotent
// and can be called from any context (scanner, summarizer, mission switch).
//
// Title priority (highest to lowest):
//  1. Active session's custom_title (from /rename)
//  2. Active session's auto_summary (from Claude or AgenC summarizer)
//  3. Repo short name (from git_repo)
//  4. Mission short ID (fallback)
func (s *Server) reconcileTmuxWindowTitle(missionID string) {
	// Step 1: Get the active session's metadata
	activeSession, err := s.db.GetActiveSession(missionID)
	if err != nil {
		s.logger.Printf("Tmux reconcile: failed to get active session for %s: %v", missionID, err)
		return
	}

	// Step 2: Get mission data for tmux_pane, tmux_window_title, git_repo
	mission, err := s.db.GetMission(missionID)
	if err != nil || mission == nil {
		return
	}

	// Step 3: Determine the best title
	bestTitle := determineBestTitle(activeSession, mission)

	// Step 4: Apply the title to tmux
	s.applyTmuxTitle(mission, bestTitle)
}

// determineBestTitle picks the best available title using the priority chain.
func determineBestTitle(activeSession *database.Session, mission *database.Mission) string {
	// Priority 1: custom_title from /rename
	if activeSession != nil && activeSession.CustomTitle != "" {
		return activeSession.CustomTitle
	}

	// Priority 2: auto_summary
	if activeSession != nil && activeSession.AutoSummary != "" {
		return activeSession.AutoSummary
	}

	// Priority 3: repo short name
	if mission.GitRepo != "" {
		repoName := extractRepoShortName(mission.GitRepo)
		if repoName != "" {
			return repoName
		}
	}

	// Priority 4: mission short ID
	return mission.ShortID
}

// applyTmuxTitle applies a title to the tmux window for a mission, subject to
// guards (sole pane, user override detection).
func (s *Server) applyTmuxTitle(mission *database.Mission, title string) {
	// No tmux pane registered -- mission is not running in tmux
	if mission.TmuxPane == nil || *mission.TmuxPane == "" {
		return
	}

	// Database stores pane IDs without the "%" prefix (e.g. "3043"), but tmux
	// commands require it (e.g. "%3043") to identify panes.
	paneID := "%" + *mission.TmuxPane

	// Guard: only rename if this pane is the sole pane in its window
	if !isSolePaneInTmuxWindow(paneID) {
		return
	}

	// Guard: if we previously set a title and the current window name differs,
	// the user has manually renamed the window -- respect that
	if mission.TmuxWindowTitle != "" {
		currentName := queryTmuxWindowName(paneID)
		if currentName != mission.TmuxWindowTitle {
			return
		}
	}

	truncatedTitle := truncateTitle(title, maxTmuxWindowTitleLen)

	// Skip if the title has not actually changed
	if truncatedTitle == mission.TmuxWindowTitle {
		return
	}

	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "rename-window", "-t", paneID, truncatedTitle).Run()

	if err := s.db.SetMissionTmuxWindowTitle(mission.ID, truncatedTitle); err != nil {
		s.logger.Printf("Tmux reconcile: failed to save window title for %s: %v", mission.ShortID, err)
	}
}

// isSolePaneInTmuxWindow returns true if the given pane is the only pane in its
// tmux window. Returns false if the window has multiple panes or if detection fails.
func isSolePaneInTmuxWindow(paneID string) bool {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{window_panes}").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "1"
}

// queryTmuxWindowName returns the current name of the tmux window containing
// paneID. Returns "" if the query fails.
func queryTmuxWindowName(paneID string) string {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{window_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// truncateTitle truncates a string to maxLen runes, appending an ellipsis if
// truncation occurs. Collapses internal whitespace first.
func truncateTitle(title string, maxLen int) string {
	collapsed := strings.Join(strings.Fields(title), " ")
	if utf8.RuneCountInString(collapsed) <= maxLen {
		return collapsed
	}
	runes := []rune(collapsed)
	return string(runes[:maxLen-1]) + "…"
}

// extractRepoShortName extracts just the repository name from a canonical repo
// reference like "owner/repo" or "host/owner/repo". Returns just "repo".
func extractRepoShortName(gitRepo string) string {
	parts := strings.Split(gitRepo, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}
