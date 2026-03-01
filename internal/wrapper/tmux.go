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

// renameWindowForTmux renames the current tmux window when running inside
// any tmux session. Priority order (highest to lowest):
//  1. AGENC_WINDOW_NAME env var (from `agenc tmux window new --name`)
//  2. windowTitle from config.yml
//  3. repo short name
//  4. mission ID
//
// Only renames the window if this pane is the sole pane in the window and the
// user has not manually renamed the window since the last AgenC-managed rename.
// In regular tmux sessions or outside tmux, this is a no-op.
func (w *Wrapper) renameWindowForTmux() {
	if os.Getenv("TMUX") == "" {
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
	// Explicit --name from tmux window new takes highest priority
	if explicitName := os.Getenv("AGENC_WINDOW_NAME"); explicitName != "" {
		title = explicitName
	}

	w.applyWindowTitle(paneID, title)
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

// extractRepoName extracts just the repository name from a canonical repo
// reference like "owner/repo" or "host/owner/repo". Returns just "repo".
func extractRepoName(gitRepoName string) string {
	parts := strings.Split(gitRepoName, "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[len(parts)-1]
}

// currentWindowName returns the current name of the tmux window containing
// paneID, by querying tmux directly. Returns "" if the query fails or we are
// not inside tmux.
func currentWindowName(paneID string) string {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", paneID, "#{window_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// applyWindowTitle renames the tmux window for this pane to title, subject to
// two guards:
//  1. The window must contain only this pane (no split panes).
//  2. If AgenC has previously set a title, the current window name must still
//     match it — if it doesn't, the user has manually renamed the window and
//     we respect that.
//
// After a successful rename, the new title is stored in the database so future
// calls can detect user renames.
func (w *Wrapper) applyWindowTitle(paneID string, title string) {
	if !isSolePaneInWindow(paneID) {
		return
	}

	// Respect user renames: if AgenC set a title before and the window no
	// longer shows it, the user has renamed the window manually.
	if missionRecord, err := w.client.GetMission(w.missionID); err == nil && missionRecord.TmuxWindowTitle != "" {
		if current := currentWindowName(paneID); current != missionRecord.TmuxWindowTitle {
			return
		}
	}

	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "rename-window", "-t", paneID, title).Run()
	//nolint:errcheck // best-effort; failure is not critical
	_ = w.client.UpdateMission(w.missionID, server.UpdateMissionRequest{TmuxWindowTitle: &title})
}
