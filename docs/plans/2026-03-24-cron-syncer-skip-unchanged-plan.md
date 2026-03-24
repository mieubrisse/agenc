Cron Syncer Skip-Unchanged Implementation Plan
================================================

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Only write plist files and call launchctl when cron config actually changed, and ensure content changes propagate via unload+reload.

**Architecture:** Extract a `launchdManager` interface from the concrete `*launchd.Manager` so the cron syncer can be tested with a mock. Rewrite the sync loop to compare generated XML against the existing file on disk, skip writes when unchanged, and unload+reload when content differs.

**Tech Stack:** Go, macOS launchd, `bytes.Equal` for comparison

---

### Task 1: Extract launchdManager interface

**Files:**
- Modify: `internal/server/cron_syncer.go`

**Step 1: Define the interface and update the struct**

Add a `launchdManager` interface above the `CronSyncer` struct with the 4 methods used by the syncer: `IsLoaded`, `LoadPlist`, `UnloadPlist`, `RemovePlist`. Change the `manager` field from `*launchd.Manager` to `launchdManager`.

```go
// launchdManager is the interface for launchd operations used by CronSyncer.
// Extracted from *launchd.Manager to enable testing with mocks.
type launchdManager interface {
	IsLoaded(label string) (bool, error)
	LoadPlist(plistPath string) error
	UnloadPlist(plistPath string) error
	RemovePlist(plistPath string) error
}

// CronSyncer manages synchronization of cron jobs to launchd plists.
type CronSyncer struct {
	agencDirpath string
	manager      launchdManager
}
```

`NewCronSyncer` stays the same — `*launchd.Manager` satisfies the interface.

**Step 2: Run checks**

Run: `make check`
Expected: All pass (no behavior change)

**Step 3: Commit**

```
git add internal/server/cron_syncer.go
git commit -m "Extract launchdManager interface from CronSyncer for testability"
```

---

### Task 2: Rewrite sync loop with content comparison

**Files:**
- Modify: `internal/server/cron_syncer.go`

**Step 1: Add `bytes` and update the sync loop**

Add `"bytes"` to the imports. Replace the section from `// Write the plist to disk` through the end of the enabled/disabled handling block (lines 105-142 in the current file) with the new logic:

```go
		// Generate plist XML
		xmlData, err := plist.GeneratePlistXML()
		if err != nil {
			logger.Printf("Cron syncer: failed to generate plist XML for '%s': %v", name, err)
			continue
		}

		// Compare against existing file on disk
		contentChanged := true
		existingData, err := os.ReadFile(plistPath)
		if err == nil {
			contentChanged = !bytes.Equal(xmlData, existingData)
		}
		// If ReadFile fails (file doesn't exist, permissions), treat as changed

		// Write plist only if content changed
		if contentChanged {
			if err := os.WriteFile(plistPath, xmlData, 0644); err != nil {
				logger.Printf("Cron syncer: failed to write plist for '%s': %v", name, err)
				continue
			}
		}

		// Handle enabled/disabled state
		if cronCfg.IsEnabled() {
			loaded, err := s.manager.IsLoaded(label)
			if err != nil {
				logger.Printf("Cron syncer: failed to check if '%s' is loaded: %v", name, err)
				continue
			}

			if !loaded {
				if err := s.manager.LoadPlist(plistPath); err != nil {
					logger.Printf("Cron syncer: failed to load plist for '%s': %v", name, err)
					continue
				}
				logger.Printf("Cron syncer: loaded plist for '%s'", name)
			} else if contentChanged {
				// Content changed and job is already loaded — unload and reload
				if err := s.manager.UnloadPlist(plistPath); err != nil {
					logger.Printf("Cron syncer: failed to unload plist for '%s' during reload: %v", name, err)
				}
				if err := s.manager.LoadPlist(plistPath); err != nil {
					logger.Printf("Cron syncer: failed to reload plist for '%s': %v", name, err)
					continue
				}
				logger.Printf("Cron syncer: reloaded plist for '%s' (content changed)", name)
			}
		} else {
			// Disabled: unload if loaded, keep file
			loaded, err := s.manager.IsLoaded(label)
			if err != nil {
				logger.Printf("Cron syncer: failed to check if '%s' is loaded: %v", name, err)
				continue
			}

			if loaded {
				if err := s.manager.UnloadPlist(plistPath); err != nil {
					logger.Printf("Cron syncer: failed to unload plist for '%s': %v", name, err)
					continue
				}
				logger.Printf("Cron syncer: unloaded plist for '%s'", name)
			}
		}
```

Also remove the `plist.WriteToDisk` call (the new code uses `os.WriteFile` directly since we already have the XML bytes).

**Step 2: Run checks**

Run: `make check`
Expected: All pass

**Step 3: Commit**

```
git add internal/server/cron_syncer.go
git commit -m "Skip unchanged plist writes and reload on content change"
```

---

### Task 3: Add mock and tests for sync behavior

**Files:**
- Modify: `internal/server/cron_syncer_test.go`

**Step 1: Add a `newCronSyncerWithManager` constructor**

In `cron_syncer.go`, add a package-internal constructor that accepts the interface:

```go
// newCronSyncerWithManager creates a CronSyncer with a custom manager (for testing).
func newCronSyncerWithManager(agencDirpath string, manager launchdManager) *CronSyncer {
	return &CronSyncer{
		agencDirpath: agencDirpath,
		manager:      manager,
	}
}
```

**Step 2: Write the mock and tests**

In `cron_syncer_test.go`, add a mock manager and three test cases:

```go
// mockLaunchdManager records calls for verification.
type mockLaunchdManager struct {
	loadedLabels map[string]bool // labels currently "loaded"
	loadCalls    []string        // plist paths passed to LoadPlist
	unloadCalls  []string        // plist paths passed to UnloadPlist
	removeCalls  []string        // plist paths passed to RemovePlist
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
	// Extract label from path for tracking
	// Mark as loaded using the filename-derived label
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
```

Test 1 — **First sync loads, second sync with same config skips:**

```go
func TestSyncCrons_UnchangedContentSkipsReload(t *testing.T) {
	agencDir := t.TempDir()
	plistDir := t.TempDir()

	mock := newMockManager()
	syncer := newCronSyncerWithManager(agencDir, mock)

	crons := map[string]config.CronConfig{
		"test-job": {
			ID:       "test-uuid-1234",
			Schedule: "0 9 * * *",
			Prompt:   "do stuff",
			Repo:     "github.com/owner/repo",
		},
	}

	testLog := &syncerTestLogger{}

	// First sync: should write plist and call LoadPlist
	err := syncer.syncCronJob("test-job", crons["test-job"], plistDir, "/usr/bin/agenc", testLog)
	if err != nil {
		t.Fatalf("first sync failed: %v", err)
	}
	if len(mock.loadCalls) != 1 {
		t.Fatalf("expected 1 load call after first sync, got %d", len(mock.loadCalls))
	}

	// Mark as loaded for second sync
	mock.loadCalls = nil
	mock.loadedLabels["agenc-cron-test-job"] = true

	// Second sync with same config: should NOT call load or unload
	err = syncer.syncCronJob("test-job", crons["test-job"], plistDir, "/usr/bin/agenc", testLog)
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
```

Test 2 — **Content change triggers unload+reload:**

```go
func TestSyncCrons_ContentChangeTriggersReload(t *testing.T) {
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
	mock.loadedLabels["agenc-cron-test-job"] = true
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
```

Test 3 — **New cron with no existing plist writes and loads:**

```go
func TestSyncCrons_NewCronWritesAndLoads(t *testing.T) {
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
	plistPath := filepath.Join(plistDir, "agenc-cron-new-job.plist")
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		t.Error("expected plist file to be written")
	}

	// Should have called LoadPlist
	if len(mock.loadCalls) != 1 {
		t.Errorf("expected 1 load call, got %d", len(mock.loadCalls))
	}
}
```

Note: The tests call a `syncCronJob` method that doesn't exist yet. This is intentional — we need to extract the per-cron logic from the main loop into a testable method.

**Step 3: Extract `syncCronJob` from the main loop**

In `cron_syncer.go`, extract the body of the `for name, cronCfg := range crons` loop into:

```go
// syncCronJob synchronizes a single cron job's plist to disk and manages its
// launchd load state. Returns an error only for fatal failures; recoverable
// issues are logged and skipped.
func (s *CronSyncer) syncCronJob(name string, cronCfg config.CronConfig, plistDirpath string, execPath string, logger logger) error {
	// ... the per-cron logic extracted from SyncCronsToLaunchd
}
```

Update `SyncCronsToLaunchd` to call `syncCronJob` in its loop, logging and continuing on error.

**Step 4: Run checks**

Run: `make check`
Expected: All pass

**Step 5: Commit**

```
git add internal/server/cron_syncer.go internal/server/cron_syncer_test.go
git commit -m "Add mock-based tests for cron syncer content-change detection"
```

---

### Task 4: Update architecture docs

**Files:**
- Modify: `docs/system-architecture.md`

**Step 1: Update the cron scheduling section**

Find the cron scheduling section and add a note about the content-comparison optimization: the syncer compares generated XML against the existing file and only writes + reloads when content differs.

**Step 2: Commit**

```
git add docs/system-architecture.md
git commit -m "Document cron syncer content-comparison optimization in architecture docs"
```
