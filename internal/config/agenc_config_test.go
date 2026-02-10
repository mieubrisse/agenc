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
		SyncedRepos: []string{"github.com/owner/repo1", "github.com/owner/repo2"},
	}

	if err := WriteAgencConfig(tmpDir, cfg, nil); err != nil {
		t.Fatalf("WriteAgencConfig failed: %v", err)
	}

	got, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	if len(got.SyncedRepos) != 2 {
		t.Fatalf("expected 2 synced repos, got %d", len(got.SyncedRepos))
	}

	if got.SyncedRepos[0] != "github.com/owner/repo1" {
		t.Errorf("expected 'github.com/owner/repo1', got '%s'", got.SyncedRepos[0])
	}

	if got.SyncedRepos[1] != "github.com/owner/repo2" {
		t.Errorf("expected 'github.com/owner/repo2', got '%s'", got.SyncedRepos[1])
	}
}

func TestReadAgencConfig_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed for missing file: %v", err)
	}
	if len(cfg.SyncedRepos) != 0 {
		t.Errorf("expected empty synced repos, got %d", len(cfg.SyncedRepos))
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
	if len(cfg.SyncedRepos) != 0 {
		t.Errorf("expected empty synced repos, got %d", len(cfg.SyncedRepos))
	}
}

func TestReadAgencConfig_NonCanonicalSyncedRepo(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	configFilepath := filepath.Join(configDirpath, ConfigFilename)
	content := "syncedRepos:\n  - owner/repo\n"
	if err := os.WriteFile(configFilepath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for non-canonical synced repo, got nil")
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
syncedRepos:
  - github.com/owner/repo1 # this is a synced repo
  - github.com/owner/repo2
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
  startMission:
    tmuxKeybinding: "C-n"
`)

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	resolved := cfg.GetResolvedPaletteCommands()
	for _, cmd := range resolved {
		if cmd.Name == "startMission" {
			if cmd.TmuxKeybinding != "C-n" {
				t.Errorf("expected overridden keybinding 'C-n', got '%s'", cmd.TmuxKeybinding)
			}
			// Title should keep the default
			if cmd.Title != "🚀 Start mission" {
				t.Errorf("expected default title '🚀 Start mission', got '%s'", cmd.Title)
			}
			if !cmd.IsBuiltin {
				t.Error("expected startMission to be marked as builtin")
			}
			return
		}
	}
	t.Error("startMission not found in resolved commands")
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

func TestPaletteCommands_AgencBinarySubstitution(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
tmuxAgencFilepath: /tmp/claude/test-agenc
paletteCommands:
  dotfiles:
    title: "📁 Dotfiles"
    command: "agenc tmux window new -- agenc mission new github.com/owner/agenc"
`)

	cfg, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	resolved := cfg.GetResolvedPaletteCommands()
	for _, cmd := range resolved {
		if cmd.Name == "dotfiles" {
			// Only standalone "agenc" tokens should be replaced, not substrings like "owner/agenc"
			expected := "/tmp/claude/test-agenc tmux window new -- /tmp/claude/test-agenc mission new github.com/owner/agenc"
			if cmd.Command != expected {
				t.Errorf("expected command\n  '%s'\ngot\n  '%s'", expected, cmd.Command)
			}
			return
		}
	}
	t.Error("dotfiles not found in resolved commands")
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
