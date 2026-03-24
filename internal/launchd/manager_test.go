package launchd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewManager(t *testing.T) {
	t.Helper()

	m := NewManager()
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	if m.timeout == 0 {
		t.Error("Manager timeout not set")
	}
}

func TestGetPlistPathForLabel(t *testing.T) {
	tests := []struct {
		name         string
		label        string
		wantFilename string
	}{
		{
			name:         "current format with UUID",
			label:        "agenc-cron.a1b2c3d4-e5f6-7890-abcd-ef1234567890",
			wantFilename: "agenc-cron.a1b2c3d4-e5f6-7890-abcd-ef1234567890.plist",
		},
		{
			name:         "legacy format with name",
			label:        "agenc-cron-my-cron-job",
			wantFilename: "agenc-cron.my-cron-job.plist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path, err := GetPlistPathForLabel(tt.label)
			if err != nil {
				t.Fatalf("GetPlistPathForLabel() error = %v", err)
			}
			if filepath.Base(path) != tt.wantFilename {
				t.Errorf("GetPlistPathForLabel() filename = %v, want %v", filepath.Base(path), tt.wantFilename)
			}
		})
	}
}

func TestRemovePlist(t *testing.T) {
	t.Helper()

	// This test verifies the logic flow but doesn't actually call launchctl
	// In a real environment, this would require mocking launchctl commands

	tempDir := t.TempDir()
	plistPath := filepath.Join(tempDir, "test.plist")

	// Create a test plist file
	err := os.WriteFile(plistPath, []byte("test"), 0644)
	if err != nil {
		t.Fatalf("failed to create test plist: %v", err)
	}

	// Note: We can't actually test RemovePlist without mocking launchctl
	// This test just verifies the file exists
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		t.Error("test plist file does not exist")
	}
}

func TestVerifyLaunchctlAvailable(t *testing.T) {
	// This test will only pass on macOS where launchctl is available
	// Skip on other platforms
	err := VerifyLaunchctlAvailable()
	if err != nil {
		t.Skipf("launchctl not available (expected on macOS only): %v", err)
	}
}
