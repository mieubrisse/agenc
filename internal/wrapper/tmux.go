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
// AgenC tmux session (AGENC_TMUX == 1). Priority order (highest to lowest):
// 1. AGENC_WINDOW_NAME env var (from `agenc tmux window new --name`)
// 2. windowTitle from config.yml
// 3. repo short name
// 4. mission ID
// In regular tmux sessions or outside tmux, this is a no-op.
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
	// Explicit --name from tmux window new takes highest priority
	if explicitName := os.Getenv("AGENC_WINDOW_NAME"); explicitName != "" {
		title = explicitName
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
//  1. AGENC_WINDOW_NAME env var (explicit --name flag — never overridden)
//  2. Custom title from Claude's /rename command
//  3. AI-generated summary from daemon (updated every ~10 user prompts)
//  4. Auto-generated session name from Claude's session metadata
//
// Only runs inside the AgenC tmux session (AGENC_TMUX == 1). Called on each
// Stop event so the title stays in sync as the session evolves.
func (w *Wrapper) updateWindowTitleFromSession() {
	if os.Getenv(agencTmuxEnvVar) != "1" {
		return
	}

	paneID := os.Getenv("TMUX_PANE")
	if paneID == "" {
		return
	}

	// Explicit --name from tmux window new takes absolute priority
	if os.Getenv("AGENC_WINDOW_NAME") != "" {
		return
	}

	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)

	// Custom title from /rename takes highest dynamic priority
	if customTitle := session.FindCustomTitle(claudeConfigDirpath, w.missionID); customTitle != "" {
		_ = w.db.UpdateMissionSessionName(w.missionID, customTitle)
		title := truncateWindowTitle(customTitle, maxWindowTitleLen)
		//nolint:errcheck // best-effort; failure is not critical
		exec.Command("tmux", "rename-window", "-t", paneID, title).Run()
		return
	}

	// AI-generated summary from the daemon (periodically updated based on
	// user activity). Preferred over auto-generated session summaries because
	// it reflects what the user is currently working on.
	if aiSummary, err := w.db.GetMissionAISummary(w.missionID); err == nil && aiSummary != "" {
		title := truncateWindowTitle(aiSummary, maxWindowTitleLen)
		//nolint:errcheck // best-effort; failure is not critical
		exec.Command("tmux", "rename-window", "-t", paneID, title).Run()
		return
	}

	// Fall back to auto-generated session name from Claude's session metadata
	sessionName := session.FindSessionName(claudeConfigDirpath, w.missionID)
	if sessionName == "" {
		return
	}

	_ = w.db.UpdateMissionSessionName(w.missionID, sessionName)
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
