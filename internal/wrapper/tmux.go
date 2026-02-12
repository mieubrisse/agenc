package wrapper

import (
	"os"
	"os/exec"
	"strings"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/session"
)

const (
	agencTmuxEnvVar = "AGENC_TMUX"
)

// renameWindowForTmux renames the current tmux window when running inside the
// AgenC tmux session (AGENC_TMUX == 1). Uses the custom windowTitle from
// config.yml if set, otherwise falls back to the repo's short name, and
// finally to the mission ID. In regular tmux sessions or outside tmux, this
// is a no-op.
func (w *Wrapper) renameWindowForTmux() {
	if os.Getenv(agencTmuxEnvVar) != "1" {
		return
	}

	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return
	}

	title := w.missionID
	if w.gitRepoName != "" {
		repoName := extractRepoName(w.gitRepoName)
		if repoName != "" {
			title = repoName
		}
	}
	if w.windowTitle != "" {
		title = w.windowTitle
	}

	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "rename-window", "-t", paneID, title).Run()
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

// resetWindowTabStyle unsets the window-level window-status-style override,
// letting the global tmux style show through. No-op outside tmux.
func (w *Wrapper) resetWindowTabStyle() {
	windowID := resolveWindowID()
	if windowID == "" {
		return
	}
	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "set-option", "-wu", "-t", windowID, "window-status-style").Run()
}

// setWindowTabColor sets the background color of this window's title in the
// tmux tab bar via window-status-style. Only the inactive style is set;
// window-status-current-style is left alone so the user's active-window
// styling is preserved. No-op outside tmux.
func (w *Wrapper) setWindowTabColor(color string) {
	windowID := resolveWindowID()
	if windowID == "" {
		return
	}
	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "set-option", "-w", "-t", windowID, "window-status-style", "bg="+color).Run()
}

// resolveWindowID returns the tmux window ID (e.g. "@3") for the pane this
// process is running in, or "" if not inside tmux.
func resolveWindowID() string {
	if os.Getenv("TMUX") == "" {
		return ""
	}
	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{window_id}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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

// updateWindowTitleFromSession checks for a session name (set by Claude via
// /rename or auto-generated summaries) and updates the tmux window title to
// match. Only runs inside the AgenC tmux session (AGENC_TMUX == 1). If a
// custom windowTitle was set via config.yml, it takes priority and this is a
// no-op. Called on each Stop event so the title stays in sync as the session
// name evolves.
func (w *Wrapper) updateWindowTitleFromSession() {
	if os.Getenv(agencTmuxEnvVar) != "1" {
		return
	}

	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return
	}

	// Custom windowTitle from config.yml takes priority — don't override it
	if w.windowTitle != "" {
		return
	}

	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	sessionName := session.FindSessionName(claudeConfigDirpath, w.missionID)
	if sessionName == "" {
		return
	}

	// Cache the session name in the database for mission listings
	_ = w.db.UpdateMissionSessionName(w.missionID, sessionName)

	// Truncate for tmux tab display
	title := truncateWindowTitle(sessionName, maxWindowTitleLen)

	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "rename-window", "-t", paneID, title).Run()
}

// truncateWindowTitle truncates a string to maxLen characters, appending an
// ellipsis if truncation occurs. Collapses internal whitespace first.
func truncateWindowTitle(title string, maxLen int) string {
	collapsed := strings.Join(strings.Fields(title), " ")
	if len(collapsed) <= maxLen {
		return collapsed
	}
	return collapsed[:maxLen-1] + "…"
}

// maxWindowTitleLen is the maximum character length for tmux window titles
// derived from session names. Keeps tabs readable without excessive truncation.
const maxWindowTitleLen = 30

// Window tab color constants for tmux visual feedback.
const (
	// windowBusyColor is displayed when Claude is actively working.
	windowBusyColor = "colour018"

	// windowAttentionColor is displayed when the mission needs user attention
	// (Claude is idle, waiting for permission, etc.).
	windowAttentionColor = "colour136"
)
