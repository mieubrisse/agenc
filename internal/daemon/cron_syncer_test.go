package daemon

import (
	"testing"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/launchd"
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

func TestSanitizeCronName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "simple name",
			input: "my-cron",
			want:  "my-cron",
		},
		{
			name:  "name with spaces",
			input: "my cron job",
			want:  "my-cron-job",
		},
		{
			name:  "name with special characters",
			input: "my@cron#job!",
			want:  "mycronjob",
		},
		{
			name:  "name with underscores",
			input: "my_cron_job",
			want:  "my_cron_job",
		},
		{
			name:  "name with mixed case",
			input: "MyCronJob",
			want:  "MyCronJob",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := launchd.SanitizeCronName(tt.input)
			if got != tt.want {
				t.Errorf("launchd.SanitizeCronName() = %v, want %v", got, tt.want)
			}
		})
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
