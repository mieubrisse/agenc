package wrapper

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func writeWrapperTestConfig(t *testing.T, agencDirpath string, configYAML string) {
	t.Helper()
	configFilepath := config.GetConfigFilepath(agencDirpath)
	if err := os.MkdirAll(filepath.Dir(configFilepath), 0755); err != nil {
		t.Fatalf("failed to create config dir: %v", err)
	}
	if err := os.WriteFile(configFilepath, []byte(configYAML), 0644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}
}

// A config that fails to load must stop the wrapper. Spawning anyway would drop
// the claudeArgs chain, and with it the default model, without a sound.
func TestNewWrapperRefusesAConfigThatFailsToLoad(t *testing.T) {
	agencDirpath := t.TempDir()
	writeWrapperTestConfig(t, agencDirpath, "defaultModel: opus\n")

	_, err := NewWrapper(agencDirpath, "test-mission-uuid", "", "", nil, nil)
	if err == nil {
		t.Fatal("expected NewWrapper to fail on a config that still sets defaultModel")
	}
	if !strings.Contains(err.Error(), "defaultModel") {
		t.Fatalf("expected the error to name the retired key, got: %v", err)
	}
}

// The config chain comes first and the mission's own args after it, so the
// mission's flag wins by Claude's last-wins resolution.
func TestNewWrapperPutsConfigArgsBeforeMissionArgs(t *testing.T) {
	agencDirpath := t.TempDir()
	writeWrapperTestConfig(t, agencDirpath, "claudeArgs:\n  - --model\n  - opus\n")

	w, err := NewWrapper(agencDirpath, "test-mission-uuid", "", "", []string{"--model", "haiku"}, nil)
	if err != nil {
		t.Fatalf("NewWrapper failed: %v", err)
	}
	expected := []string{"--model", "opus", "--model", "haiku"}
	if !reflect.DeepEqual(w.claudeSpawnArgs, expected) {
		t.Fatalf("expected %v, got %v", expected, w.claudeSpawnArgs)
	}
}
