package wrapper

import (
	"os"
	"os/exec"
	"strings"

	"github.com/odyssey/agenc/internal/database"
)

const (
	agencTmuxEnvVar       = "AGENC_TMUX"
	agencParentPaneEnvVar = "AGENC_PARENT_PANE"
)

// renameWindowForTmux renames the current tmux window to "<short_id> <repo-name>"
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

	shortID := database.ShortID(w.missionID)
	windowTitle := shortID
	if w.gitRepoName != "" {
		repoName := extractRepoName(w.gitRepoName)
		if repoName != "" {
			windowTitle = shortID + " " + repoName
		}
	}

	//nolint:errcheck // best-effort; failure is not critical
	exec.Command("tmux", "rename-window", "-t", paneID, windowTitle).Run()
}

// returnToParentPane focuses the parent pane's window and pane when the wrapper
// exits. This implements the "return-on-exit" behavior for side missions spawned
// via 'agenc tmux window new'. If AGENC_PARENT_PANE is not set or the parent
// pane no longer exists, this is a no-op.
func (w *Wrapper) returnToParentPane() {
	parentPane := os.Getenv(agencParentPaneEnvVar)
	w.logger.Info("returnToParentPane called", "parentPane", parentPane)
	if parentPane == "" {
		return
	}

	// Get the window containing the parent pane
	windowIDOutput, err := exec.Command("tmux", "display-message", "-t", parentPane, "-p", "#{window_id}").Output()
	if err != nil {
		// Parent pane no longer exists — tmux will select the next window automatically
		w.logger.Warn("Parent pane no longer exists", "parentPane", parentPane, "error", err)
		return
	}

	windowID := strings.TrimSpace(string(windowIDOutput))
	w.logger.Info("Focusing parent window/pane", "windowID", windowID, "parentPane", parentPane)

	if err := exec.Command("tmux", "select-window", "-t", windowID).Run(); err != nil {
		w.logger.Warn("Failed to select parent window", "windowID", windowID, "error", err)
	}
	if err := exec.Command("tmux", "select-pane", "-t", parentPane).Run(); err != nil {
		w.logger.Warn("Failed to select parent pane", "parentPane", parentPane, "error", err)
	}
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
