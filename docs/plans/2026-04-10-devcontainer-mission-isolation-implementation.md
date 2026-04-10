Devcontainer Mission Isolation Implementation Plan
===================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Run AgenC missions inside devcontainers so the container boundary replaces Claude Code's permission system as the safety net, eliminating permission fatigue.

**Architecture:** Per-repo opt-in via devcontainer.json. Wrapper detects devcontainer, generates a merged overlay config, manages container lifecycle (up/exec/stop), and spawns Claude inside via `devcontainer exec`. Strict nesting: wrapper(container(claude)).

**Tech Stack:** Go, devcontainer CLI, Docker (via Docker Desktop), curl-based hooks

**Design doc:** `docs/plans/2026-04-10-devcontainer-mission-isolation-design.md`

---

Task 1: Devcontainer Detection
-------------------------------

**Files:**
- Create: `internal/devcontainer/detection.go`
- Create: `internal/devcontainer/detection_test.go`

**Step 1: Write failing tests**

```go
// internal/devcontainer/detection_test.go
package devcontainer

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectDevcontainer_InDotDevcontainerDir(t *testing.T) {
	dir := t.TempDir()
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	os.MkdirAll(devcontainerDir, 0755)
	os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(`{"image":"ubuntu"}`), 0644)

	path, found := DetectDevcontainer(dir)
	if !found {
		t.Fatal("expected to find devcontainer.json")
	}
	if path != filepath.Join(devcontainerDir, "devcontainer.json") {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestDetectDevcontainer_RootFile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".devcontainer.json"), []byte(`{"image":"ubuntu"}`), 0644)

	path, found := DetectDevcontainer(dir)
	if !found {
		t.Fatal("expected to find .devcontainer.json")
	}
	if path != filepath.Join(dir, ".devcontainer.json") {
		t.Fatalf("unexpected path: %s", path)
	}
}

func TestDetectDevcontainer_PrefersSubdir(t *testing.T) {
	dir := t.TempDir()
	// Both exist — .devcontainer/ dir takes precedence per spec
	devcontainerDir := filepath.Join(dir, ".devcontainer")
	os.MkdirAll(devcontainerDir, 0755)
	os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(`{"image":"ubuntu"}`), 0644)
	os.WriteFile(filepath.Join(dir, ".devcontainer.json"), []byte(`{"image":"debian"}`), 0644)

	path, found := DetectDevcontainer(dir)
	if !found {
		t.Fatal("expected to find devcontainer.json")
	}
	if path != filepath.Join(devcontainerDir, "devcontainer.json") {
		t.Fatal("should prefer .devcontainer/ dir over root file")
	}
}

func TestDetectDevcontainer_NotFound(t *testing.T) {
	dir := t.TempDir()
	_, found := DetectDevcontainer(dir)
	if found {
		t.Fatal("should not find devcontainer.json")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/devcontainer/... -v`
Expected: Compilation error (package doesn't exist yet)

**Step 3: Write implementation**

```go
// internal/devcontainer/detection.go
package devcontainer

import (
	"os"
	"path/filepath"
)

// DetectDevcontainer checks if a repository has a devcontainer configuration.
// It checks two locations per the devcontainer spec:
//  1. .devcontainer/devcontainer.json (preferred)
//  2. .devcontainer.json (root file)
//
// Returns the absolute path to the config file and whether one was found.
func DetectDevcontainer(repoDir string) (string, bool) {
	// Check .devcontainer/devcontainer.json first (spec-preferred location)
	subdirPath := filepath.Join(repoDir, ".devcontainer", "devcontainer.json")
	if _, err := os.Stat(subdirPath); err == nil {
		return subdirPath, true
	}

	// Fall back to .devcontainer.json in repo root
	rootPath := filepath.Join(repoDir, ".devcontainer.json")
	if _, err := os.Stat(rootPath); err == nil {
		return rootPath, true
	}

	return "", false
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/devcontainer/... -v`
Expected: All 4 tests PASS

**Step 5: Commit**

```
git add internal/devcontainer/detection.go internal/devcontainer/detection_test.go
git commit -m "Add devcontainer detection for repo opt-in"
```

---

Task 2: Project Path Encoding
------------------------------

**Files:**
- Create: `internal/devcontainer/project_path_encoding.go`
- Create: `internal/devcontainer/project_path_encoding_test.go`

**Context:** Claude Code encodes working directory paths by replacing `/` and `.` with `-`. The existing `ComputeProjectDirpath` in `internal/claudeconfig/build.go:667-679` does this for the host. We need a function that computes both host and container encoded paths for the session bind mount.

**Step 1: Write failing tests**

```go
// internal/devcontainer/project_path_encoding_test.go
package devcontainer

import (
	"testing"
)

func TestEncodeProjectPath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple workspace path",
			path:     "/workspaces/my-repo",
			expected: "-workspaces-my-repo",
		},
		{
			name:     "host mission agent path",
			path:     "/Users/odyssey/.agenc/missions/abc-123/agent",
			expected: "-Users-odyssey--agenc-missions-abc-123-agent",
		},
		{
			name:     "path with dots",
			path:     "/home/user/.config/app",
			expected: "-home-user--config-app",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := EncodeProjectPath(tt.path)
			if result != tt.expected {
				t.Errorf("EncodeProjectPath(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestComputeSessionBindMount(t *testing.T) {
	mount := ComputeSessionBindMount(
		"/Users/odyssey/.agenc/missions/abc-123/agent",
		"/workspaces/my-repo",
	)

	expectedHostEncoded := "-Users-odyssey--agenc-missions-abc-123-agent"
	expectedContainerEncoded := "-workspaces-my-repo"

	if mount.HostProjectDirName != expectedHostEncoded {
		t.Errorf("host encoded = %q, want %q", mount.HostProjectDirName, expectedHostEncoded)
	}
	if mount.ContainerProjectDirName != expectedContainerEncoded {
		t.Errorf("container encoded = %q, want %q", mount.ContainerProjectDirName, expectedContainerEncoded)
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/devcontainer/... -run TestEncode -v`
Expected: Compilation error

**Step 3: Write implementation**

```go
// internal/devcontainer/project_path_encoding.go
package devcontainer

import "strings"

// SessionBindMount contains the host and container project directory names
// for the session files bind mount.
type SessionBindMount struct {
	// HostProjectDirName is the encoded directory name as it appears on the host
	// under ~/.claude/projects/
	HostProjectDirName string

	// ContainerProjectDirName is the encoded directory name as Claude inside
	// the container will write to under ~/.claude/projects/
	ContainerProjectDirName string
}

// EncodeProjectPath encodes a directory path the same way Claude Code does:
// replace "/" and "." with "-".
func EncodeProjectPath(dirpath string) string {
	return strings.ReplaceAll(strings.ReplaceAll(dirpath, "/", "-"), ".", "-")
}

// ComputeSessionBindMount computes the host and container project directory
// names needed to bind-mount the session files so they land at the correct
// host location despite Claude running inside a container with a different
// workspace path.
func ComputeSessionBindMount(hostAgentDirpath string, containerWorkspacePath string) SessionBindMount {
	return SessionBindMount{
		HostProjectDirName:      EncodeProjectPath(hostAgentDirpath),
		ContainerProjectDirName: EncodeProjectPath(containerWorkspacePath),
	}
}
```

**Step 4: Run tests**

Run: `go test ./internal/devcontainer/... -run TestEncode -v && go test ./internal/devcontainer/... -run TestComputeSession -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/devcontainer/project_path_encoding.go internal/devcontainer/project_path_encoding_test.go
git commit -m "Add project path encoding for container session bind mounts"
```

---

Task 3: Devcontainer JSON Parsing and Overlay Generation
---------------------------------------------------------

**Files:**
- Create: `internal/devcontainer/overlay.go`
- Create: `internal/devcontainer/overlay_test.go`

**Context:** Read the repo's devcontainer.json, add AgenC's mounts and env vars, absolutize relative paths, and write the merged config. The merged config goes to `~/.agenc/missions/<uuid>/devcontainer.json`.

**Step 1: Write failing tests**

```go
// internal/devcontainer/overlay_test.go
package devcontainer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateOverlay_AddsAgencMounts(t *testing.T) {
	repoDir := t.TempDir()
	devcontainerDir := filepath.Join(repoDir, ".devcontainer")
	os.MkdirAll(devcontainerDir, 0755)
	os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(`{
		"image": "ubuntu:22.04",
		"workspaceFolder": "/workspaces/test-repo"
	}`), 0644)

	outputPath := filepath.Join(t.TempDir(), "devcontainer.json")

	params := OverlayParams{
		RepoDevcontainerPath:   filepath.Join(devcontainerDir, "devcontainer.json"),
		OutputPath:             outputPath,
		MissionID:              "test-uuid-123",
		AgencDirpath:           "/home/user/.agenc",
		HostAgentDirpath:       "/home/user/.agenc/missions/test-uuid-123/agent",
		ClaudeConfigDirpath:    "/home/user/.agenc/missions/test-uuid-123/claude-config",
		WrapperSocketPath:      "/home/user/.agenc/missions/test-uuid-123/wrapper.sock",
		OAuthToken:             "test-token",
	}

	err := GenerateOverlay(params)
	if err != nil {
		t.Fatalf("GenerateOverlay failed: %v", err)
	}

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read output: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}

	// Should preserve the original image
	if result["image"] != "ubuntu:22.04" {
		t.Errorf("image should be preserved, got %v", result["image"])
	}

	// Should have mounts
	mounts, ok := result["mounts"].([]interface{})
	if !ok || len(mounts) == 0 {
		t.Fatal("expected mounts to be added")
	}

	// Should have containerEnv
	env, ok := result["containerEnv"].(map[string]interface{})
	if !ok {
		t.Fatal("expected containerEnv")
	}
	if env["AGENC_MISSION_UUID"] != "test-uuid-123" {
		t.Errorf("missing AGENC_MISSION_UUID")
	}
	if env["AGENC_WRAPPER_SOCKET"] != "/var/run/agenc/wrapper.sock" {
		t.Errorf("missing AGENC_WRAPPER_SOCKET")
	}
}

func TestGenerateOverlay_AbsolutizesDockerfilePath(t *testing.T) {
	repoDir := t.TempDir()
	devcontainerDir := filepath.Join(repoDir, ".devcontainer")
	os.MkdirAll(devcontainerDir, 0755)

	// Repo uses a relative Dockerfile path
	os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(`{
		"build": {
			"dockerfile": "../Dockerfile",
			"context": ".."
		}
	}`), 0644)

	// Create the referenced Dockerfile so the test is realistic
	os.WriteFile(filepath.Join(repoDir, "Dockerfile"), []byte("FROM ubuntu"), 0644)

	outputPath := filepath.Join(t.TempDir(), "devcontainer.json")

	params := OverlayParams{
		RepoDevcontainerPath: filepath.Join(devcontainerDir, "devcontainer.json"),
		OutputPath:           outputPath,
		MissionID:            "test-uuid",
		AgencDirpath:         "/home/user/.agenc",
		HostAgentDirpath:     "/home/user/.agenc/missions/test-uuid/agent",
		ClaudeConfigDirpath:  "/home/user/.agenc/missions/test-uuid/claude-config",
		WrapperSocketPath:    "/home/user/.agenc/missions/test-uuid/wrapper.sock",
		OAuthToken:           "test-token",
	}

	err := GenerateOverlay(params)
	if err != nil {
		t.Fatalf("GenerateOverlay failed: %v", err)
	}

	data, _ := os.ReadFile(outputPath)
	var result map[string]interface{}
	json.Unmarshal(data, &result)

	build := result["build"].(map[string]interface{})
	dockerfile := build["dockerfile"].(string)
	context := build["context"].(string)

	if !filepath.IsAbs(dockerfile) {
		t.Errorf("dockerfile should be absolute, got %q", dockerfile)
	}
	if !filepath.IsAbs(context) {
		t.Errorf("context should be absolute, got %q", context)
	}
}

func TestGenerateOverlay_PreservesExistingMounts(t *testing.T) {
	repoDir := t.TempDir()
	devcontainerDir := filepath.Join(repoDir, ".devcontainer")
	os.MkdirAll(devcontainerDir, 0755)
	os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(`{
		"image": "ubuntu:22.04",
		"mounts": ["source=my-cache,target=/cache,type=volume"]
	}`), 0644)

	outputPath := filepath.Join(t.TempDir(), "devcontainer.json")

	params := OverlayParams{
		RepoDevcontainerPath: filepath.Join(devcontainerDir, "devcontainer.json"),
		OutputPath:           outputPath,
		MissionID:            "test-uuid",
		AgencDirpath:         "/home/user/.agenc",
		HostAgentDirpath:     "/home/user/.agenc/missions/test-uuid/agent",
		ClaudeConfigDirpath:  "/home/user/.agenc/missions/test-uuid/claude-config",
		WrapperSocketPath:    "/home/user/.agenc/missions/test-uuid/wrapper.sock",
		OAuthToken:           "test-token",
	}

	GenerateOverlay(params)

	data, _ := os.ReadFile(outputPath)
	var result map[string]interface{}
	json.Unmarshal(data, &result)

	mounts := result["mounts"].([]interface{})
	// Should contain both the original mount and AgenC's mounts
	if len(mounts) < 2 {
		t.Errorf("expected at least 2 mounts (original + agenc), got %d", len(mounts))
	}

	// First mount should be the original
	if mounts[0] != "source=my-cache,target=/cache,type=volume" {
		t.Errorf("original mount not preserved: %v", mounts[0])
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/devcontainer/... -run TestGenerateOverlay -v`
Expected: Compilation error

**Step 3: Write implementation**

```go
// internal/devcontainer/overlay.go
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
	// Read and parse the repo's devcontainer.json
	data, err := os.ReadFile(params.RepoDevcontainerPath)
	if err != nil {
		return fmt.Errorf("reading devcontainer.json: %w", err)
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

	// Build AgenC mounts
	agencMounts := buildAgencMounts(params, sessionMount)

	// Concatenate mounts (repo's first, then AgenC's)
	existingMounts := getMountsFromConfig(config)
	config["mounts"] = append(existingMounts, agencMounts...)

	// Add/merge containerEnv (AgenC overrides on conflict)
	mergeContainerEnv(config, map[string]string{
		"AGENC_MISSION_UUID":     params.MissionID,
		"AGENC_WRAPPER_SOCKET":   ContainerWrapperSocketPath,
		"CLAUDE_CODE_OAUTH_TOKEN": params.OAuthToken,
	})

	// Write merged config
	if err := os.MkdirAll(filepath.Dir(params.OutputPath), 0755); err != nil {
		return fmt.Errorf("creating output directory: %w", err)
	}

	output, err := json.MarshalIndent(config, "", "\t")
	if err != nil {
		return fmt.Errorf("marshaling merged config: %w", err)
	}

	if err := os.WriteFile(params.OutputPath, output, 0644); err != nil {
		return fmt.Errorf("writing merged config: %w", err)
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

	// Absolutize dockerfile (relative to context, which defaults to config dir)
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

func buildAgencMounts(params OverlayParams, sessionMount SessionBindMount) []interface{} {
	homeDir, _ := os.UserHomeDir()
	claudeProjectsDir := filepath.Join(homeDir, ".claude", "projects")

	// Directories from host ~/.claude/ to bind-mount into container
	sharedClaudeDirs := []string{
		"todos", "tasks", "debug", "file-history", "shell-snapshots",
	}

	mounts := []interface{}{
		// Claude config as ~/.claude (read-write base for .claude.json etc)
		fmt.Sprintf("source=%s,target=%%l/.claude,type=bind", params.ClaudeConfigDirpath),
		// CLAUDE.md read-only overlay
		fmt.Sprintf("source=%s/CLAUDE.md,target=%%l/.claude/CLAUDE.md,type=bind,readonly", params.ClaudeConfigDirpath),
		// settings.json read-only overlay
		fmt.Sprintf("source=%s/settings.json,target=%%l/.claude/settings.json,type=bind,readonly", params.ClaudeConfigDirpath),
		// Session projects encoding translation
		fmt.Sprintf("source=%s/%s,target=%%l/.claude/projects/%s,type=bind",
			claudeProjectsDir, sessionMount.HostProjectDirName, sessionMount.ContainerProjectDirName),
		// Wrapper socket
		fmt.Sprintf("source=%s,target=%s,type=bind", params.WrapperSocketPath, ContainerWrapperSocketPath),
	}

	// Add shared Claude data directories
	for _, dirName := range sharedClaudeDirs {
		hostDir := filepath.Join(homeDir, ".claude", dirName)
		mounts = append(mounts, fmt.Sprintf(
			"source=%s,target=%%l/.claude/%s,type=bind", hostDir, dirName))
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
```

**NOTE:** The `%l` in mount targets is a placeholder. During implementation, research the correct devcontainer mount syntax for referencing the container user's home directory. The devcontainer spec may use `${containerEnv:HOME}` or require absolute paths. If `~` is not supported in mount targets, determine the container user from `remoteUser`/`containerUser` in the config and compute the home path.

**Step 4: Run tests**

Run: `go test ./internal/devcontainer/... -run TestGenerateOverlay -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/devcontainer/overlay.go internal/devcontainer/overlay_test.go
git commit -m "Add devcontainer overlay generation with AgenC mount injection"
```

---

Task 4: Claude-Config Container Mode
--------------------------------------

**Files:**
- Modify: `internal/claudeconfig/build.go` (lines 36-121)
- Modify: `internal/claudeconfig/overrides.go` (lines 19-39)
- Modify: `internal/claudeconfig/build_test.go`

**Context:** When building claude-config for a containerized mission:
1. Skip symlinks to `~/.claude/` (bind mounts handle this instead)
2. Generate curl-based hooks instead of `agenc` CLI hooks
3. Ensure host project directory exists before devcontainer up

**Step 1: Write failing tests for curl hook generation**

Add to existing test file or create new test functions:

```go
func TestBuildContainerHookEntries(t *testing.T) {
	entries := BuildContainerHookEntries()

	// Should have entries for all hook events
	expectedEvents := []string{"Stop", "UserPromptSubmit", "Notification", "PostToolUse", "PostToolUseFailure"}
	for _, event := range expectedEvents {
		raw, ok := entries[event]
		if !ok {
			t.Errorf("missing hook entry for %s", event)
			continue
		}
		var parsed interface{}
		json.Unmarshal(raw, &parsed)
		str := string(raw)
		if !strings.Contains(str, "curl") {
			t.Errorf("hook for %s should use curl, got: %s", event, str)
		}
		if !strings.Contains(str, "$AGENC_WRAPPER_SOCKET") {
			t.Errorf("hook for %s should reference AGENC_WRAPPER_SOCKET, got: %s", event, str)
		}
		if !strings.Contains(str, "/claude-update/"+event) {
			t.Errorf("hook for %s should have event in URL path, got: %s", event, str)
		}
	}
}
```

**Step 2: Run to verify failure**

Run: `go test ./internal/claudeconfig/... -run TestBuildContainerHook -v`
Expected: Compilation error

**Step 3: Implement curl hook entries**

Modify `internal/claudeconfig/overrides.go`:

```go
// Add new function alongside existing AgencHookEntries
var ContainerHookEntries map[string]json.RawMessage

func init() {
	// ... existing AgencHookEntries init ...

	// Container hooks use curl to reach the wrapper socket
	ContainerHookEntries = make(map[string]json.RawMessage, len(agencHookEventNames))
	for _, eventName := range agencHookEventNames {
		// curl to wrapper socket with event type in URL path, forwarding stdin as body
		cmd := fmt.Sprintf(
			`curl -s --unix-socket $AGENC_WRAPPER_SOCKET -X POST http://w/claude-update/%s -H "Content-Type: application/json" -d @- -o /dev/null || true`,
			eventName,
		)
		entry := fmt.Sprintf(`[{"hooks":[{"type":"command","command":"%s"}]}]`, cmd)
		ContainerHookEntries[eventName] = json.RawMessage(entry)
	}
}

func BuildContainerHookEntries() map[string]json.RawMessage {
	return ContainerHookEntries
}
```

**Step 4: Add containerized parameter to BuildMissionConfigDir**

Modify the signature and logic in `internal/claudeconfig/build.go`:

```go
func BuildMissionConfigDir(
	agencDirpath string,
	missionID string,
	trustedMcpServers *config.TrustedMcpServers,
	containerized bool,  // NEW PARAMETER
) error {
	// ... existing code up to symlink section (line ~96) ...

	// Choose hooks based on containerization
	hookEntries := AgencHookEntries
	if containerized {
		hookEntries = ContainerHookEntries
	}

	// ... pass hookEntries to MergeSettings instead of AgencHookEntries ...

	if containerized {
		// For containerized missions, create empty directories instead of symlinks.
		// Bind mounts from the host will overlay these at container start.
		for _, dirName := range symlinkDirNames {
			dirPath := filepath.Join(claudeConfigDirpath, dirName)
			_ = os.RemoveAll(dirPath)
			if err := os.MkdirAll(dirPath, 0700); err != nil {
				return stacktrace.Propagate(err, "creating directory '%s'", dirName)
			}
		}
	} else {
		// Non-containerized: symlink to ~/.claude/ as before
		for _, dirName := range symlinkDirNames {
			if err := symlinkToGlobalClaudeDir(claudeConfigDirpath, dirName); err != nil {
				return stacktrace.Propagate(err, "symlinking '%s'", dirName)
			}
		}
	}

	// ... rest of function ...
}
```

**Step 5: Update all callers of BuildMissionConfigDir**

Search for all call sites and add `false` (non-containerized) as the default:

- `internal/mission/mission.go` — `CreateMissionDir()` call
- Any other callers

The wrapper will call `BuildMissionConfigDir(..., true)` for containerized missions.

**Step 6: Run all tests**

Run: `go test ./internal/claudeconfig/... -v`
Expected: PASS

**Step 7: Commit**

```
git add internal/claudeconfig/overrides.go internal/claudeconfig/build.go internal/mission/mission.go
git commit -m "Add container mode to claude-config: curl hooks, no symlinks"
```

---

Task 5: Wrapper Socket URL-Path-Based Hook Endpoint
-----------------------------------------------------

**Files:**
- Modify: `internal/wrapper/socket.go`
- Modify: `internal/wrapper/socket.go` tests (if they exist)

**Context:** The existing `/claude-update` endpoint expects event name in the JSON body. Container hooks put the event name in the URL path (`/claude-update/Stop`). Add URL-path-based routing to the wrapper socket.

**Step 1: Add new endpoint pattern**

Modify `internal/wrapper/socket.go` where routes are registered (around line 88-90):

```go
// Existing endpoint (keep for backward compat with non-containerized missions)
mux.HandleFunc("POST /claude-update", handleClaudeUpdateHTTP(w, logger))

// New endpoint with event in URL path (used by containerized missions)
mux.HandleFunc("POST /claude-update/{event}", handleClaudeUpdateWithPathEvent(w, logger))
```

**Step 2: Implement the new handler**

```go
func handleClaudeUpdateWithPathEvent(w *Wrapper, logger *slog.Logger) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		event := r.PathValue("event")
		if event == "" {
			http.Error(rw, "missing event in path", http.StatusBadRequest)
			return
		}

		// Read optional body (Claude hook stdin forwarded as-is)
		var body ClaudeUpdateRequest
		if r.ContentLength > 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				// Body might not be valid JSON — that's okay for some hooks
				logger.Debug("could not parse hook body", "event", event, "error", err)
			}
		}

		// Set the event from the URL path
		body.Event = event

		cmd := Command{
			command:          "claude_update",
			event:            body.Event,
			notificationType: body.NotificationType,
			responseCh:       make(chan CommandResponse, 1),
		}

		w.commandCh <- cmd
		resp := <-cmd.responseCh
		writeJSON(rw, resp)
	}
}
```

**Step 3: Run tests**

Run: `go test ./internal/wrapper/... -v`
Expected: PASS

**Step 4: Commit**

```
git add internal/wrapper/socket.go
git commit -m "Add URL-path-based hook endpoint for containerized missions"
```

---

Task 6: Wrapper Devcontainer Lifecycle
---------------------------------------

**Files:**
- Create: `internal/wrapper/devcontainer.go`
- Modify: `internal/wrapper/wrapper.go`

**Context:** The wrapper needs to:
1. Detect if the repo has a devcontainer.json
2. Generate the overlay config
3. Run `devcontainer up` to start the container
4. Spawn Claude via `devcontainer exec` instead of direct `claude`
5. Stop the container on wrapper shutdown

**Step 1: Create devcontainer lifecycle helpers**

```go
// internal/wrapper/devcontainer.go
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
	// repoConfigPath is the path to the repo's devcontainer.json
	repoConfigPath string

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
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(w.agencDirpath, w.missionID)
	wrapperSocketPath := config.GetMissionSocketFilepath(w.agencDirpath, w.missionID)

	oauthToken, err := config.ReadOAuthToken(w.agencDirpath)
	if err != nil {
		return nil, fmt.Errorf("reading OAuth token: %w", err)
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
		repoConfigPath:   repoConfigPath,
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
```

**Step 2: Modify wrapper.go to integrate devcontainer lifecycle**

Add a `devcontainer *devcontainerState` field to the `Wrapper` struct.

In `setupRun()`, after the wrapper socket server is started but before Claude is spawned:

```go
// Detect and setup devcontainer (after socket server is started so wrapper.sock exists)
dcState, err := w.detectAndSetupDevcontainer()
if err != nil {
	w.logger.Error("devcontainer setup failed", "error", err)
	return nil, nil, fmt.Errorf("devcontainer setup: %w", err)
}
w.devcontainer = dcState

if dcState != nil {
	w.logger.Info("starting devcontainer", "config", dcState.mergedConfigPath)
	if err := devcontainerUp(dcState); err != nil {
		w.logger.Error("devcontainer up failed", "error", err)
		return nil, nil, fmt.Errorf("devcontainer up: %w", err)
	}
}
```

In the shutdown/cleanup path, stop the container:

```go
if w.devcontainer != nil {
	w.logger.Info("stopping devcontainer")
	if err := devcontainerStop(w.devcontainer); err != nil {
		w.logger.Error("devcontainer stop failed", "error", err)
	}
}
```

**Step 3: Modify Claude spawn to use devcontainer exec**

In `internal/mission/mission.go`, add a new function:

```go
// BuildDevcontainerClaudeCmd builds an exec.Cmd that runs Claude inside
// a devcontainer via `devcontainer exec`.
func BuildDevcontainerClaudeCmd(
	agencDirpath string,
	missionID string,
	agentDirpath string,
	mergedConfigPath string,
	model string,
	extraClaudeArgs []string,
	claudeArgs []string,
) (*exec.Cmd, error) {
	// Build the claude command args (same logic as BuildClaudeCmd)
	claudeBin := "claude"
	allArgs := make([]string, 0)
	if model != "" {
		allArgs = append(allArgs, "--model", model)
	}
	allArgs = append(allArgs, extraClaudeArgs...)
	allArgs = append(allArgs, claudeArgs...)

	// Wrap in devcontainer exec
	devcontainerArgs := []string{
		"exec",
		"--workspace-folder", agentDirpath,
		"--config", mergedConfigPath,
		"--",
		claudeBin,
	}
	devcontainerArgs = append(devcontainerArgs, allArgs...)

	cmd := exec.Command("devcontainer", devcontainerArgs...)
	// Note: env vars like CLAUDE_CODE_OAUTH_TOKEN are set via containerEnv
	// in the devcontainer config, not on this exec.Cmd
	return cmd, nil
}
```

In the wrapper's spawn methods, branch on `w.devcontainer != nil`:

```go
func (w *Wrapper) spawnClaude(isResume bool, prompt string) error {
	// Regenerate claude-config before every spawn
	containerized := w.devcontainer != nil
	trustedMcpServers := w.loadTrustedMcpServers()
	if err := claudeconfig.BuildMissionConfigDir(
		w.agencDirpath, w.missionID, trustedMcpServers, containerized,
	); err != nil {
		return fmt.Errorf("regenerating claude-config: %w", err)
	}

	if containerized {
		cmd, err := mission.BuildDevcontainerClaudeCmd(
			w.agencDirpath, w.missionID, w.agentDirpath,
			w.devcontainer.mergedConfigPath,
			w.defaultModel, w.claudeArgs, buildClaudeArgs(isResume, prompt),
		)
		// ... set stdin/stdout/stderr, start ...
	} else {
		// existing spawn logic
	}
}
```

**Step 4: Run build and tests**

Run: `make check` (with `dangerouslyDisableSandbox: true`)
Expected: Build succeeds, tests pass

**Step 5: Commit**

```
git add internal/wrapper/devcontainer.go internal/wrapper/wrapper.go internal/mission/mission.go
git commit -m "Integrate devcontainer lifecycle into wrapper: up, exec, stop"
```

---

Task 7: Mission Removal Container Cleanup
-------------------------------------------

**Files:**
- Modify: `internal/server/missions.go`

**Context:** `handleDeleteMission()` at lines 516-563 needs to stop and remove the container before deleting the mission directory. The container is identified by the workspace folder passed to the devcontainer CLI.

**Step 1: Add container cleanup to handleDeleteMission**

After stopping the wrapper (line ~536) and before removing the mission directory (line ~551), add:

```go
// Stop and remove devcontainer if the mission had one
agentDirpath := config.GetMissionAgentDirpath(s.agencDirpath, missionID)
if _, found := devcontainer.DetectDevcontainer(agentDirpath); found {
	missionDirpath := config.GetMissionDirpath(s.agencDirpath, missionID)
	mergedConfigPath := filepath.Join(missionDirpath, "devcontainer.json")

	// Stop container (wrapper shutdown should have done this, but be safe)
	stopCmd := exec.Command("devcontainer", "stop",
		"--workspace-folder", agentDirpath,
		"--config", mergedConfigPath,
	)
	stopCmd.Run() // Ignore error — container may already be stopped

	// TODO: Research how to remove the container via devcontainer CLI
	// May need to use `docker rm` directly with the container ID/name
}
```

**Step 2: Run tests**

Run: `go test ./internal/server/... -v`
Expected: PASS

**Step 3: Commit**

```
git add internal/server/missions.go
git commit -m "Add devcontainer cleanup to mission removal"
```

---

Task 8: Mission Rebuild Command
---------------------------------

**Files:**
- Create: `cmd/mission_rebuild.go`
- Modify: `internal/wrapper/socket.go` (add /rebuild endpoint)
- Modify: `internal/wrapper/client.go` (add Rebuild method)

**Context:** New CLI command `agenc mission rebuild <id>` that sends a rebuild request to the wrapper, which tears down and rebuilds the container, then restarts Claude.

**Step 1: Add /rebuild endpoint to wrapper socket**

In `internal/wrapper/socket.go`, add new route:

```go
mux.HandleFunc("POST /rebuild", handleRebuild(w, logger))
```

```go
func handleRebuild(w *Wrapper, logger *slog.Logger) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		cmd := Command{
			command:    "rebuild",
			responseCh: make(chan CommandResponse, 1),
		}
		w.commandCh <- cmd
		resp := <-cmd.responseCh
		writeJSON(rw, resp)
	}
}
```

**Step 2: Handle rebuild command in wrapper event loop**

In the wrapper's main event loop (Run method), add a case for the "rebuild" command:

```go
case "rebuild":
	if w.devcontainer == nil {
		cmd.responseCh <- CommandResponse{Status: "error", Error: "mission is not containerized"}
	} else {
		// Kill current Claude process
		w.killClaude()
		// Rebuild container
		if err := devcontainerRebuild(w.devcontainer); err != nil {
			cmd.responseCh <- CommandResponse{Status: "error", Error: err.Error()}
		} else {
			// Respawn Claude
			cmd.responseCh <- CommandResponse{Status: "ok"}
			w.state = stateRestarting
			// Trigger spawn (same as restart flow)
		}
	}
```

**Step 3: Add Rebuild method to wrapper client**

In `internal/wrapper/client.go`:

```go
func (c *WrapperClient) Rebuild() (*CommandResponse, error) {
	resp, err := c.post("/rebuild", nil)
	if err != nil {
		return nil, err
	}
	var result CommandResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	return &result, nil
}
```

**Step 4: Create CLI command**

```go
// cmd/mission_rebuild.go
package cmd

// Cobra command: agenc mission rebuild <id>
// Connects to the wrapper socket for the given mission
// Sends POST /rebuild
// Displays result
```

Follow the pattern of existing mission commands (e.g., `mission_send_claude_update.go`) for the Cobra command structure.

**Step 5: Run build**

Run: `make build` (with `dangerouslyDisableSandbox: true`)
Expected: Build succeeds

**Step 6: Commit**

```
git add cmd/mission_rebuild.go internal/wrapper/socket.go internal/wrapper/client.go
git commit -m "Add mission rebuild command for devcontainer recreation"
```

---

Task 9: Tmux Palette Entry for Rebuild Container
--------------------------------------------------

**Files:**
- Modify: Tmux palette configuration (where palette commands are registered)

**Context:** Add a "Rebuild Container" entry to the tmux command palette. It should only appear for containerized missions (missions whose repo has a devcontainer.json).

**Step 1: Determine palette registration mechanism**

Check `cmd/tmux_palette.go` and the config system to understand how palette commands are registered. The new entry should:
- Call `agenc mission rebuild $AGENC_CALLING_MISSION_UUID`
- Only appear when `AGENC_CALLING_MISSION_UUID` is set (mission-scoped)
- Ideally, only appear for containerized missions (may require the palette to check for devcontainer.json existence)

**Step 2: Add the palette entry**

Follow the pattern of existing palette commands. The entry should be titled "Rebuild Container" with a description like "Rebuild the devcontainer and restart Claude".

**Step 3: Test manually**

Verify the palette entry appears in a containerized mission's tmux command palette and not in non-containerized missions.

**Step 4: Commit**

```
git add <modified palette files>
git commit -m "Add Rebuild Container to tmux command palette"
```

---

Task 10: Ensure Host Project Directory Exists
-----------------------------------------------

**Files:**
- Modify: `internal/devcontainer/overlay.go`

**Context:** The session bind mount requires `~/.claude/projects/<host-encoded>/` to exist on the host before `devcontainer up`. If this directory doesn't exist, the bind mount fails.

**Step 1: Write failing test**

```go
func TestGenerateOverlay_CreatesHostProjectDir(t *testing.T) {
	// Setup repo with devcontainer
	repoDir := t.TempDir()
	devcontainerDir := filepath.Join(repoDir, ".devcontainer")
	os.MkdirAll(devcontainerDir, 0755)
	os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"),
		[]byte(`{"image":"ubuntu"}`), 0644)

	// Use a temp dir as the fake ~/.claude/projects/ base
	fakeHome := t.TempDir()
	// ... (this test needs the overlay to create the host project dir)
}
```

**Step 2: Implement**

In `GenerateOverlay()`, after computing the session mount, ensure the host directory exists:

```go
// Ensure host project directory exists for the bind mount
hostProjectDir := filepath.Join(homeDir, ".claude", "projects", sessionMount.HostProjectDirName)
if err := os.MkdirAll(hostProjectDir, 0700); err != nil {
	return fmt.Errorf("creating host project directory: %w", err)
}
```

**Step 3: Commit**

```
git add internal/devcontainer/overlay.go
git commit -m "Ensure host project directory exists before devcontainer up"
```

---

Task 11: Research devcontainer CLI Override Flags
--------------------------------------------------

**Files:**
- None (research task)

**Context:** The design doc notes a TODO to research whether the devcontainer CLI supports `--override-config` or similar flags that would allow passing additional mounts/env without merging JSON files.

**Step 1: Research**

Run: `devcontainer up --help` and `devcontainer --help` to check available flags.

Check the devcontainer CLI source/docs for:
- `--override-config` — merge additional config
- `--mount` — add mounts at CLI level
- `--remote-env` — add env vars at CLI level
- Any other mechanism to avoid JSON merge

**Step 2: Document findings**

If a simpler approach exists, update the overlay generation to use it and simplify the merge logic. If not, confirm the merge approach is necessary.

**Step 3: Commit findings**

Update the design doc with the research results.

---

Implementation Order
--------------------

Tasks should be executed in this order due to dependencies:

```
Task 1:  Devcontainer Detection           (no deps)
Task 2:  Project Path Encoding            (no deps)
Task 3:  Overlay Generation               (depends on 1, 2)
Task 10: Host Project Dir Creation         (part of 3)
Task 4:  Claude-Config Container Mode      (no deps)
Task 5:  Wrapper Socket URL-Path Endpoint  (no deps)
Task 6:  Wrapper Devcontainer Lifecycle    (depends on 1, 3, 4, 5)
Task 7:  Mission Removal Cleanup           (depends on 1)
Task 8:  Mission Rebuild Command           (depends on 6)
Task 9:  Tmux Palette Entry                (depends on 8)
Task 11: CLI Override Research             (can run anytime, may simplify 3)
```

Tasks 1, 2, 4, 5, and 11 are independent and can be parallelized.
