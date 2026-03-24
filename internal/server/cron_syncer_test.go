package server

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/launchd"
)

func TestNewCronSyncer(t *testing.T) {
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

func TestSyncCronsToLaunchd_EmptyCrons(t *testing.T) {
	tempDir := t.TempDir()
	syncer := NewCronSyncer(tempDir)

	crons := make(map[string]config.CronConfig)
	testLog := &syncerTestLogger{}
	err := syncer.SyncCronsToLaunchd(crons, testLog)

	if err != nil {
		t.Errorf("SyncCronsToLaunchd() with empty crons failed: %v", err)
	}
}

// mockLaunchdManager records calls for verification.
type mockLaunchdManager struct {
	loadedLabels map[string]bool
	loadCalls    []string
	unloadCalls  []string
	removeCalls  []string
}

func newMockManager() *mockLaunchdManager {
	return &mockLaunchdManager{
		loadedLabels: make(map[string]bool),
	}
}

func (m *mockLaunchdManager) IsLoaded(label string) (bool, error) {
	return m.loadedLabels[label], nil
}

func (m *mockLaunchdManager) LoadPlist(plistPath string) error {
	m.loadCalls = append(m.loadCalls, plistPath)
	return nil
}

func (m *mockLaunchdManager) UnloadPlist(plistPath string) error {
	m.unloadCalls = append(m.unloadCalls, plistPath)
	return nil
}

func (m *mockLaunchdManager) RemovePlist(plistPath string) error {
	m.removeCalls = append(m.removeCalls, plistPath)
	return nil
}

func (m *mockLaunchdManager) ListAgencCronJobs() ([]string, error) {
	return nil, nil
}

func (m *mockLaunchdManager) RemoveJobByLabel(label string) error {
	return nil
}

func TestSyncCronJob_UnchangedContentSkipsReload(t *testing.T) {
	agencDir := t.TempDir()
	plistDir := t.TempDir()

	mock := newMockManager()
	syncer := newCronSyncerWithManager(agencDir, mock)

	cronCfg := config.CronConfig{
		ID:       "test-uuid-1234",
		Schedule: "0 9 * * *",
		Prompt:   "do stuff",
		Repo:     "github.com/owner/repo",
	}

	testLog := &syncerTestLogger{}

	// First sync: should write plist and call LoadPlist
	err := syncer.syncCronJob("test-job", cronCfg, plistDir, "/usr/bin/agenc", testLog)
	if err != nil {
		t.Fatalf("first sync failed: %v", err)
	}
	if len(mock.loadCalls) != 1 {
		t.Fatalf("expected 1 load call after first sync, got %d", len(mock.loadCalls))
	}

	// Mark as loaded for second sync, reset call tracking
	mock.loadCalls = nil
	mock.loadedLabels[launchd.CronToLabel("test-uuid-1234")] = true

	// Second sync with same config: should NOT call load or unload
	err = syncer.syncCronJob("test-job", cronCfg, plistDir, "/usr/bin/agenc", testLog)
	if err != nil {
		t.Fatalf("second sync failed: %v", err)
	}
	if len(mock.loadCalls) != 0 {
		t.Errorf("expected 0 load calls on unchanged sync, got %d", len(mock.loadCalls))
	}
	if len(mock.unloadCalls) != 0 {
		t.Errorf("expected 0 unload calls on unchanged sync, got %d", len(mock.unloadCalls))
	}
}

func TestSyncCronJob_ContentChangeTriggersReload(t *testing.T) {
	agencDir := t.TempDir()
	plistDir := t.TempDir()

	mock := newMockManager()
	syncer := newCronSyncerWithManager(agencDir, mock)

	cronCfg := config.CronConfig{
		ID:       "test-uuid-1234",
		Schedule: "0 9 * * *",
		Prompt:   "original prompt",
		Repo:     "github.com/owner/repo",
	}

	testLog := &syncerTestLogger{}

	// First sync
	err := syncer.syncCronJob("test-job", cronCfg, plistDir, "/usr/bin/agenc", testLog)
	if err != nil {
		t.Fatalf("first sync failed: %v", err)
	}

	// Mark as loaded, reset call tracking
	mock.loadedLabels[launchd.CronToLabel("test-uuid-1234")] = true
	mock.loadCalls = nil
	mock.unloadCalls = nil

	// Change the prompt
	cronCfg.Prompt = "updated prompt"

	// Second sync: should unload + load (reload)
	err = syncer.syncCronJob("test-job", cronCfg, plistDir, "/usr/bin/agenc", testLog)
	if err != nil {
		t.Fatalf("second sync failed: %v", err)
	}
	if len(mock.unloadCalls) != 1 {
		t.Errorf("expected 1 unload call on content change, got %d", len(mock.unloadCalls))
	}
	if len(mock.loadCalls) != 1 {
		t.Errorf("expected 1 load call on content change, got %d", len(mock.loadCalls))
	}
}

func TestSyncCronJob_NewCronWritesAndLoads(t *testing.T) {
	agencDir := t.TempDir()
	plistDir := t.TempDir()

	mock := newMockManager()
	syncer := newCronSyncerWithManager(agencDir, mock)

	cronCfg := config.CronConfig{
		ID:       "new-uuid-5678",
		Schedule: "0 12 * * *",
		Prompt:   "new job prompt",
	}

	testLog := &syncerTestLogger{}

	err := syncer.syncCronJob("new-job", cronCfg, plistDir, "/usr/bin/agenc", testLog)
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}

	// Should have written the plist file
	plistPath := filepath.Join(plistDir, launchd.CronToPlistFilename("new-uuid-5678"))
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		t.Error("expected plist file to be written")
	}

	// Should have called LoadPlist
	if len(mock.loadCalls) != 1 {
		t.Errorf("expected 1 load call, got %d", len(mock.loadCalls))
	}
}

// syncerTestLogger is a minimal logger for testing cron syncer.
type syncerTestLogger struct{}

func (l *syncerTestLogger) Printf(format string, v ...any) {}
