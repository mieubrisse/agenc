package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadWriteAgencConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &AgencConfig{
		RepoConfigs: map[string]RepoConfig{
			"github.com/owner/repo1": {AlwaysSynced: true},
			"github.com/owner/repo2": {AlwaysSynced: true, WindowTitle: "Custom"},
		},
	}

	if err := WriteAgencConfig(tmpDir, cfg, nil); err != nil {
		t.Fatalf("WriteAgencConfig failed: %v", err)
	}

	got, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	if len(got.RepoConfigs) != 2 {
		t.Fatalf("expected 2 repo configs, got %d", len(got.RepoConfigs))
	}

	rc1, ok := got.RepoConfigs["github.com/owner/repo1"]
	if !ok {
		t.Fatal("expected repo1 to exist in RepoConfigs")
	}
	if !rc1.AlwaysSynced {
		t.Error("expected repo1 to have alwaysSynced=true")
	}

	rc2, ok := got.RepoConfigs["github.com/owner/repo2"]
	if !ok {
		t.Fatal("expected repo2 to exist in RepoConfigs")
	}
	if rc2.WindowTitle != "Custom" {
		t.Errorf("expected repo2 windowTitle 'Custom', got '%s'", rc2.WindowTitle)
	}
}

func TestReadAgencConfig_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed for missing file: %v", err)
	}
	if len(cfg.RepoConfigs) != 0 {
		t.Errorf("expected empty repo configs, got %d", len(cfg.RepoConfigs))
	}
}

func TestReadAgencConfig_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	configFilepath := filepath.Join(configDirpath, ConfigFilename)
	if err := os.WriteFile(configFilepath, []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed for empty file: %v", err)
	}
	if len(cfg.RepoConfigs) != 0 {
		t.Errorf("expected empty repo configs, got %d", len(cfg.RepoConfigs))
	}
}

func TestReadAgencConfig_NonCanonicalRepoConfig(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
repoConfig:
  owner/repo:
    alwaysSynced: true
`)

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for non-canonical repo config key, got nil")
	}
}

func TestWriteReadPreservesComments(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a config file with comments via raw YAML
	configFilepath := filepath.Join(configDirpath, ConfigFilename)
	rawYAML := `# Top-level config comment
repoConfig:
  github.com/owner/repo1: # this is a synced repo
    alwaysSynced: true
`
	if err := os.WriteFile(configFilepath, []byte(rawYAML), 0644); err != nil {
		t.Fatal(err)
	}

	// Read with comment preservation
	cfg, cm, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	// Write back with the comment map
	if err := WriteAgencConfig(tmpDir, cfg, cm); err != nil {
		t.Fatalf("WriteAgencConfig failed: %v", err)
	}

	// Read the raw output and verify comments survived
	data, err := os.ReadFile(configFilepath)
	if err != nil {
		t.Fatalf("failed to read written config: %v", err)
	}

	output := string(data)
	if !strings.Contains(output, "this is a synced repo") {
		t.Errorf("inline comment was not preserved in round-trip; output:\n%s", output)
	}
}

func TestEnsureConfigFile(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	configFilepath := filepath.Join(configDirpath, ConfigFilename)

	// File should not exist yet
	if _, err := os.Stat(configFilepath); !os.IsNotExist(err) {
		t.Fatal("config file should not exist before EnsureConfigFile")
	}

	// Create it
	if err := EnsureConfigFile(tmpDir); err != nil {
		t.Fatalf("EnsureConfigFile failed: %v", err)
	}

	data, err := os.ReadFile(configFilepath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	expected := "{}\n"
	if string(data) != expected {
		t.Errorf("expected %q, got %q", expected, string(data))
	}

	// Calling again should be a no-op (file already exists)
	if err := EnsureConfigFile(tmpDir); err != nil {
		t.Fatalf("EnsureConfigFile (second call) failed: %v", err)
	}

	data2, err := os.ReadFile(configFilepath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data2) != expected {
		t.Errorf("file was modified by second EnsureConfigFile call")
	}
}

// --- RepoConfig tests ---

func TestRepoConfig_FromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
repoConfig:
  github.com/owner/repo1:
    alwaysSynced: true
  github.com/owner/repo2:
    alwaysSynced: true
    windowTitle: "My Repo"
`)

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	if len(cfg.RepoConfigs) != 2 {
		t.Fatalf("expected 2 repo configs, got %d", len(cfg.RepoConfigs))
	}

	rc1 := cfg.RepoConfigs["github.com/owner/repo1"]
	if !rc1.AlwaysSynced {
		t.Error("expected repo1 to have alwaysSynced=true")
	}
	if rc1.WindowTitle != "" {
		t.Errorf("expected empty window title for repo1, got '%s'", rc1.WindowTitle)
	}

	rc2 := cfg.RepoConfigs["github.com/owner/repo2"]
	if !rc2.AlwaysSynced {
		t.Error("expected repo2 to have alwaysSynced=true")
	}
	if rc2.WindowTitle != "My Repo" {
		t.Errorf("expected 'My Repo' window title for repo2, got '%s'", rc2.WindowTitle)
	}
}

func TestRepoConfig_GetWindowTitle(t *testing.T) {
	cfg := &AgencConfig{
		RepoConfigs: map[string]RepoConfig{
			"github.com/owner/repo1": {WindowTitle: "Custom Title"},
			"github.com/owner/repo2": {},
		},
	}

	if got := cfg.GetWindowTitle("github.com/owner/repo1"); got != "Custom Title" {
		t.Errorf("expected 'Custom Title', got '%s'", got)
	}
	if got := cfg.GetWindowTitle("github.com/owner/repo2"); got != "" {
		t.Errorf("expected empty string for repo without window title, got '%s'", got)
	}
	if got := cfg.GetWindowTitle("github.com/owner/nonexistent"); got != "" {
		t.Errorf("expected empty string for nonexistent repo, got '%s'", got)
	}
}

func TestRepoConfig_GetAllSyncedRepos(t *testing.T) {
	cfg := &AgencConfig{
		RepoConfigs: map[string]RepoConfig{
			"github.com/owner/repo1": {AlwaysSynced: true},
			"github.com/owner/repo2": {AlwaysSynced: false},
			"github.com/owner/repo3": {AlwaysSynced: true, WindowTitle: "R3"},
		},
	}

	synced := cfg.GetAllSyncedRepos()
	if len(synced) != 2 {
		t.Fatalf("expected 2 synced repos, got %d", len(synced))
	}
	// Sorted alphabetically
	if synced[0] != "github.com/owner/repo1" {
		t.Errorf("expected repo1, got '%s'", synced[0])
	}
	if synced[1] != "github.com/owner/repo3" {
		t.Errorf("expected repo3, got '%s'", synced[1])
	}
}

func TestRepoConfig_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &AgencConfig{
		RepoConfigs: map[string]RepoConfig{
			"github.com/owner/repo1": {AlwaysSynced: true, WindowTitle: "Custom"},
			"github.com/owner/repo2": {AlwaysSynced: true},
		},
	}

	if err := WriteAgencConfig(tmpDir, cfg, nil); err != nil {
		t.Fatalf("WriteAgencConfig failed: %v", err)
	}

	got, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	if len(got.RepoConfigs) != 2 {
		t.Fatalf("expected 2 repo configs, got %d", len(got.RepoConfigs))
	}

	rc1 := got.RepoConfigs["github.com/owner/repo1"]
	if !rc1.AlwaysSynced {
		t.Error("expected repo1 alwaysSynced=true")
	}
	if rc1.WindowTitle != "Custom" {
		t.Errorf("expected 'Custom', got '%s'", rc1.WindowTitle)
	}

	rc2 := got.RepoConfigs["github.com/owner/repo2"]
	if !rc2.AlwaysSynced {
		t.Error("expected repo2 alwaysSynced=true")
	}
	if rc2.WindowTitle != "" {
		t.Errorf("expected empty window title, got '%s'", rc2.WindowTitle)
	}
}

func TestRepoConfig_SetAndRemove(t *testing.T) {
	cfg := &AgencConfig{}

	cfg.SetRepoConfig("github.com/owner/repo1", RepoConfig{AlwaysSynced: true})
	if len(cfg.RepoConfigs) != 1 {
		t.Fatalf("expected 1 repo config, got %d", len(cfg.RepoConfigs))
	}

	if !cfg.IsAlwaysSynced("github.com/owner/repo1") {
		t.Error("expected repo1 to be always synced")
	}

	// Remove it
	removed := cfg.RemoveRepoConfig("github.com/owner/repo1")
	if !removed {
		t.Error("expected RemoveRepoConfig to return true")
	}
	if len(cfg.RepoConfigs) != 0 {
		t.Fatalf("expected 0 repo configs after remove, got %d", len(cfg.RepoConfigs))
	}

	// Removing again should be a no-op
	removed = cfg.RemoveRepoConfig("github.com/owner/repo1")
	if removed {
		t.Error("expected RemoveRepoConfig to return false for nonexistent repo")
	}
}

func TestRepoConfig_SetAlwaysSynced(t *testing.T) {
	cfg := &AgencConfig{}

	cfg.SetAlwaysSynced("github.com/owner/repo1", true)
	if !cfg.IsAlwaysSynced("github.com/owner/repo1") {
		t.Error("expected repo1 to be always synced after SetAlwaysSynced(true)")
	}

	// Set to false
	cfg.SetAlwaysSynced("github.com/owner/repo1", false)
	if cfg.IsAlwaysSynced("github.com/owner/repo1") {
		t.Error("expected repo1 to not be always synced after SetAlwaysSynced(false)")
	}

	// Existing window title should be preserved
	cfg.SetRepoConfig("github.com/owner/repo2", RepoConfig{WindowTitle: "test"})
	cfg.SetAlwaysSynced("github.com/owner/repo2", true)
	rc, _ := cfg.GetRepoConfig("github.com/owner/repo2")
	if rc.WindowTitle != "test" {
		t.Errorf("expected window title 'test' preserved, got '%s'", rc.WindowTitle)
	}
	if !rc.AlwaysSynced {
		t.Error("expected alwaysSynced=true")
	}
}

func TestIsCanonicalRepoName(t *testing.T) {
	if !IsCanonicalRepoName("github.com/owner/repo") {
		t.Error("expected github.com/owner/repo to be canonical")
	}
	if IsCanonicalRepoName("owner/repo") {
		t.Error("expected owner/repo to not be canonical")
	}
	if IsCanonicalRepoName("") {
		t.Error("expected empty string to not be canonical")
	}
}

// --- Palette commands tests ---

func writeConfigYAML(t *testing.T, tmpDir string, content string) {
	t.Helper()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}
	configFilepath := filepath.Join(configDirpath, ConfigFilename)
	if err := os.WriteFile(configFilepath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestPaletteCommands_BuiltinDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, "{}")

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	resolved := cfg.GetResolvedPaletteCommands()

	// All builtins should be present
	builtinNames := make(map[string]bool)
	for _, cmd := range resolved {
		builtinNames[cmd.Name] = true
	}
	for _, name := range builtinPaletteCommandOrder {
		if !builtinNames[name] {
			t.Errorf("expected builtin '%s' to be present in resolved commands", name)
		}
	}

	// Check specific defaults
	for _, cmd := range resolved {
		if cmd.Name == "do" {
			if cmd.Title != "✅ Do" {
				t.Errorf("expected do title '✅ Do', got '%s'", cmd.Title)
			}
			if cmd.TmuxKeybinding != "d" {
				t.Errorf("expected do keybinding 'd', got '%s'", cmd.TmuxKeybinding)
			}
			if !cmd.IsBuiltin {
				t.Error("expected do to be marked as builtin")
			}
		}
	}
}

func TestPaletteCommands_BuiltinOverride(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
paletteCommands:
  newMission:
    tmuxKeybinding: "C-n"
`)

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	resolved := cfg.GetResolvedPaletteCommands()
	for _, cmd := range resolved {
		if cmd.Name == "newMission" {
			if cmd.TmuxKeybinding != "C-n" {
				t.Errorf("expected overridden keybinding 'C-n', got '%s'", cmd.TmuxKeybinding)
			}
			// Title should keep the default
			if cmd.Title != "🚀 New Mission" {
				t.Errorf("expected default title '🚀 New Mission', got '%s'", cmd.Title)
			}
			if !cmd.IsBuiltin {
				t.Error("expected newMission to be marked as builtin")
			}
			return
		}
	}
	t.Error("newMission not found in resolved commands")
}

func TestPaletteCommands_BuiltinDisable(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
paletteCommands:
  nukeMissions: {}
`)

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	resolved := cfg.GetResolvedPaletteCommands()
	for _, cmd := range resolved {
		if cmd.Name == "nukeMissions" {
			t.Error("expected nukeMissions to be disabled (not in resolved list)")
		}
	}
}

func TestPaletteCommands_CustomWithKeybinding(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
paletteCommands:
  dotfiles:
    title: "📁 Open dotfiles"
    description: "Start a dotfiles mission"
    command: "agenc tmux window new -- agenc mission new mieubrisse/dotfiles"
    tmuxKeybinding: "f"
`)

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	resolved := cfg.GetResolvedPaletteCommands()
	var found bool
	for _, cmd := range resolved {
		if cmd.Name == "dotfiles" {
			found = true
			if cmd.Title != "📁 Open dotfiles" {
				t.Errorf("expected title '📁 Open dotfiles', got '%s'", cmd.Title)
			}
			if cmd.TmuxKeybinding != "f" {
				t.Errorf("expected keybinding 'f', got '%s'", cmd.TmuxKeybinding)
			}
			if cmd.IsBuiltin {
				t.Error("expected dotfiles to not be builtin")
			}
		}
	}
	if !found {
		t.Error("custom command 'dotfiles' not found in resolved list")
	}
}

func TestPaletteCommands_KeybindingUniqueness(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
paletteCommands:
  custom1:
    title: "Custom 1"
    command: "agenc do"
    tmuxKeybinding: "d"
`)

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for duplicate keybinding, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate palette keybinding") {
		t.Errorf("expected error mentioning duplicate keybinding, got: %v", err)
	}
}

func TestPaletteCommands_TitleUniqueness(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
paletteCommands:
  custom1:
    title: "✅ Do"
    command: "agenc do"
`)

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for duplicate title, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate palette title") {
		t.Errorf("expected error mentioning duplicate title, got: %v", err)
	}
}

func TestPaletteCommands_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &AgencConfig{
		PaletteCommands: map[string]PaletteCommandConfig{
			"dotfiles": {
				Title:          "📁 Open dotfiles",
				Description:    "Start a dotfiles mission",
				Command:        "agenc tmux window new -- agenc mission new mieubrisse/dotfiles",
				TmuxKeybinding: "f",
			},
			"logs": {
				Title:   "📋 Daemon logs",
				Command: "agenc tmux window new -- agenc daemon logs",
			},
		},
	}

	if err := WriteAgencConfig(tmpDir, cfg, nil); err != nil {
		t.Fatalf("WriteAgencConfig failed: %v", err)
	}

	got, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	if len(got.PaletteCommands) != 2 {
		t.Fatalf("expected 2 palette commands, got %d", len(got.PaletteCommands))
	}

	dotfiles, ok := got.PaletteCommands["dotfiles"]
	if !ok {
		t.Fatal("expected 'dotfiles' palette command to exist")
	}
	if dotfiles.Title != "📁 Open dotfiles" {
		t.Errorf("expected title '📁 Open dotfiles', got '%s'", dotfiles.Title)
	}
	if dotfiles.Command != "agenc tmux window new -- agenc mission new mieubrisse/dotfiles" {
		t.Errorf("expected command 'agenc tmux window new -- agenc mission new mieubrisse/dotfiles', got '%s'", dotfiles.Command)
	}
	if dotfiles.TmuxKeybinding != "f" {
		t.Errorf("expected keybinding 'f', got '%s'", dotfiles.TmuxKeybinding)
	}
}

func TestPaletteCommands_CustomMissingTitle(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
paletteCommands:
  custom1:
    command: "agenc do"
`)

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing title, got nil")
	}
	if !strings.Contains(err.Error(), "title") {
		t.Errorf("expected error mentioning title, got: %v", err)
	}
}

func TestPaletteCommands_CustomMissingCommand(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
paletteCommands:
  custom1:
    title: "My Command"
`)

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing command, got nil")
	}
	if !strings.Contains(err.Error(), "command") {
		t.Errorf("expected error mentioning command, got: %v", err)
	}
}

func TestPaletteCommands_InvalidName(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
paletteCommands:
  123bad:
    title: "Bad Name"
    command: "agenc do"
`)

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for invalid command name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected error mentioning invalid name, got: %v", err)
	}
}

func TestPaletteCommands_OrderPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
paletteCommands:
  zzz:
    title: "ZZZ"
    command: "agenc do"
  aaa:
    title: "AAA"
    command: "agenc do"
`)

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	resolved := cfg.GetResolvedPaletteCommands()

	// Custom entries should come after all builtins, in alphabetical order
	var customEntries []string
	for _, cmd := range resolved {
		if !cmd.IsBuiltin {
			customEntries = append(customEntries, cmd.Name)
		}
	}

	if len(customEntries) != 2 {
		t.Fatalf("expected 2 custom entries, got %d", len(customEntries))
	}
	if customEntries[0] != "aaa" || customEntries[1] != "zzz" {
		t.Errorf("expected custom entries in alphabetical order [aaa, zzz], got %v", customEntries)
	}
}

func TestIsMissionScoped_True(t *testing.T) {
	cmd := ResolvedPaletteCommand{
		Name:    "stopThisMission",
		Command: "agenc mission stop $AGENC_CALLING_MISSION_UUID",
	}
	if !cmd.IsMissionScoped() {
		t.Error("expected command referencing AGENC_CALLING_MISSION_UUID to be mission-scoped")
	}
}

func TestIsMissionScoped_False(t *testing.T) {
	cmd := ResolvedPaletteCommand{
		Name:    "do",
		Command: "agenc tmux window new -- agenc do",
	}
	if cmd.IsMissionScoped() {
		t.Error("expected command without AGENC_CALLING_MISSION_UUID to not be mission-scoped")
	}
}

func TestGetPaletteTmuxKeybinding_Default(t *testing.T) {
	cfg := &AgencConfig{}
	if got := cfg.GetPaletteTmuxKeybinding(); got != "-T agenc k" {
		t.Errorf("expected default palette keybinding '-T agenc k', got '%s'", got)
	}
}

func TestGetPaletteTmuxKeybinding_Custom(t *testing.T) {
	cfg := &AgencConfig{PaletteTmuxKeybinding: "p"}
	if got := cfg.GetPaletteTmuxKeybinding(); got != "p" {
		t.Errorf("expected custom palette keybinding 'p', got '%s'", got)
	}
}

func TestPaletteTmuxKeybinding_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &AgencConfig{PaletteTmuxKeybinding: "p"}
	if err := WriteAgencConfig(tmpDir, cfg, nil); err != nil {
		t.Fatalf("WriteAgencConfig failed: %v", err)
	}

	got, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	if got.PaletteTmuxKeybinding != "p" {
		t.Errorf("expected 'p', got '%s'", got.PaletteTmuxKeybinding)
	}
}

func TestPaletteTmuxKeybinding_ConflictsWithCommand(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
paletteTmuxKeybinding: "-T agenc d"
`)

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for palette keybinding conflicting with command keybinding, got nil")
	}
	if !strings.Contains(err.Error(), "palette keybinding") || !strings.Contains(err.Error(), "conflicts") {
		t.Errorf("expected error mentioning palette keybinding conflict, got: %v", err)
	}
}

func TestPaletteCommandConfig_IsEmpty(t *testing.T) {
	empty := PaletteCommandConfig{}
	if !empty.IsEmpty() {
		t.Error("expected empty config to be empty")
	}

	notEmpty := PaletteCommandConfig{Title: "test"}
	if notEmpty.IsEmpty() {
		t.Error("expected config with title to not be empty")
	}
}
