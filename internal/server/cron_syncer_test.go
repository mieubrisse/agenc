package server

import (
	"errors"
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
	loadedLabels    map[string]bool
	loadCalls       []string
	unloadCalls     []string
	removeCalls     []string
	removeJobCalls  []string
	loadedJobLabels []string // returned by ListAgencCronJobs

	// Configurable errors for testing failure paths
	unloadErr    error
	removeJobErr error
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
	return m.unloadErr
}

func (m *mockLaunchdManager) RemovePlist(plistPath string) error {
	m.removeCalls = append(m.removeCalls, plistPath)
	return nil
}

func (m *mockLaunchdManager) ListAgencCronJobs(_ string) ([]string, error) {
	return m.loadedJobLabels, nil
}

func (m *mockLaunchdManager) RemoveJobByLabel(label string) error {
	m.removeJobCalls = append(m.removeJobCalls, label)
	return m.removeJobErr
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
	mock.loadedLabels[launchd.CronToLabel(syncer.cronPlistPrefix, "test-uuid-1234")] = true

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
	mock.loadedLabels[launchd.CronToLabel(syncer.cronPlistPrefix, "test-uuid-1234")] = true
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

func TestSyncCronJob_UnloadFailsFallsBackToRemoveJobByLabel(t *testing.T) {
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

	// First sync to write plist and load
	err := syncer.syncCronJob("test-job", cronCfg, plistDir, "/usr/bin/agenc", testLog)
	if err != nil {
		t.Fatalf("first sync failed: %v", err)
	}

	// Mark as loaded, configure unload to fail, reset tracking
	mock.loadedLabels[launchd.CronToLabel(syncer.cronPlistPrefix, "test-uuid-1234")] = true
	mock.unloadErr = errors.New("simulated unload failure")
	mock.loadCalls = nil
	mock.unloadCalls = nil
	mock.removeJobCalls = nil

	// Change prompt to trigger reload
	cronCfg.Prompt = "updated prompt"

	// Sync should succeed via RemoveJobByLabel fallback
	err = syncer.syncCronJob("test-job", cronCfg, plistDir, "/usr/bin/agenc", testLog)
	if err != nil {
		t.Fatalf("sync with unload failure should succeed via fallback: %v", err)
	}
	if len(mock.unloadCalls) != 1 {
		t.Errorf("expected 1 unload attempt, got %d", len(mock.unloadCalls))
	}
	if len(mock.removeJobCalls) != 1 {
		t.Errorf("expected 1 RemoveJobByLabel fallback call, got %d", len(mock.removeJobCalls))
	}
	if len(mock.loadCalls) != 1 {
		t.Errorf("expected 1 load call after fallback, got %d", len(mock.loadCalls))
	}
}

func TestSyncCronJob_UnloadAndRemoveBothFail(t *testing.T) {
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

	// Mark as loaded, configure both unload and remove to fail
	mock.loadedLabels[launchd.CronToLabel(syncer.cronPlistPrefix, "test-uuid-1234")] = true
	mock.unloadErr = errors.New("unload failed")
	mock.removeJobErr = errors.New("remove failed")
	mock.loadCalls = nil

	// Change prompt to trigger reload
	cronCfg.Prompt = "updated prompt"

	// Sync should fail — both unload paths exhausted
	err = syncer.syncCronJob("test-job", cronCfg, plistDir, "/usr/bin/agenc", testLog)
	if err == nil {
		t.Fatal("expected error when both unload and remove fail")
	}
	// LoadPlist should NOT have been called
	if len(mock.loadCalls) != 0 {
		t.Errorf("expected 0 load calls when removal fails, got %d", len(mock.loadCalls))
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
	plistPath := filepath.Join(plistDir, launchd.CronToPlistFilename(syncer.cronPlistPrefix, "new-uuid-5678"))
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		t.Error("expected plist file to be written")
	}

	// Should have called LoadPlist
	if len(mock.loadCalls) != 1 {
		t.Errorf("expected 1 load call, got %d", len(mock.loadCalls))
	}
}

func TestSyncCronJob_DisabledCronUnloads(t *testing.T) {
	agencDir := t.TempDir()
	plistDir := t.TempDir()

	mock := newMockManager()
	syncer := newCronSyncerWithManager(agencDir, mock)

	enabled := false
	cronCfg := config.CronConfig{
		ID:       "disabled-uuid",
		Schedule: "0 9 * * *",
		Prompt:   "disabled job",
		Enabled:  &enabled,
	}

	testLog := &syncerTestLogger{}

	// First sync with enabled to create the plist
	enabledCfg := cronCfg
	enabledVal := true
	enabledCfg.Enabled = &enabledVal
	err := syncer.syncCronJob("test-job", enabledCfg, plistDir, "/usr/bin/agenc", testLog)
	if err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	// Mark as loaded, reset tracking
	mock.loadedLabels[launchd.CronToLabel(syncer.cronPlistPrefix, "disabled-uuid")] = true
	mock.loadCalls = nil
	mock.unloadCalls = nil

	// Sync with disabled config: should unload
	err = syncer.syncCronJob("test-job", cronCfg, plistDir, "/usr/bin/agenc", testLog)
	if err != nil {
		t.Fatalf("disabled sync failed: %v", err)
	}
	if len(mock.unloadCalls) != 1 {
		t.Errorf("expected 1 unload call for disabled cron, got %d", len(mock.unloadCalls))
	}
	if len(mock.loadCalls) != 0 {
		t.Errorf("expected 0 load calls for disabled cron, got %d", len(mock.loadCalls))
	}
}

func TestSyncCronJob_DisabledNotLoadedIsNoop(t *testing.T) {
	agencDir := t.TempDir()
	plistDir := t.TempDir()

	mock := newMockManager()
	syncer := newCronSyncerWithManager(agencDir, mock)

	enabled := false
	cronCfg := config.CronConfig{
		ID:       "disabled-uuid",
		Schedule: "0 9 * * *",
		Prompt:   "disabled job",
		Enabled:  &enabled,
	}

	testLog := &syncerTestLogger{}

	// Sync disabled cron that's not loaded: should not call unload or load
	err := syncer.syncCronJob("test-job", cronCfg, plistDir, "/usr/bin/agenc", testLog)
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if len(mock.unloadCalls) != 0 {
		t.Errorf("expected 0 unload calls, got %d", len(mock.unloadCalls))
	}
	if len(mock.loadCalls) != 0 {
		t.Errorf("expected 0 load calls, got %d", len(mock.loadCalls))
	}
}

func TestRemoveOrphanedLaunchdJobs(t *testing.T) {
	agencDir := t.TempDir()

	mock := newMockManager()
	syncer := newCronSyncerWithManager(agencDir, mock)

	// Simulate launchd having jobs for known UUID, unknown UUID, and legacy label
	mock.loadedJobLabels = []string{
		launchd.CronToLabel(syncer.cronPlistPrefix, "known-uuid"),
		launchd.CronToLabel(syncer.cronPlistPrefix, "orphan-uuid"),
		"agenc-cron-legacy-name",
	}

	crons := map[string]config.CronConfig{
		"my-cron": {ID: "known-uuid", Schedule: "0 9 * * *", Prompt: "test"},
	}

	testLog := &syncerTestLogger{}
	err := syncer.removeOrphanedLaunchdJobs(crons, testLog)
	if err != nil {
		t.Fatalf("removeOrphanedLaunchdJobs failed: %v", err)
	}

	// Should have removed the orphan UUID and legacy label, but NOT the known one
	if len(mock.removeJobCalls) != 2 {
		t.Fatalf("expected 2 removeJobByLabel calls, got %d: %v", len(mock.removeJobCalls), mock.removeJobCalls)
	}

	removed := make(map[string]bool)
	for _, label := range mock.removeJobCalls {
		removed[label] = true
	}

	if !removed[launchd.CronToLabel(syncer.cronPlistPrefix, "orphan-uuid")] {
		t.Error("expected orphan-uuid to be removed")
	}
	if !removed["agenc-cron-legacy-name"] {
		t.Error("expected legacy label to be removed")
	}
	if removed[launchd.CronToLabel(syncer.cronPlistPrefix, "known-uuid")] {
		t.Error("known-uuid should NOT have been removed")
	}
}

// syncerTestLogger is a minimal logger for testing cron syncer.
type syncerTestLogger struct{}

func (l *syncerTestLogger) Printf(format string, v ...any) {}
