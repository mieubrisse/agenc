# defaultModel Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a `defaultModel` config key (top-level and per-repo) that passes `--model <value>` to Claude CLI when spawning missions.

**Architecture:** Add `DefaultModel string` to both `AgencConfig` and `RepoConfig`. Resolution: repo > global > empty. The wrapper resolves the model at startup and passes it through to `BuildClaudeCmd`, which prepends `--model <value>` to args when non-empty.

**Tech Stack:** Go, Cobra CLI, YAML config (goccy/go-yaml)

---

### Task 1: Add DefaultModel to data model and resolution method

**Files:**
- Modify: `internal/config/agenc_config.go:318` (RepoConfig struct)
- Modify: `internal/config/agenc_config.go:366` (AgencConfig struct)

**Step 1: Write the failing test**

Add to `internal/config/agenc_config_test.go`:

```go
func TestGetDefaultModel(t *testing.T) {
	cfg := &AgencConfig{
		DefaultModel: "sonnet",
		RepoConfigs: map[string]RepoConfig{
			"github.com/owner/repo1": {DefaultModel: "opus"},
			"github.com/owner/repo2": {},
		},
	}

	// Repo with override returns repo model
	if got := cfg.GetDefaultModel("github.com/owner/repo1"); got != "opus" {
		t.Errorf("expected 'opus' for repo1, got '%s'", got)
	}

	// Repo without override falls back to global
	if got := cfg.GetDefaultModel("github.com/owner/repo2"); got != "sonnet" {
		t.Errorf("expected 'sonnet' for repo2, got '%s'", got)
	}

	// Unknown repo falls back to global
	if got := cfg.GetDefaultModel("github.com/owner/unknown"); got != "sonnet" {
		t.Errorf("expected 'sonnet' for unknown repo, got '%s'", got)
	}

	// Empty repo name falls back to global
	if got := cfg.GetDefaultModel(""); got != "sonnet" {
		t.Errorf("expected 'sonnet' for empty repo, got '%s'", got)
	}

	// No global, no repo override returns empty
	cfgEmpty := &AgencConfig{}
	if got := cfgEmpty.GetDefaultModel("github.com/owner/repo1"); got != "" {
		t.Errorf("expected '' for unset config, got '%s'", got)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestGetDefaultModel -v`
Expected: FAIL — `GetDefaultModel` method does not exist

**Step 3: Write minimal implementation**

In `internal/config/agenc_config.go`, add `DefaultModel` field to `RepoConfig` (around line 318):

```go
type RepoConfig struct {
	AlwaysSynced      bool               `yaml:"alwaysSynced,omitempty"`
	WindowTitle       string             `yaml:"windowTitle,omitempty"`
	TrustedMcpServers *TrustedMcpServers `yaml:"trustedMcpServers,omitempty"`
	DefaultModel      string             `yaml:"defaultModel,omitempty"`
}
```

Add `DefaultModel` field to `AgencConfig` (around line 366):

```go
type AgencConfig struct {
	RepoConfigs           map[string]RepoConfig           `yaml:"repoConfig,omitempty"`
	Crons                 map[string]CronConfig           `yaml:"crons,omitempty"`
	CronsMaxConcurrent    int                             `yaml:"cronsMaxConcurrent,omitempty"`
	PaletteCommands       map[string]PaletteCommandConfig `yaml:"paletteCommands,omitempty"`
	PaletteTmuxKeybinding string                          `yaml:"paletteTmuxKeybinding,omitempty"`
	TmuxWindowTitle       *TmuxWindowTitleConfig          `yaml:"tmuxWindowTitle,omitempty"`
	DefaultModel          string                          `yaml:"defaultModel,omitempty"`
}
```

Add the resolution method after the existing getter methods (around line 420):

```go
// GetDefaultModel returns the resolved model for a given repo.
// Precedence: repoConfig.defaultModel > config.defaultModel > "" (Claude decides).
func (c *AgencConfig) GetDefaultModel(repoName string) string {
	if repoName != "" {
		if rc, ok := c.RepoConfigs[repoName]; ok && rc.DefaultModel != "" {
			return rc.DefaultModel
		}
	}
	return c.DefaultModel
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestGetDefaultModel -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/agenc_config.go internal/config/agenc_config_test.go
git commit -m "Add DefaultModel to AgencConfig and RepoConfig with resolution method"
```

---

### Task 2: Add YAML round-trip test for defaultModel

**Files:**
- Modify: `internal/config/agenc_config_test.go`

**Step 1: Write the test**

```go
func TestDefaultModel_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &AgencConfig{
		DefaultModel: "sonnet",
		RepoConfigs: map[string]RepoConfig{
			"github.com/owner/repo1": {DefaultModel: "opus"},
			"github.com/owner/repo2": {},
		},
	}

	if err := WriteAgencConfig(tmpDir, cfg, nil); err != nil {
		t.Fatalf("WriteAgencConfig failed: %v", err)
	}

	got, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	if got.DefaultModel != "sonnet" {
		t.Errorf("expected global defaultModel 'sonnet', got '%s'", got.DefaultModel)
	}

	rc1 := got.RepoConfigs["github.com/owner/repo1"]
	if rc1.DefaultModel != "opus" {
		t.Errorf("expected repo1 defaultModel 'opus', got '%s'", rc1.DefaultModel)
	}

	rc2 := got.RepoConfigs["github.com/owner/repo2"]
	if rc2.DefaultModel != "" {
		t.Errorf("expected empty defaultModel for repo2, got '%s'", rc2.DefaultModel)
	}
}

func TestDefaultModel_FromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
defaultModel: haiku
repoConfig:
  github.com/owner/repo1:
    defaultModel: opus
  github.com/owner/repo2:
    alwaysSynced: true
`)

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	if cfg.DefaultModel != "haiku" {
		t.Errorf("expected global defaultModel 'haiku', got '%s'", cfg.DefaultModel)
	}
	if cfg.GetDefaultModel("github.com/owner/repo1") != "opus" {
		t.Errorf("expected repo1 resolved model 'opus', got '%s'", cfg.GetDefaultModel("github.com/owner/repo1"))
	}
	if cfg.GetDefaultModel("github.com/owner/repo2") != "haiku" {
		t.Errorf("expected repo2 to fall back to 'haiku', got '%s'", cfg.GetDefaultModel("github.com/owner/repo2"))
	}
}
```

**Step 2: Run tests to verify they pass**

Run: `go test ./internal/config/ -run TestDefaultModel -v`
Expected: PASS (struct fields and method already exist from Task 1)

**Step 3: Commit**

```bash
git add internal/config/agenc_config_test.go
git commit -m "Add YAML round-trip and parsing tests for defaultModel"
```

---

### Task 3: Wire model into BuildClaudeCmd and spawn functions

**Files:**
- Modify: `internal/mission/mission.go:69` (BuildClaudeCmd signature)
- Modify: `internal/mission/mission.go:129-207` (Spawn function signatures)
- Modify: `internal/wrapper/wrapper.go:105` (NewWrapper — resolve model)
- Modify: `internal/wrapper/wrapper.go:249-296` (spawn calls in Run)
- Modify: `internal/wrapper/wrapper.go:697-708` (buildHeadlessClaudeCmd)

**Step 1: Add `model string` parameter to `BuildClaudeCmd`**

In `internal/mission/mission.go`, change the signature:

```go
func BuildClaudeCmd(agencDirpath string, missionID string, agentDirpath string, model string, claudeArgs []string) (*exec.Cmd, error) {
```

At the start of the function body (after the `claudeBinary` lookup and before constructing `cmd`), prepend the model flag:

```go
	// Prepend --model flag if a default model is configured
	if model != "" {
		claudeArgs = append([]string{"--model", model}, claudeArgs...)
	}
```

**Step 2: Update all Spawn functions to accept and pass `model`**

```go
func SpawnClaude(agencDirpath string, missionID string, agentDirpath string, model string) (*exec.Cmd, error) {
	return SpawnClaudeWithPrompt(agencDirpath, missionID, agentDirpath, model, "")
}

func SpawnClaudeWithPrompt(agencDirpath string, missionID string, agentDirpath string, model string, initialPrompt string) (*exec.Cmd, error) {
	var args []string
	if initialPrompt != "" {
		args = []string{initialPrompt}
	}
	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, model, args)
	// ... rest unchanged
}

func SpawnClaudeResume(agencDirpath string, missionID string, agentDirpath string, model string) (*exec.Cmd, error) {
	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, model, []string{"-c"})
	// ... rest unchanged
}

func SpawnClaudeResumeWithSession(agencDirpath string, missionID string, agentDirpath string, model string, sessionID string) (*exec.Cmd, error) {
	var args []string
	if sessionID != "" {
		args = []string{"-r", sessionID}
	} else {
		args = []string{"-c"}
	}
	cmd, err := BuildClaudeCmd(agencDirpath, missionID, agentDirpath, model, args)
	// ... rest unchanged
}
```

**Step 3: Resolve model in the Wrapper and store it**

In `internal/wrapper/wrapper.go`, add a `defaultModel` field to the `Wrapper` struct (around line 42):

```go
type Wrapper struct {
	agencDirpath   string
	missionID      string
	gitRepoName    string
	windowTitle    string
	initialPrompt  string
	defaultModel   string  // resolved from config: repo > global > ""
	missionDirpath string
	// ... rest unchanged
}
```

In `NewWrapper` (around line 105), after the existing `cfg` read, resolve the model:

```go
func NewWrapper(agencDirpath string, missionID string, gitRepoName string, windowTitle string, initialPrompt string, db *database.DB) *Wrapper {
	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	var titleCfg *config.TmuxWindowTitleConfig
	var defaultModel string
	if err == nil {
		titleCfg = cfg.GetTmuxWindowTitleConfig()
		defaultModel = cfg.GetDefaultModel(gitRepoName)
	} else {
		titleCfg = &config.TmuxWindowTitleConfig{}
	}

	return &Wrapper{
		// ... existing fields ...
		defaultModel:   defaultModel,
		// ... rest unchanged
	}
}
```

**Step 4: Update all spawn calls in wrapper.go**

In `Run()` method (around lines 249-296), update every spawn call to pass `w.defaultModel`:

```go
// Line ~249
w.claudeCmd, err = mission.SpawnClaudeResumeWithSession(w.agencDirpath, w.missionID, w.agentDirpath, w.defaultModel, sessionID)
// Line ~252
w.claudeCmd, err = mission.SpawnClaudeWithPrompt(w.agencDirpath, w.missionID, w.agentDirpath, w.defaultModel, "")
// Line ~255
w.claudeCmd, err = mission.SpawnClaudeWithPrompt(w.agencDirpath, w.missionID, w.agentDirpath, w.defaultModel, w.initialPrompt)
// Line ~294
w.claudeCmd, err = mission.SpawnClaudeResumeWithSession(w.agencDirpath, w.missionID, w.agentDirpath, w.defaultModel, sessionID)
// Line ~296
w.claudeCmd, err = mission.SpawnClaudeWithPrompt(w.agencDirpath, w.missionID, w.agentDirpath, w.defaultModel, "")
```

In `buildHeadlessClaudeCmd` (around line 697):

```go
func (w *Wrapper) buildHeadlessClaudeCmd(isResume bool) (*exec.Cmd, error) {
	var args []string
	if isResume {
		args = []string{"-c", "--print", "-p", w.initialPrompt}
	} else {
		args = []string{"--print", "-p", w.initialPrompt}
	}
	return mission.BuildClaudeCmd(w.agencDirpath, w.missionID, w.agentDirpath, w.defaultModel, args)
}
```

**Step 5: Verify compilation**

Run: `go build ./...`
Expected: PASS (no compilation errors)

**Step 6: Run all existing tests**

Run: `go test ./...`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/mission/mission.go internal/wrapper/wrapper.go
git commit -m "Wire defaultModel through BuildClaudeCmd and wrapper spawn calls"
```

---

### Task 4: Add CLI support — config get/set/unset defaultModel

**Files:**
- Modify: `cmd/config_get.go:13` (supportedConfigKeys)
- Modify: `cmd/config_get.go:67` (getConfigValue switch)
- Modify: `cmd/config_set.go:16-50` (Long help text)
- Modify: `cmd/config_set.go:113` (setConfigValue switch)
- Modify: `cmd/config_unset.go:13-23` (Long help text)
- Modify: `cmd/config_unset.go:65` (unsetConfigValue switch)

**Step 1: Add "defaultModel" to supportedConfigKeys**

In `cmd/config_get.go` (line 13):

```go
var supportedConfigKeys = []string{
	"claudeCodeOAuthToken",
	"defaultModel",
	"paletteTmuxKeybinding",
	"tmuxWindowTitle.busyBackgroundColor",
	"tmuxWindowTitle.busyForegroundColor",
	"tmuxWindowTitle.attentionBackgroundColor",
	"tmuxWindowTitle.attentionForegroundColor",
}
```

**Step 2: Add defaultModel to getConfigValue switch**

In `cmd/config_get.go`, add case in `getConfigValue` (around line 78):

```go
	case "defaultModel":
		if cfg.DefaultModel == "" {
			return "unset", nil
		}
		return cfg.DefaultModel, nil
```

**Step 3: Add defaultModel to setConfigValue switch**

In `cmd/config_set.go`, add case in `setConfigValue` (around line 115):

```go
	case "defaultModel":
		cfg.DefaultModel = value
		return nil
```

**Step 4: Add defaultModel to unsetConfigValue switch**

In `cmd/config_unset.go`, add case in `unsetConfigValue` (around line 67):

```go
	case "defaultModel":
		cfg.DefaultModel = ""
		return nil
```

**Step 5: Update Long help text in config_set.go and config_unset.go**

Add `defaultModel` line to the "Supported keys:" sections in both files:

```
  defaultModel                                 Default Claude model for missions (e.g., "opus", "sonnet", "claude-opus-4-6")
```

**Step 6: Verify compilation**

Run: `go build ./...`
Expected: PASS

**Step 7: Commit**

```bash
git add cmd/config_get.go cmd/config_set.go cmd/config_unset.go
git commit -m "Add CLI support for config get/set/unset defaultModel"
```

---

### Task 5: Add per-repo --default-model flag

**Files:**
- Modify: `cmd/command_str_consts.go:111` (add flag name constant)
- Modify: `cmd/config_repo_config.go:13-28` (Long help text)
- Modify: `cmd/config_repo_config_set.go:26-34` (flags and init)
- Modify: `cmd/config_repo_config_set.go:37-108` (runConfigRepoConfigSet)
- Modify: `cmd/config_repo_config_ls.go:43` (table columns)

**Step 1: Add flag name constant**

In `cmd/command_str_consts.go` (after line 111):

```go
	repoConfigDefaultModelFlagName      = "default-model"
```

**Step 2: Register the flag**

In `cmd/config_repo_config_set.go` init() function, add:

```go
	configRepoConfigSetCmd.Flags().String(repoConfigDefaultModelFlagName, "", `default Claude model for missions using this repo (e.g., "opus", "sonnet")`)
```

**Step 3: Handle the flag in runConfigRepoConfigSet**

Add `defaultModelChanged` check alongside the other flag checks:

```go
	defaultModelChanged := cmd.Flags().Changed(repoConfigDefaultModelFlagName)
```

Update the "at least one flag" check:

```go
	if !alwaysSyncedChanged && !windowTitleChanged && !trustedChanged && !defaultModelChanged {
		return stacktrace.NewError("at least one of --%s, --%s, --%s, or --%s must be provided",
			repoConfigAlwaysSyncedFlagName, repoConfigWindowTitleFlagName, repoConfigTrustedMcpServersFlagName, repoConfigDefaultModelFlagName)
	}
```

Add the flag handling block (after the trustedChanged block):

```go
	if defaultModelChanged {
		model, err := cmd.Flags().GetString(repoConfigDefaultModelFlagName)
		if err != nil {
			return stacktrace.Propagate(err, "failed to read --%s flag", repoConfigDefaultModelFlagName)
		}
		rc.DefaultModel = model
	}
```

**Step 4: Add DEFAULT MODEL column to repo-config ls**

In `cmd/config_repo_config_ls.go`, update the table header and row:

```go
	tbl := tableprinter.NewTable("REPO", "ALWAYS SYNCED", "WINDOW TITLE", "DEFAULT MODEL", "TRUSTED MCP SERVERS")
	for _, name := range repoNames {
		rc := cfg.RepoConfigs[name]
		synced := formatCheckmark(rc.AlwaysSynced)
		windowTitle := rc.WindowTitle
		if windowTitle == "" {
			windowTitle = "--"
		}
		defaultModel := rc.DefaultModel
		if defaultModel == "" {
			defaultModel = "--"
		}
		tbl.AddRow(displayGitRepo(name), synced, windowTitle, defaultModel, formatTrustedMcpServers(rc.TrustedMcpServers))
	}
```

**Step 5: Update help text in config_repo_config.go**

Add `defaultModel` to the Long description's settings list and example config.yml:

```
  defaultModel       - default Claude model for missions using this repo
```

And in the example:

```yaml
    github.com/owner/repo:
      alwaysSynced: true
      windowTitle: "my-repo"
      defaultModel: opus
      trustedMcpServers: all
```

**Step 6: Verify compilation**

Run: `go build ./...`
Expected: PASS

**Step 7: Commit**

```bash
git add cmd/command_str_consts.go cmd/config_repo_config.go cmd/config_repo_config_set.go cmd/config_repo_config_ls.go
git commit -m "Add --default-model flag to repo-config set and DEFAULT MODEL column to ls"
```

---

### Task 6: Update docs/system-architecture.md

**Files:**
- Modify: `docs/system-architecture.md` — document the new config key in the relevant section

**Step 1: Find and update the config section**

Search for where config keys or `config.yml` schema is documented in `docs/system-architecture.md`. Add `defaultModel` to the top-level config keys list and to the repoConfig subsection.

**Step 2: Commit**

```bash
git add docs/system-architecture.md
git commit -m "Document defaultModel config key in system architecture"
```

---

### Task 7: Manual smoke test

**Step 1: Build the binary**

Run: `make build`

**Step 2: Test config set/get/unset**

```bash
./agenc config set defaultModel opus
./agenc config get defaultModel        # expect: opus
./agenc config unset defaultModel
./agenc config get defaultModel        # expect: unset
```

**Step 3: Test repo-config**

```bash
./agenc config repoConfig set github.com/owner/repo --default-model sonnet
./agenc config repoConfig ls           # expect: DEFAULT MODEL column showing "sonnet"
./agenc config repoConfig set github.com/owner/repo --default-model ""
./agenc config repoConfig ls           # expect: "--" in DEFAULT MODEL column
```

**Step 4: Commit any fixes**

If any issues found, fix and commit.
