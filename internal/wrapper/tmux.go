package wrapper

import (
	"os"
	"os/exec"
	"strings"


)

const (
	agencTmuxEnvVar       = "AGENC_TMUX"
	agencParentPaneEnvVar = "AGENC_PARENT_PANE"
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

// returnFocusToParentPane switches tmux focus back to the parent pane that
// spawned this mission window. The parent pane ID is passed via the
// AGENC_PARENT_PANE env var, set by `agenc tmux window new` / `agenc tmux pane new`.
// This is a no-op if the env var is not set (e.g., the initial session window).
func (w *Wrapper) returnFocusToParentPane() {
	parentPaneID := os.Getenv(agencParentPaneEnvVar)
	if parentPaneID == "" {
		return
	}

	// Look up the parent pane's window (it may have moved since creation)
	windowIDOutput, err := exec.Command("tmux", "display-message", "-t", parentPaneID, "-p", "#{window_id}").Output()
	if err == nil {
		windowID := strings.TrimSpace(string(windowIDOutput))
		//nolint:errcheck // best-effort
		exec.Command("tmux", "select-window", "-t", windowID).Run()
	}
	//nolint:errcheck // best-effort
	exec.Command("tmux", "select-pane", "-t", parentPaneID).Run()
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
