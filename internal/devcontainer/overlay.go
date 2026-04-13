package devcontainer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// ContainerWrapperSocketPath is the well-known path where the wrapper
	// socket is mounted inside containers, following Docker's /var/run convention.
	ContainerWrapperSocketPath = "/var/run/agenc/wrapper.sock"
)

// OverlayParams contains the parameters needed to generate a merged
// devcontainer.json with AgenC's overlay.
type OverlayParams struct {
	// RepoDevcontainerPath is the path to the repo's devcontainer.json
	RepoDevcontainerPath string

	// OutputPath is where the merged devcontainer.json will be written
	OutputPath string

	// MissionID is the mission UUID
	MissionID string

	// AgencDirpath is the root agenc directory (e.g., ~/.agenc)
	AgencDirpath string

	// HostAgentDirpath is the mission's agent directory on the host
	HostAgentDirpath string

	// ClaudeConfigDirpath is the mission's claude-config directory on the host
	ClaudeConfigDirpath string

	// WrapperSocketPath is the path to wrapper.sock on the host
	WrapperSocketPath string

	// OAuthToken is the Claude API OAuth token
	OAuthToken string
}

// GenerateOverlay reads the repo's devcontainer.json, merges AgenC's overlay
// (mounts, env vars), absolutizes relative paths, and writes the result.
func GenerateOverlay(params OverlayParams) error {
	data, err := os.ReadFile(params.RepoDevcontainerPath)
	if err != nil {
		return fmt.Errorf("reading devcontainer.json at '%s': %w", params.RepoDevcontainerPath, err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("parsing devcontainer.json: %w", err)
	}

	originalConfigDir := filepath.Dir(params.RepoDevcontainerPath)

	// Absolutize relative paths in build section
	absolutizeBuildPaths(config, originalConfigDir)

	// Absolutize dockerComposeFile if present
	absolutizeDockerComposePath(config, originalConfigDir)

	// Determine container workspace path for session mount
	containerWorkspacePath := getWorkspaceFolder(config)

	// Compute session bind mount paths
	sessionMount := ComputeSessionBindMount(params.HostAgentDirpath, containerWorkspacePath)

	// Ensure host project directory exists for the bind mount
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("determining home directory: %w", err)
	}
	hostProjectDir := filepath.Join(homeDir, ".claude", "projects", sessionMount.HostProjectDirName)
	if err := os.MkdirAll(hostProjectDir, 0700); err != nil {
		return fmt.Errorf("creating host project directory '%s': %w", hostProjectDir, err)
	}

	// Compute container home path from remoteUser/containerUser
	containerHome := getContainerHomePath(config)

	// Build AgenC mounts
	agencMounts := buildAgencMounts(params, sessionMount, homeDir, containerHome)

	// Concatenate mounts (repo's first, then AgenC's)
	existingMounts := getMountsFromConfig(config)
	allMounts := make([]interface{}, 0, len(existingMounts)+len(agencMounts))
	allMounts = append(allMounts, existingMounts...)
	allMounts = append(allMounts, agencMounts...)
	config["mounts"] = allMounts

	// Add/merge containerEnv (AgenC overrides on conflict)
	mergeContainerEnv(config, map[string]string{
		"AGENC_MISSION_UUID":      params.MissionID,
		"AGENC_WRAPPER_SOCKET":    ContainerWrapperSocketPath,
		"CLAUDE_CODE_OAUTH_TOKEN": params.OAuthToken,
	})

	// Write merged config
	if err := os.MkdirAll(filepath.Dir(params.OutputPath), 0755); err != nil {
		return fmt.Errorf("creating output directory for '%s': %w", params.OutputPath, err)
	}

	output, err := json.MarshalIndent(config, "", "\t")
	if err != nil {
		return fmt.Errorf("marshaling merged config: %w", err)
	}

	if err := os.WriteFile(params.OutputPath, output, 0644); err != nil {
		return fmt.Errorf("writing merged config to '%s': %w", params.OutputPath, err)
	}

	return nil
}

func absolutizeBuildPaths(config map[string]interface{}, originalConfigDir string) {
	buildRaw, ok := config["build"]
	if !ok {
		return
	}
	build, ok := buildRaw.(map[string]interface{})
	if !ok {
		return
	}

	// Absolutize context (relative to config dir)
	if contextRaw, ok := build["context"]; ok {
		if context, ok := contextRaw.(string); ok && !filepath.IsAbs(context) {
			build["context"] = filepath.Clean(filepath.Join(originalConfigDir, context))
		}
	}

	// Absolutize dockerfile (relative to context dir, which defaults to config dir)
	if dockerfileRaw, ok := build["dockerfile"]; ok {
		if dockerfile, ok := dockerfileRaw.(string); ok && !filepath.IsAbs(dockerfile) {
			contextDir := originalConfigDir
			if contextRaw, ok := build["context"]; ok {
				if context, ok := contextRaw.(string); ok {
					contextDir = context // already absolutized above
				}
			}
			build["dockerfile"] = filepath.Clean(filepath.Join(contextDir, dockerfile))
		}
	}
}

func absolutizeDockerComposePath(config map[string]interface{}, originalConfigDir string) {
	composeRaw, ok := config["dockerComposeFile"]
	if !ok {
		return
	}

	switch v := composeRaw.(type) {
	case string:
		if !filepath.IsAbs(v) {
			config["dockerComposeFile"] = filepath.Clean(filepath.Join(originalConfigDir, v))
		}
	case []interface{}:
		for i, item := range v {
			if s, ok := item.(string); ok && !filepath.IsAbs(s) {
				v[i] = filepath.Clean(filepath.Join(originalConfigDir, s))
			}
		}
	}
}

func getWorkspaceFolder(config map[string]interface{}) string {
	if wf, ok := config["workspaceFolder"]; ok {
		if s, ok := wf.(string); ok {
			return s
		}
	}
	// Default per devcontainer spec when using image/Dockerfile
	return "/workspaces"
}

// getContainerHomePath computes the home directory path inside the container
// based on the remoteUser/containerUser from devcontainer.json. The devcontainer
// spec checks remoteUser first, then containerUser, then defaults to "root".
func getContainerHomePath(config map[string]interface{}) string {
	user := ""
	if ru, ok := config["remoteUser"]; ok {
		if s, ok := ru.(string); ok {
			user = s
		}
	}
	if user == "" {
		if cu, ok := config["containerUser"]; ok {
			if s, ok := cu.(string); ok {
				user = s
			}
		}
	}
	if user == "" || user == "root" {
		return "/root"
	}
	return "/home/" + user
}

func buildAgencMounts(params OverlayParams, sessionMount SessionBindMount, homeDir string, containerHome string) []interface{} {
	claudeProjectsDir := filepath.Join(homeDir, ".claude", "projects")
	containerClaudeDir := containerHome + "/.claude"

	// Directories from host ~/.claude/ to bind-mount into container
	sharedClaudeDirs := []string{
		"todos", "tasks", "debug", "file-history", "shell-snapshots",
	}

	mounts := []interface{}{
		// Claude config as ~/.claude (read-write base for .claude.json etc)
		fmt.Sprintf("source=%s,target=%s,type=bind", params.ClaudeConfigDirpath, containerClaudeDir),
		// CLAUDE.md read-only overlay
		fmt.Sprintf("source=%s/CLAUDE.md,target=%s/CLAUDE.md,type=bind,readonly", params.ClaudeConfigDirpath, containerClaudeDir),
		// settings.json read-only overlay
		fmt.Sprintf("source=%s/settings.json,target=%s/settings.json,type=bind,readonly", params.ClaudeConfigDirpath, containerClaudeDir),
		// Session projects encoding translation
		fmt.Sprintf("source=%s/%s,target=%s/projects/%s,type=bind",
			claudeProjectsDir, sessionMount.HostProjectDirName, containerClaudeDir, sessionMount.ContainerProjectDirName),
		// Wrapper socket
		fmt.Sprintf("source=%s,target=%s,type=bind", params.WrapperSocketPath, ContainerWrapperSocketPath),
	}

	// Add shared Claude data directories
	for _, dirName := range sharedClaudeDirs {
		hostDir := filepath.Join(homeDir, ".claude", dirName)
		mounts = append(mounts, fmt.Sprintf(
			"source=%s,target=%s/%s,type=bind", hostDir, containerClaudeDir, dirName))
	}

	return mounts
}

func getMountsFromConfig(config map[string]interface{}) []interface{} {
	mountsRaw, ok := config["mounts"]
	if !ok {
		return nil
	}
	mounts, ok := mountsRaw.([]interface{})
	if !ok {
		return nil
	}
	return mounts
}

func mergeContainerEnv(config map[string]interface{}, agencEnv map[string]string) {
	envRaw, ok := config["containerEnv"]
	var env map[string]interface{}
	if ok {
		env, ok = envRaw.(map[string]interface{})
		if !ok {
			env = make(map[string]interface{})
		}
	} else {
		env = make(map[string]interface{})
	}

	// AgenC overrides on conflict
	for k, v := range agencEnv {
		env[k] = v
	}

	config["containerEnv"] = env
}
