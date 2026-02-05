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
		AgentTemplates: map[string]AgentTemplateProperties{
			"github.com/owner/repo1": {Nickname: "my-agent"},
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

	cfg, _, err := ReadAgencConfig(tmpDir)
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

	cfg, _, err := ReadAgencConfig(tmpDir)
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

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for non-canonical key, got nil")
	}
}

func TestReadWriteDefaultFor(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := &AgencConfig{
		AgentTemplates: map[string]AgentTemplateProperties{
			"github.com/owner/coding-agent": {
				Nickname:   "coder",
				DefaultFor: "emptyMission",
			},
			"github.com/owner/repo-agent": {
				DefaultFor: "repo",
			},
			"github.com/owner/meta-agent": {
				DefaultFor: "agentTemplate",
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

	if len(got.AgentTemplates) != 3 {
		t.Fatalf("expected 3 templates, got %d", len(got.AgentTemplates))
	}

	codingAgent := got.AgentTemplates["github.com/owner/coding-agent"]
	if codingAgent.Nickname != "coder" {
		t.Errorf("expected nickname 'coder', got '%s'", codingAgent.Nickname)
	}
	if codingAgent.DefaultFor != "emptyMission" {
		t.Errorf("expected defaultFor 'emptyMission', got '%s'", codingAgent.DefaultFor)
	}

	repoAgent := got.AgentTemplates["github.com/owner/repo-agent"]
	if repoAgent.DefaultFor != "repo" {
		t.Errorf("expected defaultFor 'repo', got '%s'", repoAgent.DefaultFor)
	}

	metaAgent := got.AgentTemplates["github.com/owner/meta-agent"]
	if metaAgent.DefaultFor != "agentTemplate" {
		t.Errorf("expected defaultFor 'agentTemplate', got '%s'", metaAgent.DefaultFor)
	}
}

func TestReadAgencConfig_DuplicateDefaultFor(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	configFilepath := filepath.Join(configDirpath, ConfigFilename)
	content := `agentTemplates:
  github.com/owner/agent1:
    defaultFor: repo
  github.com/owner/agent2:
    defaultFor: repo
`
	if err := os.WriteFile(configFilepath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for duplicate defaultFor, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate defaultFor") {
		t.Errorf("expected 'duplicate defaultFor' in error, got: %v", err)
	}
}

func TestReadAgencConfig_InvalidDefaultFor(t *testing.T) {
	tmpDir := t.TempDir()
	configDirpath := filepath.Join(tmpDir, ConfigDirname)
	if err := os.MkdirAll(configDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	configFilepath := filepath.Join(configDirpath, ConfigFilename)
	content := `agentTemplates:
  github.com/owner/agent1:
    defaultFor: bogusValue
`
	if err := os.WriteFile(configFilepath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, err := ReadAgencConfig(tmpDir)
	if err == nil {
		t.Fatal("expected error for invalid defaultFor value, got nil")
	}
	if !strings.Contains(err.Error(), "invalid defaultFor") {
		t.Errorf("expected 'invalid defaultFor' in error, got: %v", err)
	}
}

func TestFindDefaultTemplate(t *testing.T) {
	templates := map[string]AgentTemplateProperties{
		"github.com/owner/coding-agent": {
			Nickname:   "coder",
			DefaultFor: "emptyMission",
		},
		"github.com/owner/repo-agent": {
			DefaultFor: "repo",
		},
		"github.com/owner/plain-agent": {},
	}

	if got := FindDefaultTemplate(templates, "emptyMission"); got != "github.com/owner/coding-agent" {
		t.Errorf("expected 'github.com/owner/coding-agent' for emptyMission, got '%s'", got)
	}

	if got := FindDefaultTemplate(templates, "repo"); got != "github.com/owner/repo-agent" {
		t.Errorf("expected 'github.com/owner/repo-agent' for repo, got '%s'", got)
	}

	if got := FindDefaultTemplate(templates, "agentTemplate"); got != "" {
		t.Errorf("expected empty string for agentTemplate (not claimed), got '%s'", got)
	}

	if got := FindDefaultTemplate(templates, "nonexistent"); got != "" {
		t.Errorf("expected empty string for nonexistent context, got '%s'", got)
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
agentTemplates:
  github.com/owner/coding-agent:
    nickname: coder
    defaultFor: emptyMission # this is the default for blank missions
  github.com/owner/repo-agent:
    defaultFor: repo
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
	if !strings.Contains(output, "this is the default for blank missions") {
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
	expected := `agentTemplates:
  github.com/mieubrisse/agenc-agent-template_agenc-engineer:
    nickname: "🤖 AgenC Engineer"
    defaultFor: agentTemplate
`
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
