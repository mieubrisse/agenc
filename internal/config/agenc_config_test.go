package config

import (
	"os"
	"path/filepath"
	"reflect"
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

// --- Custom commands tests ---

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

func TestCustomCommands_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &AgencConfig{
		CustomCommands: map[string]CustomCommandConfig{
			"dotfiles": {
				Args:        "mission new github.com/mieubrisse/dotfiles",
				PaletteName: "Open dotfiles",
			},
			"logs": {
				Args:        "daemon logs",
				PaletteName: "View daemon logs",
			},
		},
	}

	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	if err := WriteAgencConfig(tmpDir, cfg, nil); err != nil {
		t.Fatalf("WriteAgencConfig failed: %v", err)
	}

	got, _, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	if len(got.CustomCommands) != 2 {
		t.Fatalf("expected 2 custom commands, got %d", len(got.CustomCommands))
	}

	dotfiles, ok := got.CustomCommands["dotfiles"]
	if !ok {
		t.Fatal("expected 'dotfiles' custom command to exist")
	}
	if dotfiles.PaletteName != "Open dotfiles" {
		t.Errorf("expected paletteName 'Open dotfiles', got '%s'", dotfiles.PaletteName)
	}
	if dotfiles.Args != "mission new github.com/mieubrisse/dotfiles" {
		t.Errorf("expected args 'mission new github.com/mieubrisse/dotfiles', got '%s'", dotfiles.Args)
	}
}

func TestCustomCommands_MissingPaletteName(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
customCommands:
  dotfiles:
    args: mission new github.com/mieubrisse/dotfiles
`)

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing paletteName, got nil")
	}
	if !strings.Contains(err.Error(), "paletteName") {
		t.Errorf("expected error mentioning paletteName, got: %v", err)
	}
}

func TestCustomCommands_MissingArgs(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
customCommands:
  dotfiles:
    paletteName: "Open dotfiles"
`)

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
	if !strings.Contains(err.Error(), "args") {
		t.Errorf("expected error mentioning args, got: %v", err)
	}
}

func TestCustomCommands_DuplicatePaletteName(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
customCommands:
  dotfiles:
    args: mission new github.com/mieubrisse/dotfiles
    paletteName: "Same Name"
  other:
    args: daemon logs
    paletteName: "Same Name"
`)

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for duplicate paletteName, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate paletteName") {
		t.Errorf("expected error mentioning duplicate paletteName, got: %v", err)
	}
}

func TestCustomCommands_InvalidName(t *testing.T) {
	tmpDir := t.TempDir()
	writeConfigYAML(t, tmpDir, `
customCommands:
  123bad:
    args: mission new foo
    paletteName: "Bad Name"
`)

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for invalid command name, got nil")
	}
	if !strings.Contains(err.Error(), "invalid") {
		t.Errorf("expected error mentioning invalid name, got: %v", err)
	}
}

func TestCustomCommandConfig_GetArgs(t *testing.T) {
	tests := []struct {
		name     string
		args     string
		expected []string
	}{
		{
			name:     "simple args",
			args:     "mission new",
			expected: []string{"mission", "new"},
		},
		{
			name:     "args with extra whitespace",
			args:     "  mission   new   github.com/owner/repo  ",
			expected: []string{"mission", "new", "github.com/owner/repo"},
		},
		{
			name:     "single arg",
			args:     "status",
			expected: []string{"status"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &CustomCommandConfig{Args: tt.args}
			got := cfg.GetArgs()
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("GetArgs() = %v, want %v", got, tt.expected)
			}
		})
	}
}
