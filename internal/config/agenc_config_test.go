package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
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
		if cmd.Name == "newMission" {
			if cmd.Title != "🚀  New Mission" {
				t.Errorf("expected newMission title '🚀  New Mission', got '%s'", cmd.Title)
			}
			if cmd.TmuxKeybinding != "-n C-n" {
				t.Errorf("expected newMission keybinding '-n C-n', got '%s'", cmd.TmuxKeybinding)
			}
			if !cmd.IsBuiltin {
				t.Error("expected newMission to be marked as builtin")
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
			if cmd.Title != "🚀  New Mission" {
				t.Errorf("expected default title '🚀  New Mission', got '%s'", cmd.Title)
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
    command: "echo test"
    tmuxKeybinding: "-n C-n"
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
    title: "🚀  New Mission"
    command: "echo test"
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
    command: "echo test"
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
    command: "echo test"
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
    command: "echo test"
  aaa:
    title: "AAA"
    command: "echo test"
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
		Name:    "newMission",
		Command: "agenc mission new",
	}
	if cmd.IsMissionScoped() {
		t.Error("expected command without AGENC_CALLING_MISSION_UUID to not be mission-scoped")
	}
}

func TestGetPaletteTmuxKeybinding_Default(t *testing.T) {
	cfg := &AgencConfig{}
	if got := cfg.GetPaletteTmuxKeybinding(); got != DefaultPaletteTmuxKeybinding {
		t.Errorf("expected default palette keybinding '%s', got '%s'", DefaultPaletteTmuxKeybinding, got)
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
	// Set palette to "-T agenc x" and a custom command with bare keybinding "x"
	// so the palette key "x" conflicts with the command keybinding "x".
	writeConfigYAML(t, tmpDir, `
paletteTmuxKeybinding: "-T agenc x"
paletteCommands:
  customCmd:
    title: "Custom"
    command: "echo test"
    tmuxKeybinding: "x"
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

// --- Validation tests ---

func TestValidateGitRepoURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{
			name:    "valid HTTPS GitHub URL",
			url:     "https://github.com/owner/repo",
			wantErr: false,
		},
		{
			name:    "valid HTTPS GitHub URL with .git",
			url:     "https://github.com/owner/repo.git",
			wantErr: false,
		},
		{
			name:    "valid SSH GitHub URL",
			url:     "git@github.com:owner/repo",
			wantErr: false,
		},
		{
			name:    "valid SSH GitHub URL with .git",
			url:     "git@github.com:owner/repo.git",
			wantErr: false,
		},
		{
			name:    "empty URL",
			url:     "",
			wantErr: true,
		},
		{
			name:    "invalid HTTPS URL - no path",
			url:     "https://github.com",
			wantErr: true,
		},
		{
			name:    "invalid HTTPS URL - only slash",
			url:     "https://github.com/",
			wantErr: true,
		},
		{
			name:    "invalid HTTPS URL - single component",
			url:     "https://github.com/owner",
			wantErr: true,
		},
		{
			name:    "invalid SSH URL - missing colon",
			url:     "git@github.com/owner/repo",
			wantErr: true,
		},
		{
			name:    "invalid SSH URL - no path",
			url:     "git@github.com:",
			wantErr: true,
		},
		{
			name:    "invalid scheme",
			url:     "ftp://github.com/owner/repo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGitRepoURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGitRepoURL() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidatePathNoTraversal(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantErr bool
	}{
		{
			name:    "empty path",
			path:    "",
			wantErr: false,
		},
		{
			name:    "valid absolute path",
			path:    "/home/user/project",
			wantErr: false,
		},
		{
			name:    "valid relative path",
			path:    "src/main.go",
			wantErr: false,
		},
		{
			name:    "path with parent traversal",
			path:    "../../../etc/passwd",
			wantErr: true,
		},
		{
			name:    "path with relative in middle",
			path:    "/home/../etc/passwd",
			wantErr: true,
		},
		{
			name:    "path ending with ..",
			path:    "/home/user/..",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePathNoTraversal(tt.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidatePathNoTraversal() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateMaxConcurrent(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		wantErr bool
	}{
		{
			name:    "valid value 1",
			value:   1,
			wantErr: false,
		},
		{
			name:    "valid value 10",
			value:   10,
			wantErr: false,
		},
		{
			name:    "valid value 100",
			value:   100,
			wantErr: false,
		},
		{
			name:    "zero",
			value:   0,
			wantErr: true,
		},
		{
			name:    "negative",
			value:   -1,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMaxConcurrent(tt.value)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateMaxConcurrent() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSanitizePrompt(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantOutput   string
		wantModified bool
	}{
		{
			name:         "clean string",
			input:        "Hello world!",
			wantOutput:   "Hello world!",
			wantModified: false,
		},
		{
			name:         "string with newlines",
			input:        "Line 1\nLine 2",
			wantOutput:   "Line 1\nLine 2",
			wantModified: false,
		},
		{
			name:         "string with tabs",
			input:        "Column1\tColumn2",
			wantOutput:   "Column1\tColumn2",
			wantModified: false,
		},
		{
			name:         "string with bell character",
			input:        "Alert\a!",
			wantOutput:   "Alert!",
			wantModified: true,
		},
		{
			name:         "string with null character",
			input:        "Test\x00value",
			wantOutput:   "Testvalue",
			wantModified: true,
		},
		{
			name:         "string with ANSI escape",
			input:        "\x1b[31mRed text\x1b[0m",
			wantOutput:   "[31mRed text[0m",
			wantModified: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, modified := SanitizePrompt(tt.input)
			if output != tt.wantOutput {
				t.Errorf("SanitizePrompt() output = %q, want %q", output, tt.wantOutput)
			}
			if modified != tt.wantModified {
				t.Errorf("SanitizePrompt() modified = %v, want %v", modified, tt.wantModified)
			}
		})
	}
}

func TestValidateCronTimeout_Bounds(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		wantErr bool
	}{
		{
			name:    "empty is valid (uses default)",
			timeout: "",
			wantErr: false,
		},
		{
			name:    "valid timeout 1h",
			timeout: "1h",
			wantErr: false,
		},
		{
			name:    "valid timeout 30m",
			timeout: "30m",
			wantErr: false,
		},
		{
			name:    "valid timeout 24h (max)",
			timeout: "24h",
			wantErr: false,
		},
		{
			name:    "invalid timeout - exceeds max",
			timeout: "25h",
			wantErr: true,
		},
		{
			name:    "invalid timeout - negative",
			timeout: "-1h",
			wantErr: true,
		},
		{
			name:    "invalid timeout - zero",
			timeout: "0s",
			wantErr: true,
		},
		{
			name:    "invalid timeout format",
			timeout: "not-a-duration",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCronTimeout(tt.timeout)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCronTimeout() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateAndPopulateDefaults(t *testing.T) {
	t.Run("sanitizes cron prompt with control characters", func(t *testing.T) {
		cfg := &AgencConfig{
			Crons: map[string]CronConfig{
				"test": {
					Schedule: "0 9 * * *",
					Prompt:   "Test\x00prompt\awith\x1bcontrols",
				},
			},
		}

		err := ValidateAndPopulateDefaults(cfg)
		if err != nil {
			t.Fatalf("ValidateAndPopulateDefaults() failed: %v", err)
		}

		got := cfg.Crons["test"].Prompt
		want := "Testpromptwithcontrols"
		if got != want {
			t.Errorf("prompt not sanitized: got %q, want %q", got, want)
		}
	})

	t.Run("validates timeout bounds", func(t *testing.T) {
		cfg := &AgencConfig{
			Crons: map[string]CronConfig{
				"test": {
					Schedule: "0 9 * * *",
					Prompt:   "Test",
					Timeout:  "25h",
				},
			},
		}

		err := ValidateAndPopulateDefaults(cfg)
		if err == nil {
			t.Fatal("expected error for timeout exceeding max, got nil")
		}
		if !strings.Contains(err.Error(), "exceeds maximum") {
			t.Errorf("expected error about exceeding maximum, got: %v", err)
		}
	})

	t.Run("validates max concurrent >= 1", func(t *testing.T) {
		cfg := &AgencConfig{
			CronsMaxConcurrent: -5,
		}

		err := ValidateAndPopulateDefaults(cfg)
		if err == nil {
			t.Fatal("expected error for negative max concurrent, got nil")
		}
		if !strings.Contains(err.Error(), "must be >= 1") {
			t.Errorf("expected error about >= 1, got: %v", err)
		}
	})

	t.Run("allows max concurrent = 0 (uses default)", func(t *testing.T) {
		cfg := &AgencConfig{
			CronsMaxConcurrent: 0,
		}

		err := ValidateAndPopulateDefaults(cfg)
		if err != nil {
			t.Fatalf("ValidateAndPopulateDefaults() should allow 0: %v", err)
		}
	})

	t.Run("sanitizes palette command strings", func(t *testing.T) {
		cfg := &AgencConfig{
			PaletteCommands: map[string]PaletteCommandConfig{
				"test": {
					Title:   "Test",
					Command: "echo 'test'\x00\x1b",
				},
			},
		}

		err := ValidateAndPopulateDefaults(cfg)
		if err != nil {
			t.Fatalf("ValidateAndPopulateDefaults() failed: %v", err)
		}

		got := cfg.PaletteCommands["test"].Command
		want := "echo 'test'"
		if got != want {
			t.Errorf("command not sanitized: got %q, want %q", got, want)
		}
	})
}

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

func TestTrustedMcpServers_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	allServers := &TrustedMcpServers{All: true}
	listServers := &TrustedMcpServers{List: []string{"github", "sentry"}}

	cfg := &AgencConfig{
		RepoConfigs: map[string]RepoConfig{
			"github.com/owner/repo1": {TrustedMcpServers: allServers},
			"github.com/owner/repo2": {TrustedMcpServers: listServers},
			"github.com/owner/repo3": {},
		},
	}

	if err := WriteAgencConfig(tmpDir, cfg, nil); err != nil {
		t.Fatalf("WriteAgencConfig failed: %v", err)
	}

	got, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	rc1 := got.RepoConfigs["github.com/owner/repo1"]
	if rc1.TrustedMcpServers == nil || !rc1.TrustedMcpServers.All {
		t.Errorf("repo1: expected TrustedMcpServers.All=true, got %+v", rc1.TrustedMcpServers)
	}

	rc2 := got.RepoConfigs["github.com/owner/repo2"]
	if rc2.TrustedMcpServers == nil || rc2.TrustedMcpServers.All {
		t.Errorf("repo2: expected TrustedMcpServers with list, got %+v", rc2.TrustedMcpServers)
	}
	if len(rc2.TrustedMcpServers.List) != 2 || rc2.TrustedMcpServers.List[0] != "github" {
		t.Errorf("repo2: expected [github sentry], got %v", rc2.TrustedMcpServers.List)
	}

	rc3 := got.RepoConfigs["github.com/owner/repo3"]
	if rc3.TrustedMcpServers != nil {
		t.Errorf("repo3: expected nil TrustedMcpServers, got %+v", rc3.TrustedMcpServers)
	}
}

func TestReadAgencConfig_CallsValidation(t *testing.T) {
	t.Run("rejects cron with invalid timeout", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeConfigYAML(t, tmpDir, `
crons:
  test:
    schedule: "0 9 * * *"
    prompt: "Test"
    timeout: "48h"
`)

		_, _, err := ReadAgencConfig(tmpDir)
		if err == nil {
			t.Fatal("expected error for timeout exceeding max, got nil")
		}
		if !strings.Contains(err.Error(), "exceeds maximum") {
			t.Errorf("expected validation error about exceeding maximum, got: %v", err)
		}
	})

	t.Run("rejects invalid max concurrent", func(t *testing.T) {
		tmpDir := t.TempDir()
		writeConfigYAML(t, tmpDir, `
cronsMaxConcurrent: -5
`)

		_, _, err := ReadAgencConfig(tmpDir)
		if err == nil {
			t.Fatal("expected error for negative max concurrent, got nil")
		}
		if !strings.Contains(err.Error(), "must be >= 1") {
			t.Errorf("expected validation error about >= 1, got: %v", err)
		}
	})
}

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

func TestPostUpdateHook_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &AgencConfig{
		RepoConfigs: map[string]RepoConfig{
			"github.com/owner/repo1": {PostUpdateHook: "make setup"},
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

	rc1 := got.RepoConfigs["github.com/owner/repo1"]
	if rc1.PostUpdateHook != "make setup" {
		t.Errorf("expected repo1 postUpdateHook 'make setup', got '%s'", rc1.PostUpdateHook)
	}

	rc2 := got.RepoConfigs["github.com/owner/repo2"]
	if rc2.PostUpdateHook != "" {
		t.Errorf("expected empty postUpdateHook for repo2, got '%s'", rc2.PostUpdateHook)
	}
}

func TestPostUpdateHook_FromYAML(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	yamlContent := `
repoConfig:
  github.com/owner/repo:
    alwaysSynced: true
    postUpdateHook: "npm install && npm run build"
`
	configFilepath := filepath.Join(configDirpath, ConfigFilename)
	if err := os.WriteFile(configFilepath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	rc := cfg.RepoConfigs["github.com/owner/repo"]
	if !rc.AlwaysSynced {
		t.Error("expected alwaysSynced=true")
	}
	if rc.PostUpdateHook != "npm install && npm run build" {
		t.Errorf("expected postUpdateHook 'npm install && npm run build', got '%s'", rc.PostUpdateHook)
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
