package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadWriteAgencConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &AgencConfig{
		AgentTemplates: map[string]AgentTemplateProperties{
			"github.com/owner/repo1": {Nickname: "my-agent"},
			"github.com/owner/repo2": {},
		},
	}

	if err := WriteAgencConfig(tmpDir, cfg); err != nil {
		t.Fatalf("WriteAgencConfig failed: %v", err)
	}

	got, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed: %v", err)
	}

	if len(got.AgentTemplates) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(got.AgentTemplates))
	}

	props1, ok := got.AgentTemplates["github.com/owner/repo1"]
	if !ok {
		t.Fatal("missing key 'github.com/owner/repo1'")
	}
	if props1.Nickname != "my-agent" {
		t.Errorf("expected nickname 'my-agent', got '%s'", props1.Nickname)
	}

	props2, ok := got.AgentTemplates["github.com/owner/repo2"]
	if !ok {
		t.Fatal("missing key 'github.com/owner/repo2'")
	}
	if props2.Nickname != "" {
		t.Errorf("expected empty nickname, got '%s'", props2.Nickname)
	}
}

func TestReadAgencConfig_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	cfg, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed for missing file: %v", err)
	}
	if len(cfg.AgentTemplates) != 0 {
		t.Errorf("expected empty templates, got %d", len(cfg.AgentTemplates))
	}
	if cfg.AgentTemplates == nil {
		t.Error("expected non-nil map, got nil")
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

	cfg, err := ReadAgencConfig(tmpDir)
	if err != nil {
		t.Fatalf("ReadAgencConfig failed for empty file: %v", err)
	}
	if len(cfg.AgentTemplates) != 0 {
		t.Errorf("expected empty templates, got %d", len(cfg.AgentTemplates))
	}
	if cfg.AgentTemplates == nil {
		t.Error("expected non-nil map, got nil")
	}
}

func TestReadAgencConfig_NonCanonicalKey(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	configFilepath := filepath.Join(configDirpath, ConfigFilename)
	content := "agentTemplates:\n    owner/repo: {}\n"
	if err := os.WriteFile(configFilepath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for non-canonical key, got nil")
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
	expected := "agentTemplates: {}\n"
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
