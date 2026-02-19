package wrapper

import (
	"os"
	"os/exec"
	"strings"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/session"
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
	_ = w.db.SetTmuxPane(w.missionID, strings.TrimPrefix(paneID, "%"))
}

// clearTmuxPane removes the tmux pane association for this mission.
func (w *Wrapper) clearTmuxPane() {
	_ = w.db.ClearTmuxPane(w.missionID)
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

// updateWindowTitleFromSession updates the tmux window title based on the best
// available name for this mission. Priority order (highest to lowest):
//  1. Custom title from Claude's /rename command (beats everything, including --name)
//  2. AGENC_WINDOW_NAME env var (explicit --name flag — fixed; no AI/session updates)
//  3. AI-generated summary from daemon (updated every ~10 user prompts)
//  4. Auto-generated session name from Claude's session metadata
//
// Only runs inside a tmux session. Called on each Stop event so the title
// stays in sync as the session evolves.
func (w *Wrapper) updateWindowTitleFromSession() {
	if os.Getenv("TMUX") == "" {
		return
	}

	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return
	}

	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)

	// Custom title from /rename takes highest dynamic priority — beats even an
	// explicit --name flag so Quick Claude sessions can still be renamed.
	if customTitle := session.FindCustomTitle(claudeConfigDirpath, w.missionID); customTitle != "" {
		_ = w.db.UpdateMissionSessionName(w.missionID, customTitle)
		title := truncateWindowTitle(customTitle, maxWindowTitleLen)
		w.applyWindowTitle(paneID, title)
		return
	}

	// If an explicit --name was provided at launch, treat it as a fixed title.
	// Don't update it with AI summaries or session names (only /rename can override).
	if os.Getenv("AGENC_WINDOW_NAME") != "" {
		return
	}

	// AI-generated summary from the daemon (periodically updated based on
	// user activity). Preferred over auto-generated session summaries because
	// it reflects what the user is currently working on.
	if aiSummary, err := w.db.GetMissionAISummary(w.missionID); err == nil && aiSummary != "" {
		_ = w.db.UpdateMissionSessionName(w.missionID, aiSummary)
		title := truncateWindowTitle(aiSummary, maxWindowTitleLen)
		w.applyWindowTitle(paneID, title)
		return
	}

	// Fall back to auto-generated session name from Claude's session metadata
	sessionName := session.FindSessionName(claudeConfigDirpath, w.missionID)
	if sessionName == "" {
		return
	}

	_ = w.db.UpdateMissionSessionName(w.missionID, sessionName)
	title := truncateWindowTitle(sessionName, maxWindowTitleLen)
	w.applyWindowTitle(paneID, title)
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
	if storedTitle, err := w.db.GetMissionTmuxWindowTitle(w.missionID); err == nil && storedTitle != "" {
		if current := currentWindowName(paneID); current != storedTitle {
			return
		}
	}

	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "rename-window", "-t", paneID, title).Run()
	//nolint:errcheck // best-effort; failure is not critical
	_ = w.db.SetMissionTmuxWindowTitle(w.missionID, title)
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
