package claudeconfig

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestMergeAgencSandbox_EmptySettings(t *testing.T) {
	agencDirpath := "/tmp/test-agenc"
	expectedSocketPath := config.GetServerSocketFilepath(agencDirpath)
	settings := make(map[string]json.RawMessage)

	if err := mergeAgencSandbox(settings, agencDirpath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sockets := extractAllowUnixSockets(t, settings)
	if len(sockets) != 1 || sockets[0] != expectedSocketPath {
		t.Fatalf("expected [%s], got %v", expectedSocketPath, sockets)
	}
}

func TestMergeAgencSandbox_AppendsToExisting(t *testing.T) {
	agencDirpath := "/tmp/test-agenc"
	expectedSocketPath := config.GetServerSocketFilepath(agencDirpath)
	settingsJSON := `{"sandbox":{"network":{"allowUnixSockets":["//tmp/claude"]}}}`
	var settings map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		t.Fatalf("failed to parse test settings: %v", err)
	}

	if err := mergeAgencSandbox(settings, agencDirpath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sockets := extractAllowUnixSockets(t, settings)
	if len(sockets) != 2 {
		t.Fatalf("expected 2 sockets, got %d: %v", len(sockets), sockets)
	}
	if sockets[0] != "//tmp/claude" {
		t.Errorf("expected first socket '//tmp/claude', got %q", sockets[0])
	}
	if sockets[1] != expectedSocketPath {
		t.Errorf("expected second socket %q, got %q", expectedSocketPath, sockets[1])
	}
}

func TestMergeAgencSandbox_NoDuplicate(t *testing.T) {
	agencDirpath := "/tmp/test-agenc"
	expectedSocketPath := config.GetServerSocketFilepath(agencDirpath)
	settingsJSON := `{"sandbox":{"network":{"allowUnixSockets":["` + expectedSocketPath + `"]}}}`
	var settings map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		t.Fatalf("failed to parse test settings: %v", err)
	}

	if err := mergeAgencSandbox(settings, agencDirpath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sockets := extractAllowUnixSockets(t, settings)
	if len(sockets) != 1 {
		t.Fatalf("expected 1 socket (no duplicate), got %d: %v", len(sockets), sockets)
	}
}

func TestMergeAgencSandbox_PreservesExistingFields(t *testing.T) {
	agencDirpath := "/tmp/test-agenc"
	settingsJSON := `{"sandbox":{"filesystem":{"allowWrite":["//tmp"]},"network":{"allowedDomains":["example.com"],"allowUnixSockets":["//tmp/claude"]}}}`
	var settings map[string]json.RawMessage
	if err := json.Unmarshal([]byte(settingsJSON), &settings); err != nil {
		t.Fatalf("failed to parse test settings: %v", err)
	}

	if err := mergeAgencSandbox(settings, agencDirpath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sandboxMap map[string]json.RawMessage
	if err := json.Unmarshal(settings["sandbox"], &sandboxMap); err != nil {
		t.Fatalf("failed to parse sandbox: %v", err)
	}
	if _, ok := sandboxMap["filesystem"]; !ok {
		t.Fatal("expected filesystem section to be preserved")
	}

	var networkMap map[string]json.RawMessage
	if err := json.Unmarshal(sandboxMap["network"], &networkMap); err != nil {
		t.Fatalf("failed to parse network: %v", err)
	}
	if _, ok := networkMap["allowedDomains"]; !ok {
		t.Fatal("expected allowedDomains to be preserved")
	}
}

func TestMergeAgencSandbox_CustomAgencDirpath(t *testing.T) {
	customDirpath := "/custom/agenc/dir"
	settings := make(map[string]json.RawMessage)

	if err := mergeAgencSandbox(settings, customDirpath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sockets := extractAllowUnixSockets(t, settings)
	expectedPath := filepath.Join(customDirpath, config.ServerDirname, config.ServerSocketFilename)
	if len(sockets) != 1 || sockets[0] != expectedPath {
		t.Fatalf("expected [%s], got %v", expectedPath, sockets)
	}
}

// extractAllowUnixSockets is a test helper that extracts the allowUnixSockets
// array from a settings map.
func extractAllowUnixSockets(t *testing.T, settings map[string]json.RawMessage) []string {
	t.Helper()

	var sandboxMap map[string]json.RawMessage
	if err := json.Unmarshal(settings["sandbox"], &sandboxMap); err != nil {
		t.Fatalf("failed to parse sandbox: %v", err)
	}

	var networkMap map[string]json.RawMessage
	if err := json.Unmarshal(sandboxMap["network"], &networkMap); err != nil {
		t.Fatalf("failed to parse network: %v", err)
	}

	var sockets []string
	if err := json.Unmarshal(networkMap["allowUnixSockets"], &sockets); err != nil {
		t.Fatalf("failed to parse allowUnixSockets: %v", err)
	}

	return sockets
}
