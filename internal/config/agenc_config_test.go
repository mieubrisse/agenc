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
