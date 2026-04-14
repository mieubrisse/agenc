package wrapper

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/devcontainer"
)

// devcontainerState holds the state needed for devcontainer lifecycle management.
type devcontainerState struct {
	// mergedConfigPath is the path to the AgenC-merged devcontainer.json
	mergedConfigPath string

	// agentDirpath is the workspace folder passed to devcontainer CLI
	agentDirpath string
}

// detectAndSetupDevcontainer checks if the repo has a devcontainer.json
// and prepares the overlay config if so. Returns nil if no devcontainer found.
func (w *Wrapper) detectAndSetupDevcontainer() (*devcontainerState, error) {
	repoConfigPath, found := devcontainer.DetectDevcontainer(w.agentDirpath)
	if !found {
		return nil, nil
	}

	missionDirpath := config.GetMissionDirpath(w.agencDirpath, w.missionID)
	mergedConfigPath := filepath.Join(missionDirpath, "devcontainer.json")
	claudeConfigDirpath := filepath.Join(missionDirpath, claudeconfig.MissionClaudeConfigDirname)
	wrapperSocketPath := config.GetMissionSocketFilepath(w.agencDirpath, w.missionID)

	oauthToken, err := config.ReadOAuthToken(w.agencDirpath)
	if err != nil {
		return nil, fmt.Errorf("reading OAuth token for devcontainer overlay: %w", err)
	}

	params := devcontainer.OverlayParams{
		RepoDevcontainerPath: repoConfigPath,
		OutputPath:           mergedConfigPath,
		MissionID:            w.missionID,
		AgencDirpath:         w.agencDirpath,
		HostAgentDirpath:     w.agentDirpath,
		ClaudeConfigDirpath:  claudeConfigDirpath,
		WrapperSocketPath:    wrapperSocketPath,
		OAuthToken:           oauthToken,
	}

	if err := devcontainer.GenerateOverlay(params); err != nil {
		return nil, fmt.Errorf("generating devcontainer overlay: %w", err)
	}

	return &devcontainerState{
		mergedConfigPath: mergedConfigPath,
		agentDirpath:     w.agentDirpath,
	}, nil
}

// devcontainerUp starts the container. Idempotent — creates if missing,
// starts if stopped, no-ops if running.
func devcontainerUp(state *devcontainerState) error {
	cmd := exec.Command("devcontainer", "up",
		"--workspace-folder", state.agentDirpath,
		"--config", state.mergedConfigPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// devcontainerExecClaude builds an exec.Cmd that runs Claude inside the
// devcontainer via `devcontainer exec`. The returned command has stdin/stdout/stderr
// connected to the parent process.
func devcontainerExecClaude(state *devcontainerState, claudeArgs []string) *exec.Cmd {
	args := []string{
		"exec",
		"--workspace-folder", state.agentDirpath,
		"--config", state.mergedConfigPath,
		"--",
		"claude",
	}
	args = append(args, claudeArgs...)

	cmd := exec.Command("devcontainer", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd
}

// devcontainerStop stops the container.
func devcontainerStop(state *devcontainerState) error {
	cmd := exec.Command("devcontainer", "stop",
		"--workspace-folder", state.agentDirpath,
		"--config", state.mergedConfigPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// devcontainerRebuild stops, removes, and rebuilds the container.
func devcontainerRebuild(state *devcontainerState) error {
	// Stop first (ignore error — container might not be running)
	_ = devcontainerStop(state)

	cmd := exec.Command("devcontainer", "up",
		"--workspace-folder", state.agentDirpath,
		"--config", state.mergedConfigPath,
		"--build-no-cache",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
