package wrapper

import (
	"os"
	"os/exec"
	"strings"
)

const (
	agencTmuxEnvVar = "AGENC_TMUX"
)

// renameWindowForTmux renames the current tmux window to the repo name
// when running inside the AgenC tmux session (AGENC_TMUX == 1). In regular tmux
// sessions or outside tmux, this is a no-op.
func (w *Wrapper) renameWindowForTmux() {
	if os.Getenv(agencTmuxEnvVar) != "1" {
		return
	}

	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return
	}

	windowTitle := w.missionID
	if w.gitRepoName != "" {
		repoName := extractRepoName(w.gitRepoName)
		if repoName != "" {
			windowTitle = repoName
		}
	}

	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "rename-window", "-t", paneID, windowTitle).Run()
}

// registerTmuxPane records the current tmux pane ID in the database so that
// keybindings can resolve which mission is focused. No-ops when not inside tmux
// (e.g. headless mode).
func (w *Wrapper) registerTmuxPane() {
	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return
	}
	_ = w.db.SetTmuxPane(w.missionID, paneID)
}

// clearTmuxPane removes the tmux pane association for this mission.
func (w *Wrapper) clearTmuxPane() {
	_ = w.db.ClearTmuxPane(w.missionID)
}

// setPaneNeedsAttention sets the tmux pane background to the attention color,
// signaling that the mission needs user interaction (e.g., Claude is idle or
// waiting for permission). No-op when TMUX_PANE is empty.
func (w *Wrapper) setPaneNeedsAttention() {
	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return
	}
	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "select-pane", "-t", paneID, "-P", "bg="+paneAttentionColor).Run()
}

// resetPaneStyle resets the tmux pane background to the terminal default.
// No-op when TMUX_PANE is empty.
func (w *Wrapper) resetPaneStyle() {
	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return
	}
	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "select-pane", "-t", paneID, "-P", "bg="+paneDefaultColor).Run()
}

// extractRepoName extracts just the repository name from a canonical repo
// reference like "owner/repo" or "host/owner/repo". Returns just "repo".
func extractRepoName(gitRepoName string) string {
	parts := strings.Split(gitRepoName, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// Pane color constants for tmux visual feedback.
const (
	// paneAttentionColor is displayed when the mission needs user attention
	// (Claude is idle, waiting for permission, etc.). Dark teal — visible on
	// dark terminals without being garish.
	paneAttentionColor = "colour022"

	// paneDefaultColor resets the pane to the terminal's normal background.
	paneDefaultColor = "default"
)
