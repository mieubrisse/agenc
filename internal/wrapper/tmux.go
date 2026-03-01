package wrapper

import (
	"os"
	"os/exec"
	"strings"

	"github.com/odyssey/agenc/internal/server"
)

// isSolePaneInWindow returns true if the given pane is the only pane in its window.
// Returns false if the window has multiple panes or if detection fails.
func isSolePaneInWindow(paneID string) bool {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{window_panes}").Output()
	if err != nil {
		return false
	}
	paneCount := strings.TrimSpace(string(out))
	return paneCount == "1"
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
	pane := strings.TrimPrefix(paneID, "%")
	_ = w.client.UpdateMission(w.missionID, server.UpdateMissionRequest{TmuxPane: &pane})
}

// clearTmuxPane removes the tmux pane association for this mission.
func (w *Wrapper) clearTmuxPane() {
	empty := ""
	_ = w.client.UpdateMission(w.missionID, server.UpdateMissionRequest{TmuxPane: &empty})
}

// setWindowBusy sets the tmux window tab to the busy colors, indicating
// Claude is actively working. No-op when TMUX_PANE is empty or both colors are empty.
func (w *Wrapper) setWindowBusy() {
	if w.windowBusyBackgroundColor == "" && w.windowBusyForegroundColor == "" {
		return
	}
	w.setWindowTabColors(w.windowBusyBackgroundColor, w.windowBusyForegroundColor)
}

// setWindowNeedsAttention sets the tmux window tab to the attention colors,
// signaling that the mission needs user interaction (e.g., Claude is idle or
// waiting for permission). No-op when TMUX_PANE is empty or both colors are empty.
func (w *Wrapper) setWindowNeedsAttention() {
	if w.windowAttentionBackgroundColor == "" && w.windowAttentionForegroundColor == "" {
		return
	}
	w.setWindowTabColors(w.windowAttentionBackgroundColor, w.windowAttentionForegroundColor)
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

// setWindowTabColors sets the foreground and background colors of this window's title in the
// tmux tab bar via window-status-style. Only the inactive style is set;
// window-status-current-style is left alone so the user's active-window
// styling is preserved. No-op outside tmux. Empty strings for either color mean that
// color component is not changed.
func (w *Wrapper) setWindowTabColors(bgColor, fgColor string) {
	windowID := resolveWindowID()
	if windowID == "" {
		return
	}

	var styleComponents []string
	if bgColor != "" {
		styleComponents = append(styleComponents, "bg="+bgColor)
	}
	if fgColor != "" {
		styleComponents = append(styleComponents, "fg="+fgColor)
	}

	if len(styleComponents) == 0 {
		return
	}

	style := strings.Join(styleComponents, ",")
	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "set-option", "-w", "-t", windowID, "window-status-style", style).Run()
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
