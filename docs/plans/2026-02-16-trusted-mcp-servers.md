# trusted-mcp-servers Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `trustedMcpServers` field to `repoConfig` in `config.yml` so missions skip the Claude Code MCP server consent prompt.

**Architecture:** Claude Code skips the MCP trust dialog when a project's `.claude.json` entry contains `enabledMcpjsonServers` and `disabledMcpjsonServers` fields. We add a `TrustedMcpServers` type to `AgencConfig.RepoConfig`, thread it through `BuildMissionConfigDir` → `copyAndPatchClaudeJSON`, and add a `--trusted-mcp-servers` flag to `agenc config repoConfig set`.

**Tech Stack:** Go, `github.com/goccy/go-yaml` (already used in codebase), Cobra CLI (already used), standard `encoding/json`

---

### Task 1: Add `TrustedMcpServers` type and `RepoConfig` field

**Files:**
- Modify: `internal/config/agenc_config.go` (around line 315 — the `RepoConfig` struct)

**Step 1: Write the failing tests**

Add to `internal/config/agenc_config_test.go`:

```go
func TestTrustedMcpServers_UnmarshalYAML_All(t *testing.T) {
	type wrapper struct {
		Trust *TrustedMcpServers `yaml:"trust"`
	}
	input := `trust: all`
	var w wrapper
	if err := yaml.Unmarshal([]byte(input), &w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Trust == nil {
		t.Fatal("expected non-nil TrustedMcpServers")
	}
	if !w.Trust.All {
		t.Error("expected All=true")
	}
	if len(w.Trust.List) != 0 {
		t.Errorf("expected empty List, got %v", w.Trust.List)
	}
}

func TestTrustedMcpServers_UnmarshalYAML_List(t *testing.T) {
	type wrapper struct {
		Trust *TrustedMcpServers `yaml:"trust"`
	}
	input := "trust:\n  - github\n  - sentry\n"
	var w wrapper
	if err := yaml.Unmarshal([]byte(input), &w); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Trust == nil {
		t.Fatal("expected non-nil TrustedMcpServers")
	}
	if w.Trust.All {
		t.Error("expected All=false")
	}
	if len(w.Trust.List) != 2 || w.Trust.List[0] != "github" || w.Trust.List[1] != "sentry" {
		t.Errorf("expected [github sentry], got %v", w.Trust.List)
	}
}

func TestTrustedMcpServers_UnmarshalYAML_EmptyList(t *testing.T) {
	type wrapper struct {
		Trust *TrustedMcpServers `yaml:"trust"`
	}
	input := "trust: []\n"
	var w wrapper
	if err := yaml.Unmarshal([]byte(input), &w); err == nil {
		t.Fatal("expected error for empty list, got nil")
	}
}

func TestTrustedMcpServers_UnmarshalYAML_InvalidString(t *testing.T) {
	type wrapper struct {
		Trust *TrustedMcpServers `yaml:"trust"`
	}
	input := `trust: none`
	var w wrapper
	if err := yaml.Unmarshal([]byte(input), &w); err == nil {
		t.Fatal("expected error for invalid string, got nil")
	}
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/config/... -run TestTrustedMcpServers -v
```

Expected: FAIL with "undefined: TrustedMcpServers"

**Step 3: Add the `TrustedMcpServers` type**

In `internal/config/agenc_config.go`, add after the `RepoConfig` struct (around line 321):

```go
// TrustedMcpServers configures MCP server trust for a repository.
// Supports two formats: "all" (trust every server in .mcp.json) or
// a list of named servers to trust.
type TrustedMcpServers struct {
	All  bool     // true when "all" is specified
	List []string // populated when a list of server names is specified
}

// UnmarshalYAML implements yaml.Unmarshaler to handle "all" (string)
// or a list of server names.
func (t *TrustedMcpServers) UnmarshalYAML(unmarshal func(interface{}) error) error {
	// Try string first ("all")
	var str string
	if err := unmarshal(&str); err == nil {
		if str == "all" {
			t.All = true
			return nil
		}
		return fmt.Errorf("trustedMcpServers: invalid value %q; must be \"all\" or a list of server names", str)
	}

	// Try list of strings
	var list []string
	if err := unmarshal(&list); err == nil {
		if len(list) == 0 {
			return fmt.Errorf("trustedMcpServers: empty list is not valid; use \"all\" to trust all servers, or list at least one server name")
		}
		t.List = list
		return nil
	}

	return fmt.Errorf("trustedMcpServers: must be \"all\" or a list of server names")
}
```

Also update the `RepoConfig` struct to add the new field:

```go
type RepoConfig struct {
	AlwaysSynced      bool               `yaml:"alwaysSynced,omitempty"`
	WindowTitle       string             `yaml:"windowTitle,omitempty"`
	TrustedMcpServers *TrustedMcpServers `yaml:"trustedMcpServers,omitempty"`
}
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/config/... -run TestTrustedMcpServers -v
```

Expected: PASS (4 tests)

**Step 5: Commit**

```bash
git add internal/config/agenc_config.go internal/config/agenc_config_test.go
git commit -m "Add TrustedMcpServers type and RepoConfig field"
git pull --rebase
git push
```

---

### Task 2: Validate `trustedMcpServers` in `ReadAgencConfig`

**Files:**
- Modify: `internal/config/agenc_config.go` (the `ReadAgencConfig` function, around line 449)

The YAML unmarshaler already validates during parse, so `ReadAgencConfig` will naturally return an error on invalid `trustedMcpServers` values. No additional validation code is needed — the unmarshal error will propagate.

**Step 1: Write a round-trip test**

Add to `internal/config/agenc_config_test.go`:

```go
func TestReadAgencConfig_TrustedMcpServers_All(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	content := `repoConfig:
  github.com/owner/repo:
    trustedMcpServers: all
`
	if err := os.WriteFile(filepath.Join(configDirpath, ConfigFilename), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	rc, ok := cfg.RepoConfigs["github.com/owner/repo"]
	if !ok {
		t.Fatal("repo not found in config")
	}
	if rc.TrustedMcpServers == nil {
		t.Fatal("expected non-nil TrustedMcpServers")
	}
	if !rc.TrustedMcpServers.All {
		t.Error("expected All=true")
	}
}

func TestReadAgencConfig_TrustedMcpServers_List(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	content := "repoConfig:\n  github.com/owner/repo:\n    trustedMcpServers:\n      - github\n      - sentry\n"
	if err := os.WriteFile(filepath.Join(configDirpath, ConfigFilename), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	rc := cfg.RepoConfigs["github.com/owner/repo"]
	if rc.TrustedMcpServers == nil {
		t.Fatal("expected non-nil TrustedMcpServers")
	}
	if rc.TrustedMcpServers.All {
		t.Error("expected All=false")
	}
	if len(rc.TrustedMcpServers.List) != 2 {
		t.Errorf("expected 2 servers, got %d", len(rc.TrustedMcpServers.List))
	}
}

func TestReadAgencConfig_TrustedMcpServers_Invalid(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	content := `repoConfig:
  github.com/owner/repo:
    trustedMcpServers: none
`
	if err := os.WriteFile(filepath.Join(configDirpath, ConfigFilename), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for invalid trustedMcpServers, got nil")
	}
}
```

**Step 2: Run tests to verify they pass**

```bash
go test ./internal/config/... -run TestReadAgencConfig_TrustedMcpServers -v
```

Expected: PASS (3 tests)

**Step 3: Commit**

```bash
git add internal/config/agenc_config_test.go
git commit -m "Add ReadAgencConfig tests for trustedMcpServers"
git pull --rebase
git push
```

---

### Task 3: Update `copyAndPatchClaudeJSON` to inject MCP consent fields

**Files:**
- Modify: `internal/claudeconfig/build.go` (the `copyAndPatchClaudeJSON` function, around line 461)

**Step 1: Write the failing tests**

Add to `internal/claudeconfig/build_test.go`:

```go
func TestCopyAndPatchClaudeJSON_NoTrust(t *testing.T) {
	// Setup: create a minimal .claude.json as the "source"
	homeDir := setupFakeHome(t)
	claudeJSONPath := filepath.Join(homeDir, ".claude", ".claude.json")
	if err := os.MkdirAll(filepath.Dir(claudeJSONPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeJSONPath, []byte(`{"projects":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	agentDir := "/fake/agent/dir"

	if err := copyAndPatchClaudeJSON(destDir, agentDir, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := readClaudeJSONProjects(t, destDir, agentDir)
	if _, ok := result["enabledMcpjsonServers"]; ok {
		t.Error("expected enabledMcpjsonServers to be absent when trust is nil")
	}
	if _, ok := result["disabledMcpjsonServers"]; ok {
		t.Error("expected disabledMcpjsonServers to be absent when trust is nil")
	}
	if result["hasTrustDialogAccepted"] != true {
		t.Error("expected hasTrustDialogAccepted=true")
	}
}

func TestCopyAndPatchClaudeJSON_TrustAll(t *testing.T) {
	homeDir := setupFakeHome(t)
	claudeJSONPath := filepath.Join(homeDir, ".claude", ".claude.json")
	if err := os.MkdirAll(filepath.Dir(claudeJSONPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeJSONPath, []byte(`{"projects":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	agentDir := "/fake/agent/dir"
	trust := &config.TrustedMcpServers{All: true}

	if err := copyAndPatchClaudeJSON(destDir, agentDir, trust); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := readClaudeJSONProjects(t, destDir, agentDir)
	enabled, ok := result["enabledMcpjsonServers"]
	if !ok {
		t.Fatal("expected enabledMcpjsonServers to be present")
	}
	if arr, ok := enabled.([]interface{}); !ok || len(arr) != 0 {
		t.Errorf("expected enabledMcpjsonServers=[], got %v", enabled)
	}
	disabled, ok := result["disabledMcpjsonServers"]
	if !ok {
		t.Fatal("expected disabledMcpjsonServers to be present")
	}
	if arr, ok := disabled.([]interface{}); !ok || len(arr) != 0 {
		t.Errorf("expected disabledMcpjsonServers=[], got %v", disabled)
	}
}

func TestCopyAndPatchClaudeJSON_TrustList(t *testing.T) {
	homeDir := setupFakeHome(t)
	claudeJSONPath := filepath.Join(homeDir, ".claude", ".claude.json")
	if err := os.MkdirAll(filepath.Dir(claudeJSONPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(claudeJSONPath, []byte(`{"projects":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	destDir := t.TempDir()
	agentDir := "/fake/agent/dir"
	trust := &config.TrustedMcpServers{List: []string{"github", "sentry"}}

	if err := copyAndPatchClaudeJSON(destDir, agentDir, trust); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := readClaudeJSONProjects(t, destDir, agentDir)
	enabled, ok := result["enabledMcpjsonServers"]
	if !ok {
		t.Fatal("expected enabledMcpjsonServers to be present")
	}
	arr, ok := enabled.([]interface{})
	if !ok || len(arr) != 2 || arr[0] != "github" || arr[1] != "sentry" {
		t.Errorf("expected [github sentry], got %v", enabled)
	}
	disabled, ok := result["disabledMcpjsonServers"]
	if !ok {
		t.Fatal("expected disabledMcpjsonServers to be present")
	}
	if disabledArr, ok := disabled.([]interface{}); !ok || len(disabledArr) != 0 {
		t.Errorf("expected disabledMcpjsonServers=[], got %v", disabled)
	}
}

// setupFakeHome creates a temp dir and sets HOME so os.UserHomeDir() returns it.
func setupFakeHome(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	return homeDir
}

// readClaudeJSONProjects reads the project entry from the output .claude.json.
func readClaudeJSONProjects(t *testing.T, destDir string, agentDir string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(destDir, ".claude.json"))
	if err != nil {
		t.Fatalf("failed to read .claude.json: %v", err)
	}
	var claudeJSON map[string]interface{}
	if err := json.Unmarshal(data, &claudeJSON); err != nil {
		t.Fatalf("failed to parse .claude.json: %v", err)
	}
	projects, ok := claudeJSON["projects"].(map[string]interface{})
	if !ok {
		t.Fatal("projects key missing or wrong type")
	}
	entry, ok := projects[agentDir].(map[string]interface{})
	if !ok {
		t.Fatalf("agent dir entry missing in projects")
	}
	return entry
}
```

**Step 2: Run tests to verify they fail**

```bash
go test ./internal/claudeconfig/... -run TestCopyAndPatchClaudeJSON_ -v
```

Expected: FAIL — `copyAndPatchClaudeJSON` doesn't accept a trust parameter yet

**Step 3: Update `copyAndPatchClaudeJSON` signature and logic**

In `internal/claudeconfig/build.go`, change the function signature and trust entry building:

```go
func copyAndPatchClaudeJSON(claudeConfigDirpath string, missionAgentDirpath string, trustedMcpServers *config.TrustedMcpServers) error {
```

Replace the trust entry construction (around line 505):

```go
// Build trust entry for the mission agent directory
trustEntry := map[string]interface{}{
    "hasTrustDialogAccepted": true,
}
if trustedMcpServers != nil {
    if trustedMcpServers.All {
        trustEntry["enabledMcpjsonServers"] = []string{}
        trustEntry["disabledMcpjsonServers"] = []string{}
    } else {
        trustEntry["enabledMcpjsonServers"] = trustedMcpServers.List
        trustEntry["disabledMcpjsonServers"] = []string{}
    }
}
trustEntryData, err := json.Marshal(trustEntry)
if err != nil {
    return stacktrace.Propagate(err, "failed to marshal trust entry")
}
projects[missionAgentDirpath] = json.RawMessage(trustEntryData)
```

Also update the internal call site in `BuildMissionConfigDir` (line 91) to pass `nil` for now (callers updated in Task 4):

```go
if err := copyAndPatchClaudeJSON(claudeConfigDirpath, missionAgentDirpath, nil); err != nil {
```

**Step 4: Run tests to verify they pass**

```bash
go test ./internal/claudeconfig/... -run TestCopyAndPatchClaudeJSON_ -v
```

Expected: PASS (3 tests)

**Step 5: Run full test suite to catch regressions**

```bash
go test ./...
```

Expected: PASS

**Step 6: Commit**

```bash
git add internal/claudeconfig/build.go internal/claudeconfig/build_test.go
git commit -m "Update copyAndPatchClaudeJSON to inject MCP consent fields"
git pull --rebase
git push
```

---

### Task 4: Thread `TrustedMcpServers` through `BuildMissionConfigDir` and callers

**Files:**
- Modify: `internal/claudeconfig/build.go` (`BuildMissionConfigDir` signature)
- Modify: `internal/mission/mission.go` (`CreateMissionDir` — reads AgencConfig, passes trust)
- Modify: `cmd/mission_update_config.go` (`updateMissionConfig` — reads AgencConfig, passes trust)

**Step 1: Update `BuildMissionConfigDir` signature**

In `internal/claudeconfig/build.go`, change line 40:

```go
func BuildMissionConfigDir(agencDirpath string, missionID string, trustedMcpServers *config.TrustedMcpServers) error {
```

And update the `copyAndPatchClaudeJSON` call inside it (around line 91) to pass the parameter:

```go
if err := copyAndPatchClaudeJSON(claudeConfigDirpath, missionAgentDirpath, trustedMcpServers); err != nil {
```

**Step 2: Update `CreateMissionDir` in `internal/mission/mission.go`**

`CreateMissionDir` receives `gitRepoName`. Update it to read AgencConfig and look up the trust config:

```go
func CreateMissionDir(agencDirpath string, missionID string, gitRepoName string, gitRepoSource string) (string, error) {
    // ... existing dir/repo setup code unchanged ...

    // Look up MCP trust config for this repo
    var trustedMcpServers *config.TrustedMcpServers
    if gitRepoName != "" {
        cfg, _, err := config.ReadAgencConfig(agencDirpath)
        if err == nil {
            if rc, ok := cfg.GetRepoConfig(gitRepoName); ok {
                trustedMcpServers = rc.TrustedMcpServers
            }
        }
    }

    // Build per-mission claude config directory from shadow repo
    if err := claudeconfig.BuildMissionConfigDir(agencDirpath, missionID, trustedMcpServers); err != nil {
        return "", stacktrace.Propagate(err, "failed to build per-mission claude config directory")
    }

    return missionDirpath, nil
}
```

**Step 3: Update `updateMissionConfig` in `cmd/mission_update_config.go`**

`updateMissionConfig` already fetches the mission record (which has `GitRepo`). Update it:

```go
func updateMissionConfig(db *database.DB, missionID string, newCommitHash string) error {
    missionRecord, err := db.GetMission(missionID)
    // ... existing record-fetch and early-exit code unchanged ...

    // Look up MCP trust config for this repo
    var trustedMcpServers *config.TrustedMcpServers
    if missionRecord.GitRepo != "" {
        cfg, _, cfgErr := config.ReadAgencConfig(agencDirpath)
        if cfgErr == nil {
            if rc, ok := cfg.GetRepoConfig(missionRecord.GitRepo); ok {
                trustedMcpServers = rc.TrustedMcpServers
            }
        }
    }

    // Rebuild per-mission config directory from shadow repo
    if err := claudeconfig.BuildMissionConfigDir(agencDirpath, missionID, trustedMcpServers); err != nil {
        return stacktrace.Propagate(err, "failed to rebuild config for mission '%s'", missionID)
    }
    // ... rest unchanged ...
}
```

**Step 4: Run the full test suite**

```bash
go test ./...
```

Expected: PASS (all compile errors resolved, existing tests unaffected)

**Step 5: Build the binary**

```bash
make build
```

Expected: success (use `dangerouslyDisableSandbox: true` per CLAUDE.md)

**Step 6: Commit**

```bash
git add internal/claudeconfig/build.go internal/mission/mission.go cmd/mission_update_config.go
git commit -m "Thread TrustedMcpServers through BuildMissionConfigDir and callers"
git pull --rebase
git push
```

---

### Task 5: Add `--trusted-mcp-servers` flag to CLI

**Files:**
- Modify: `cmd/command_str_consts.go` (add flag name constant, around line 110)
- Modify: `cmd/config_repo_config_set.go` (add flag, parsing, and update logic)
- Modify: `cmd/config_repo_config.go` (update Long description)
- Modify: `cmd/config_repo_config_ls.go` (add column to table)

**Step 1: Add flag name constant**

In `cmd/command_str_consts.go`, add alongside the existing constants (around line 110):

```go
repoConfigTrustedMcpServersFlagName = "trusted-mcp-servers"
```

**Step 2: Update `config_repo_config_set.go`**

Add the flag in `init()`:

```go
configRepoConfigSetCmd.Flags().String(repoConfigTrustedMcpServersFlagName, "", "MCP server trust: \"all\", comma-separated server names, or \"\" to clear")
```

Update the "at least one flag" guard to include the new flag:

```go
trustedChanged := cmd.Flags().Changed(repoConfigTrustedMcpServersFlagName)
if !alwaysSyncedChanged && !windowTitleChanged && !trustedChanged {
    return stacktrace.NewError("at least one of --%s, --%s, or --%s must be provided",
        repoConfigAlwaysSyncedFlagName, repoConfigWindowTitleFlagName, repoConfigTrustedMcpServersFlagName)
}
```

Add handling for the new flag (after the existing `windowTitleChanged` block):

```go
if trustedChanged {
    raw, err := cmd.Flags().GetString(repoConfigTrustedMcpServersFlagName)
    if err != nil {
        return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigTrustedMcpServersFlagName)
    }
    if raw == "" {
        rc.TrustedMcpServers = nil
    } else if raw == "all" {
        rc.TrustedMcpServers = &config.TrustedMcpServers{All: true}
    } else {
        parts := strings.Split(raw, ",")
        servers := make([]string, 0, len(parts))
        for _, p := range parts {
            if s := strings.TrimSpace(p); s != "" {
                servers = append(servers, s)
            }
        }
        if len(servers) == 0 {
            return stacktrace.NewError("--%s: no valid server names found in %q", repoConfigTrustedMcpServersFlagName, raw)
        }
        rc.TrustedMcpServers = &config.TrustedMcpServers{List: servers}
    }
}
```

Add `"strings"` to the import block if not already present.

**Step 3: Update `config_repo_config_ls.go` to show `TRUSTED MCP SERVERS`**

Add a helper to format the field, and update the table:

```go
func formatTrustedMcpServers(t *config.TrustedMcpServers) string {
    if t == nil {
        return "--"
    }
    if t.All {
        return "all"
    }
    return strings.Join(t.List, ", ")
}
```

Update the table creation:

```go
tbl := tableprinter.NewTable("REPO", "ALWAYS SYNCED", "WINDOW TITLE", "TRUSTED MCP SERVERS")
for _, name := range repoNames {
    rc := cfg.RepoConfigs[name]
    // ...
    tbl.AddRow(displayGitRepo(name), synced, windowTitle, formatTrustedMcpServers(rc.TrustedMcpServers))
}
```

**Step 4: Update `config_repo_config.go` Long description**

Add `trustedMcpServers` to the description:

```go
Long: `Manage per-repo configuration in config.yml.

Each repo is identified by its canonical name (github.com/owner/repo) and
supports these optional settings:

  alwaysSynced        - daemon keeps the repo continuously fetched (every 60s)
  windowTitle         - custom tmux window name for missions using this repo
  trustedMcpServers   - pre-approve MCP servers to skip the consent prompt

Example config.yml:

  repoConfig:
    github.com/owner/repo:
      alwaysSynced: true
      windowTitle: "my-repo"
      trustedMcpServers: all
`,
```

**Step 5: Build to verify compilation**

```bash
make build
```

Expected: success

**Step 6: Commit**

```bash
git add cmd/command_str_consts.go cmd/config_repo_config_set.go cmd/config_repo_config_ls.go cmd/config_repo_config.go
git commit -m "Add --trusted-mcp-servers flag to config repoConfig set"
git pull --rebase
git push
```

---

### Task 6: Update documentation

**Files:**
- Modify: `docs/configuration.md` (add `trustedMcpServers` to the `repoConfig` example)
- Modify: `docs/system-architecture.md` (update `internal/config/` package description)

**Step 1: Update `docs/configuration.md`**

In the `repoConfig` example (around line 19), add the new field as a comment:

```yaml
repoConfig:
  github.com/owner/repo:
    alwaysSynced: true                # daemon fetches every 60s (optional, default: false)
    windowTitle: "my-repo"            # custom tmux window name (optional)
    trustedMcpServers: all            # pre-approve MCP servers: "all" or list of names (optional)

  github.com/other/repo:
    trustedMcpServers:                # trust specific servers only
      - github
      - sentry
```

**Step 2: Update `docs/system-architecture.md`**

In the `internal/config/` package description (around line 263), add to the `agenc_config.go` bullet:

> `RepoConfig` struct (per-repo settings: `alwaysSynced`, `windowTitle`, `trustedMcpServers`), `TrustedMcpServers` struct (MCP server trust config with custom YAML unmarshaling)

**Step 3: Commit**

```bash
git add docs/configuration.md docs/system-architecture.md
git commit -m "Document trustedMcpServers in configuration and architecture docs"
git pull --rebase
git push
```

---

### Task 7: Final verification

**Step 1: Run the full test suite**

```bash
go test ./...
```

Expected: PASS

**Step 2: Build**

```bash
make build
```

Expected: success

**Step 3: Smoke test the CLI**

```bash
./agenc config repoConfig set github.com/owner/repo --trusted-mcp-servers all
./agenc config repoConfig ls
./agenc config repoConfig set github.com/owner/repo --trusted-mcp-servers "github,sentry"
./agenc config repoConfig ls
./agenc config repoConfig set github.com/owner/repo --trusted-mcp-servers ""
./agenc config repoConfig ls
```

Expected: commands succeed without error; `ls` shows the updated values.
