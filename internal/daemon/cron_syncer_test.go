package daemon

import (
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestNewCronSyncer(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	syncer := NewCronSyncer(tempDir)

	if syncer == nil {
		t.Fatal("NewCronSyncer returned nil")
	}
	if syncer.agencDirpath != tempDir {
		t.Errorf("agencDirpath = %v, want %v", syncer.agencDirpath, tempDir)
	}
	if syncer.manager == nil {
		t.Error("manager not initialized")
	}
}

// Note: Full integration tests for SyncCronsToLaunchd would require:
// 1. Mocking launchctl commands
// 2. Creating temporary plist directories
// 3. Setting up a test config with cron definitions
// These are best done as integration tests in a separate test suite.

func TestSyncCronsToLaunchd_EmptyCrons(t *testing.T) {
	t.Helper()

	tempDir := t.TempDir()
	syncer := NewCronSyncer(tempDir)

	// Test with no crons
	crons := make(map[string]config.CronConfig)

	// This should not error even with no crons
	// Note: We can't test the actual launchctl operations without mocking
	testLog := &syncerTestLogger{}
	err := syncer.SyncCronsToLaunchd(crons, testLog)

	// Should complete without error
	if err != nil {
		t.Errorf("SyncCronsToLaunchd() with empty crons failed: %v", err)
	}
}

// syncerTestLogger is a minimal logger for testing cron syncer
type syncerTestLogger struct {
	messages []string
}

func (l *syncerTestLogger) Printf(format string, v ...any) {
	// Store messages for verification if needed
}
