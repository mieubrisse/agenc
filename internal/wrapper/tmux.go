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
//
// The pane number is stored WITHOUT the "%" prefix that $TMUX_PANE includes,
// since tmux format variables like #{pane_id} omit it. Stripping the prefix
// here keeps the database representation canonical; callers that need the
// tmux-native form (e.g. tmux rename-window -t) should prepend "%" themselves.
func (w *Wrapper) registerTmuxPane() {
	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return
	}
	_ = w.db.SetTmuxPane(w.missionID, strings.TrimPrefix(paneID, "%"))
}

// clearTmuxPane removes the tmux pane association for this mission.
func (w *Wrapper) clearTmuxPane() {
	_ = w.db.ClearTmuxPane(w.missionID)
}

// setWindowBusy sets the tmux window tab to the busy color, indicating
// Claude is actively working. No-op when TMUX_PANE is empty.
func (w *Wrapper) setWindowBusy() {
	w.setWindowTabColor(windowBusyColor)
}

// setWindowNeedsAttention sets the tmux window tab to the attention color,
// signaling that the mission needs user interaction (e.g., Claude is idle or
// waiting for permission). No-op when TMUX_PANE is empty.
func (w *Wrapper) setWindowNeedsAttention() {
	w.setWindowTabColor(windowAttentionColor)
}

// resetWindowTabStyle resets the tmux window tab to the default style.
// No-op when TMUX_PANE is empty.
func (w *Wrapper) resetWindowTabStyle() {
	w.setWindowTabColor(windowDefaultColor)
}

// setWindowTabColor sets the background color of this window's title in the
// tmux tab bar. Both window-status-style (inactive) and
// window-status-current-style (active) are set so the color is visible
// regardless of which window is focused. No-op when TMUX_PANE is empty.
func (w *Wrapper) setWindowTabColor(color string) {
	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return
	}
	style := "bg=" + color
	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "set-window-option", "-t", paneID, "window-status-style", style).Run()
	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "set-window-option", "-t", paneID, "window-status-current-style", style).Run()
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

// Window tab color constants for tmux visual feedback.
const (
	// windowBusyColor is displayed when Claude is actively working.
	windowBusyColor = "colour018"

	// windowAttentionColor is displayed when the mission needs user attention
	// (Claude is idle, waiting for permission, etc.).
	windowAttentionColor = "colour136"

	// windowDefaultColor resets the window tab to the default style.
	windowDefaultColor = "default"
)
