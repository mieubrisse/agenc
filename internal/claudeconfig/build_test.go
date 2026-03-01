package claudeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestComputeCredentialServiceName(t *testing.T) {
	t.Run("deterministic output", func(t *testing.T) {
		path := "/Users/test/.agenc/missions/abc123/claude-config"
		name1 := ComputeCredentialServiceName(path)
		name2 := ComputeCredentialServiceName(path)
		if name1 != name2 {
			t.Errorf("expected deterministic output, got %q and %q", name1, name2)
		}
	})

	t.Run("has correct prefix", func(t *testing.T) {
		path := "/Users/test/.agenc/missions/abc123/claude-config"
		name := ComputeCredentialServiceName(path)
		prefix := "Claude Code-credentials-"
		if !strings.HasPrefix(name, prefix) {
			t.Errorf("expected prefix %q, got %q", prefix, name)
		}
	})

	t.Run("hash suffix is exactly 8 hex characters", func(t *testing.T) {
		path := "/Users/test/.agenc/missions/abc123/claude-config"
		name := ComputeCredentialServiceName(path)
		prefix := "Claude Code-credentials-"
		suffix := strings.TrimPrefix(name, prefix)
		if len(suffix) != 8 {
			t.Errorf("expected 8-char hash suffix, got %d chars: %q", len(suffix), suffix)
		}
		matched, _ := regexp.MatchString(`^[0-9a-f]{8}$`, suffix)
		if !matched {
			t.Errorf("expected hex characters in suffix, got %q", suffix)
		}
	})

	t.Run("different paths produce different names", func(t *testing.T) {
		path1 := "/Users/test/.agenc/missions/abc123/claude-config"
		path2 := "/Users/test/.agenc/missions/def456/claude-config"
		name1 := ComputeCredentialServiceName(path1)
		name2 := ComputeCredentialServiceName(path2)
		if name1 == name2 {
			t.Errorf("expected different names for different paths, both got %q", name1)
		}
	})
}

func TestCopyAndPatchClaudeJSON_NoTrust(t *testing.T) {
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

	result := readClaudeJSONProjectEntry(t, destDir, agentDir)
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

	result := readClaudeJSONProjectEntry(t, destDir, agentDir)
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

	result := readClaudeJSONProjectEntry(t, destDir, agentDir)
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

func TestComputeProjectDirpath(t *testing.T) {
	result, err := ComputeProjectDirpath("/Users/odyssey/.agenc/missions/abc-123/agent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	homeDir, _ := os.UserHomeDir()
	expected := filepath.Join(homeDir, ".claude", "projects", "-Users-odyssey--agenc-missions-abc-123-agent")
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

// setupFakeHome creates a temp dir and overrides HOME so os.UserHomeDir() returns it.
func setupFakeHome(t *testing.T) string {
	t.Helper()
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	return homeDir
}

// readClaudeJSONProjectEntry reads the project entry map from the output .claude.json.
func readClaudeJSONProjectEntry(t *testing.T, destDir string, agentDir string) map[string]interface{} {
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
		t.Fatalf("agent dir entry missing in projects for %q", agentDir)
	}
	return entry
}
