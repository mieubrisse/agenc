package devcontainer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestParams(t *testing.T, repoDevcontainerPath string) OverlayParams {
	t.Helper()
	outputPath := filepath.Join(t.TempDir(), "devcontainer.json")
	return OverlayParams{
		RepoDevcontainerPath: repoDevcontainerPath,
		OutputPath:           outputPath,
		MissionID:            "test-uuid-123",
		AgencDirpath:         "/home/user/.agenc",
		HostAgentDirpath:     "/home/user/.agenc/missions/test-uuid-123/agent",
		ClaudeConfigDirpath:  "/home/user/.agenc/missions/test-uuid-123/claude-config",
		WrapperSocketPath:    "/home/user/.agenc/missions/test-uuid-123/wrapper.sock",
		OAuthToken:           "test-token",
	}
}

func writeDevcontainerJSON(t *testing.T, dir string, content string) string {
	t.Helper()
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	require.NoError(t, os.MkdirAll(devcontainerDir, 0755))
	configPath := filepath.Join(devcontainerDir, "devcontainer.json")
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0644))
	return configPath
}

func readOutputAsMap(t *testing.T, outputPath string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	var result map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &result))
	return result
}

func TestGenerateOverlay_AddsAgencMounts(t *testing.T) {
	repoDir := t.TempDir()
	configPath := writeDevcontainerJSON(t, repoDir, `{
		"image": "ubuntu:22.04",
		"workspaceFolder": "/workspaces/test-repo"
	}`)

	params := newTestParams(t, configPath)
	require.NoError(t, GenerateOverlay(params))

	result := readOutputAsMap(t, params.OutputPath)

	// Should preserve the original image
	require.Equal(t, "ubuntu:22.04", result["image"])

	// Should have mounts
	mounts, ok := result["mounts"].([]interface{})
	require.True(t, ok, "expected mounts array")
	require.NotEmpty(t, mounts, "expected mounts to be added")

	// Should have containerEnv
	env, ok := result["containerEnv"].(map[string]interface{})
	require.True(t, ok, "expected containerEnv")
	require.Equal(t, "test-uuid-123", env["AGENC_MISSION_UUID"])
	require.Equal(t, ContainerWrapperSocketPath, env["AGENC_WRAPPER_SOCKET"])
	require.Equal(t, "test-token", env["CLAUDE_CODE_OAUTH_TOKEN"])
}

func TestGenerateOverlay_AbsolutizesDockerfilePath(t *testing.T) {
	repoDir := t.TempDir()
	devcontainerDir := filepath.Join(repoDir, ".devcontainer")
	require.NoError(t, os.MkdirAll(devcontainerDir, 0755))

	// Repo uses a relative Dockerfile path
	configPath := filepath.Join(devcontainerDir, "devcontainer.json")
	require.NoError(t, os.WriteFile(configPath, []byte(`{
		"build": {
			"dockerfile": "../Dockerfile",
			"context": ".."
		}
	}`), 0644))

	// Create the referenced Dockerfile so the test is realistic
	require.NoError(t, os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte("FROM ubuntu"), 0644))

	params := newTestParams(t, configPath)
	require.NoError(t, GenerateOverlay(params))

	result := readOutputAsMap(t, params.OutputPath)
	build := result["build"].(map[string]interface{})

	dockerfile := build["dockerfile"].(string)
	context := build["context"].(string)

	require.True(t, filepath.IsAbs(dockerfile), "dockerfile should be absolute, got %q", dockerfile)
	require.True(t, filepath.IsAbs(context), "context should be absolute, got %q", context)
}

func TestGenerateOverlay_PreservesExistingMounts(t *testing.T) {
	repoDir := t.TempDir()
	configPath := writeDevcontainerJSON(t, repoDir, `{
		"image": "ubuntu:22.04",
		"mounts": ["source=my-cache,target=/cache,type=volume"]
	}`)

	params := newTestParams(t, configPath)
	require.NoError(t, GenerateOverlay(params))

	result := readOutputAsMap(t, params.OutputPath)
	mounts := result["mounts"].([]interface{})

	// Should contain both the original mount and AgenC's mounts
	require.GreaterOrEqual(t, len(mounts), 2, "expected at least 2 mounts (original + agenc)")

	// First mount should be the original
	require.Equal(t, "source=my-cache,target=/cache,type=volume", mounts[0])
}

func TestGenerateOverlay_PreservesExistingContainerEnv(t *testing.T) {
	repoDir := t.TempDir()
	configPath := writeDevcontainerJSON(t, repoDir, `{
		"image": "ubuntu:22.04",
		"containerEnv": {
			"MY_VAR": "my-value"
		}
	}`)

	params := newTestParams(t, configPath)
	require.NoError(t, GenerateOverlay(params))

	result := readOutputAsMap(t, params.OutputPath)
	env := result["containerEnv"].(map[string]interface{})

	// Original env var preserved
	require.Equal(t, "my-value", env["MY_VAR"])
	// AgenC env vars added
	require.Equal(t, "test-uuid-123", env["AGENC_MISSION_UUID"])
}

func TestGenerateOverlay_AbsolutizesDockerComposePath(t *testing.T) {
	repoDir := t.TempDir()
	configPath := writeDevcontainerJSON(t, repoDir, `{
		"dockerComposeFile": "../docker-compose.yml"
	}`)

	params := newTestParams(t, configPath)
	require.NoError(t, GenerateOverlay(params))

	result := readOutputAsMap(t, params.OutputPath)
	composePath := result["dockerComposeFile"].(string)
	require.True(t, filepath.IsAbs(composePath), "dockerComposeFile should be absolute, got %q", composePath)
}

func TestGenerateOverlay_CreatesHostProjectDir(t *testing.T) {
	repoDir := t.TempDir()
	configPath := writeDevcontainerJSON(t, repoDir, `{"image":"ubuntu"}`)

	params := newTestParams(t, configPath)
	require.NoError(t, GenerateOverlay(params))

	// Verify the host project directory was created
	homeDir, err := os.UserHomeDir()
	require.NoError(t, err)

	hostProjectDir := filepath.Join(homeDir, ".claude", "projects",
		EncodeProjectPath(params.HostAgentDirpath))
	info, err := os.Stat(hostProjectDir)
	require.NoError(t, err, "host project directory should exist")
	require.True(t, info.IsDir(), "should be a directory")
}

func TestGenerateOverlay_DefaultWorkspaceFolder(t *testing.T) {
	repoDir := t.TempDir()
	// No workspaceFolder specified — should default to /workspaces
	configPath := writeDevcontainerJSON(t, repoDir, `{"image":"ubuntu"}`)

	params := newTestParams(t, configPath)
	require.NoError(t, GenerateOverlay(params))

	result := readOutputAsMap(t, params.OutputPath)
	mounts := result["mounts"].([]interface{})

	// Find the session projects mount and verify it uses the default workspace folder
	hasSessionMount := false
	expectedContainerEncoded := EncodeProjectPath("/workspaces")
	for _, m := range mounts {
		s, ok := m.(string)
		if !ok {
			continue
		}
		if contains(s, expectedContainerEncoded) {
			hasSessionMount = true
			break
		}
	}
	require.True(t, hasSessionMount, "expected session bind mount with default /workspaces encoding")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
