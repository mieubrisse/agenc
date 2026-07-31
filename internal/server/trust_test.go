package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

// trustEntryFromFile reads claudeJSONFilepath and returns the projects entry for
// agentDirpath as a parsed map. Returns nil if absent.
func trustEntryFromFile(t *testing.T, claudeJSONFilepath string, agentDirpath string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(claudeJSONFilepath)
	if err != nil {
		t.Fatalf("trustEntryFromFile: failed to read '%s': %v", claudeJSONFilepath, err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("trustEntryFromFile: failed to parse root JSON: %v", err)
	}
	projectsRaw, ok := root["projects"]
	if !ok {
		return nil
	}
	var projects map[string]json.RawMessage
	if err := json.Unmarshal(projectsRaw, &projects); err != nil {
		t.Fatalf("trustEntryFromFile: failed to parse projects: %v", err)
	}
	entryRaw, ok := projects[agentDirpath]
	if !ok {
		return nil
	}
	var entry map[string]interface{}
	if err := json.Unmarshal(entryRaw, &entry); err != nil {
		t.Fatalf("trustEntryFromFile: failed to parse entry: %v", err)
	}
	return entry
}

// rootKeysFromFile returns the set of top-level keys in claudeJSONFilepath.
func rootKeysFromFile(t *testing.T, claudeJSONFilepath string) map[string]struct{} {
	t.Helper()
	data, err := os.ReadFile(claudeJSONFilepath)
	if err != nil {
		t.Fatalf("rootKeysFromFile: failed to read '%s': %v", claudeJSONFilepath, err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("rootKeysFromFile: failed to parse JSON: %v", err)
	}
	keys := make(map[string]struct{}, len(root))
	for k := range root {
		keys[k] = struct{}{}
	}
	return keys
}

// projectKeysFromFile returns the set of keys under "projects" in claudeJSONFilepath.
func projectKeysFromFile(t *testing.T, claudeJSONFilepath string) map[string]struct{} {
	t.Helper()
	data, err := os.ReadFile(claudeJSONFilepath)
	if err != nil {
		t.Fatalf("projectKeysFromFile: failed to read '%s': %v", claudeJSONFilepath, err)
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("projectKeysFromFile: failed to parse root JSON: %v", err)
	}
	projectsRaw, ok := root["projects"]
	if !ok {
		return map[string]struct{}{}
	}
	var projects map[string]json.RawMessage
	if err := json.Unmarshal(projectsRaw, &projects); err != nil {
		t.Fatalf("projectKeysFromFile: failed to parse projects: %v", err)
	}
	keys := make(map[string]struct{}, len(projects))
	for k := range projects {
		keys[k] = struct{}{}
	}
	return keys
}

// assertNoTempFiles fails the test if any .claude.json.tmp-* files exist in dir.
func assertNoTempFiles(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".claude.json.tmp-*"))
	if err != nil {
		t.Fatalf("assertNoTempFiles: glob error: %v", err)
	}
	if len(matches) > 0 {
		t.Errorf("leftover temp files in '%s': %v", dir, matches)
	}
}

// TestWriteTrustEntry_AbsentFile verifies that writeTrustEntry creates the file
// from scratch when it does not exist.
func TestWriteTrustEntry_AbsentFile(t *testing.T) {
	dir := t.TempDir()
	claudeJSON := filepath.Join(dir, ".claude.json")
	agentDir := "/home/user/.agenc/missions/abc/agent"

	if err := writeTrustEntry(claudeJSON, agentDir, nil); err != nil {
		t.Fatalf("writeTrustEntry: unexpected error: %v", err)
	}

	entry := trustEntryFromFile(t, claudeJSON, agentDir)
	if entry == nil {
		t.Fatal("expected trust entry to exist after writeTrustEntry")
	}
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Errorf("hasTrustDialogAccepted: expected true, got %v", entry["hasTrustDialogAccepted"])
	}

	assertNoTempFiles(t, dir)
}

// TestWriteTrustEntry_Preservation verifies that other top-level keys and other
// projects entries are preserved byte-equivalent after writeTrustEntry.
func TestWriteTrustEntry_Preservation(t *testing.T) {
	dir := t.TempDir()
	claudeJSON := filepath.Join(dir, ".claude.json")
	agentDir := "/home/user/.agenc/missions/abc/agent"
	otherProject := "/home/user/other-project"

	// Seed the file with a telemetry key, an unrelated key, and an existing project entry.
	initial := map[string]interface{}{
		"telemetry":    map[string]interface{}{"enabled": false},
		"someOtherKey": "someValue",
		"projects": map[string]interface{}{
			otherProject: map[string]interface{}{
				"hasTrustDialogAccepted": true,
				"customField":            "preserved",
			},
		},
	}
	data, err := json.MarshalIndent(initial, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal initial file: %v", err)
	}
	if err := os.WriteFile(claudeJSON, append(data, '\n'), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	if err := writeTrustEntry(claudeJSON, agentDir, nil); err != nil {
		t.Fatalf("writeTrustEntry: unexpected error: %v", err)
	}

	// New entry must be present.
	entry := trustEntryFromFile(t, claudeJSON, agentDir)
	if entry == nil {
		t.Fatal("expected new trust entry to exist")
	}
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Errorf("hasTrustDialogAccepted: expected true, got %v", entry["hasTrustDialogAccepted"])
	}

	// Other top-level keys must survive.
	rootKeys := rootKeysFromFile(t, claudeJSON)
	for _, k := range []string{"telemetry", "someOtherKey", "projects"} {
		if _, ok := rootKeys[k]; !ok {
			t.Errorf("expected top-level key '%s' to survive, but it was lost", k)
		}
	}

	// Other projects entry must survive.
	projectKeys := projectKeysFromFile(t, claudeJSON)
	if _, ok := projectKeys[otherProject]; !ok {
		t.Errorf("expected other project entry '%s' to survive, but it was lost", otherProject)
	}

	// Both the new and old project must be present.
	if _, ok := projectKeys[agentDir]; !ok {
		t.Errorf("expected new project entry '%s' to exist, but it is absent", agentDir)
	}

	// The other project's customField must survive byte-equivalent.
	otherEntry := trustEntryFromFile(t, claudeJSON, otherProject)
	if otherEntry == nil {
		t.Fatal("other project entry was lost entirely")
	}
	if cf, _ := otherEntry["customField"].(string); cf != "preserved" {
		t.Errorf("other project customField: expected 'preserved', got %v", otherEntry["customField"])
	}

	assertNoTempFiles(t, dir)
}

// TestWriteTrustEntry_Idempotent verifies that calling writeTrustEntry twice
// yields the same result and does not error.
func TestWriteTrustEntry_Idempotent(t *testing.T) {
	dir := t.TempDir()
	claudeJSON := filepath.Join(dir, ".claude.json")
	agentDir := "/home/user/.agenc/missions/abc/agent"

	for i := 0; i < 2; i++ {
		if err := writeTrustEntry(claudeJSON, agentDir, nil); err != nil {
			t.Fatalf("writeTrustEntry attempt %d: unexpected error: %v", i+1, err)
		}
	}

	entry := trustEntryFromFile(t, claudeJSON, agentDir)
	if entry == nil {
		t.Fatal("expected trust entry to exist after two writes")
	}
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Errorf("hasTrustDialogAccepted: expected true, got %v", entry["hasTrustDialogAccepted"])
	}

	assertNoTempFiles(t, dir)
}

// TestWriteTrustEntry_TrustedMcpServers_All verifies that when trustedMcpServers.All
// is true, the entry includes empty enabledMcpjsonServers and disabledMcpjsonServers.
func TestWriteTrustEntry_TrustedMcpServers_All(t *testing.T) {
	dir := t.TempDir()
	claudeJSON := filepath.Join(dir, ".claude.json")
	agentDir := "/home/user/.agenc/missions/abc/agent"

	trusted := &config.TrustedMcpServers{All: true}
	if err := writeTrustEntry(claudeJSON, agentDir, trusted); err != nil {
		t.Fatalf("writeTrustEntry: unexpected error: %v", err)
	}

	entry := trustEntryFromFile(t, claudeJSON, agentDir)
	if entry == nil {
		t.Fatal("expected trust entry to exist")
	}
	if v, _ := entry["hasTrustDialogAccepted"].(bool); !v {
		t.Errorf("hasTrustDialogAccepted: expected true")
	}

	// When All=true, both lists must be present and empty.
	enabled, ok := entry["enabledMcpjsonServers"]
	if !ok {
		t.Error("expected enabledMcpjsonServers key")
	} else {
		list, _ := enabled.([]interface{})
		if len(list) != 0 {
			t.Errorf("enabledMcpjsonServers: expected empty list, got %v", list)
		}
	}
	disabled, ok := entry["disabledMcpjsonServers"]
	if !ok {
		t.Error("expected disabledMcpjsonServers key")
	} else {
		list, _ := disabled.([]interface{})
		if len(list) != 0 {
			t.Errorf("disabledMcpjsonServers: expected empty list, got %v", list)
		}
	}

	assertNoTempFiles(t, dir)
}

// TestWriteTrustEntry_TrustedMcpServers_List verifies that when trustedMcpServers
// contains a named list, enabledMcpjsonServers is set to that list and
// disabledMcpjsonServers is empty.
func TestWriteTrustEntry_TrustedMcpServers_List(t *testing.T) {
	dir := t.TempDir()
	claudeJSON := filepath.Join(dir, ".claude.json")
	agentDir := "/home/user/.agenc/missions/abc/agent"

	trusted := &config.TrustedMcpServers{List: []string{"github", "sentry"}}
	if err := writeTrustEntry(claudeJSON, agentDir, trusted); err != nil {
		t.Fatalf("writeTrustEntry: unexpected error: %v", err)
	}

	entry := trustEntryFromFile(t, claudeJSON, agentDir)
	if entry == nil {
		t.Fatal("expected trust entry to exist")
	}

	enabledRaw, ok := entry["enabledMcpjsonServers"]
	if !ok {
		t.Fatal("expected enabledMcpjsonServers key")
	}
	enabled, _ := enabledRaw.([]interface{})
	if len(enabled) != 2 {
		t.Fatalf("enabledMcpjsonServers: expected 2 entries, got %d: %v", len(enabled), enabled)
	}
	if enabled[0].(string) != "github" || enabled[1].(string) != "sentry" {
		t.Errorf("enabledMcpjsonServers: expected [github sentry], got %v", enabled)
	}

	disabledRaw, ok := entry["disabledMcpjsonServers"]
	if !ok {
		t.Fatal("expected disabledMcpjsonServers key")
	}
	disabled, _ := disabledRaw.([]interface{})
	if len(disabled) != 0 {
		t.Errorf("disabledMcpjsonServers: expected empty list, got %v", disabled)
	}

	assertNoTempFiles(t, dir)
}

// TestWriteTrustEntry_AtomicityMarker verifies no leftover temp files remain
// after a successful write.
func TestWriteTrustEntry_AtomicityMarker(t *testing.T) {
	dir := t.TempDir()
	claudeJSON := filepath.Join(dir, ".claude.json")
	agentDir := "/home/user/.agenc/missions/abc/agent"

	if err := writeTrustEntry(claudeJSON, agentDir, nil); err != nil {
		t.Fatalf("writeTrustEntry: unexpected error: %v", err)
	}

	assertNoTempFiles(t, dir)
}

// TestPruneTrustEntry_RemovesEntry verifies that pruneTrustEntry removes only
// the targeted entry while preserving all other content.
func TestPruneTrustEntry_RemovesEntry(t *testing.T) {
	dir := t.TempDir()
	claudeJSON := filepath.Join(dir, ".claude.json")
	agentDir := "/home/user/.agenc/missions/abc/agent"
	otherProject := "/home/user/other-project"

	// Seed the file with two project entries and an extra top-level key.
	initial := map[string]interface{}{
		"telemetry": map[string]interface{}{"enabled": true},
		"projects": map[string]interface{}{
			agentDir:     map[string]interface{}{"hasTrustDialogAccepted": true},
			otherProject: map[string]interface{}{"hasTrustDialogAccepted": true, "extra": "data"},
		},
	}
	data, err := json.MarshalIndent(initial, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal initial file: %v", err)
	}
	if err := os.WriteFile(claudeJSON, append(data, '\n'), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	if err := pruneTrustEntry(claudeJSON, agentDir); err != nil {
		t.Fatalf("pruneTrustEntry: unexpected error: %v", err)
	}

	// Targeted entry must be gone.
	projectKeys := projectKeysFromFile(t, claudeJSON)
	if _, ok := projectKeys[agentDir]; ok {
		t.Errorf("expected entry for '%s' to be removed, but it is still present", agentDir)
	}

	// Other project entry must survive.
	if _, ok := projectKeys[otherProject]; !ok {
		t.Errorf("expected other project entry '%s' to survive prune, but it was lost", otherProject)
	}

	// Top-level telemetry key must survive.
	rootKeys := rootKeysFromFile(t, claudeJSON)
	if _, ok := rootKeys["telemetry"]; !ok {
		t.Error("expected 'telemetry' top-level key to survive prune")
	}

	assertNoTempFiles(t, dir)
}

// TestPruneTrustEntry_AbsentEntry verifies that pruning an absent entry is a no-op.
func TestPruneTrustEntry_AbsentEntry(t *testing.T) {
	dir := t.TempDir()
	claudeJSON := filepath.Join(dir, ".claude.json")
	agentDir := "/home/user/.agenc/missions/abc/agent"

	// File exists but has no entry for agentDir.
	initial := map[string]interface{}{
		"projects": map[string]interface{}{
			"/some/other": map[string]interface{}{"hasTrustDialogAccepted": true},
		},
	}
	data, err := json.MarshalIndent(initial, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal initial file: %v", err)
	}
	if err := os.WriteFile(claudeJSON, append(data, '\n'), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	if err := pruneTrustEntry(claudeJSON, agentDir); err != nil {
		t.Fatalf("pruneTrustEntry on absent entry: unexpected error: %v", err)
	}

	// Other entry must still be present.
	if _, ok := projectKeysFromFile(t, claudeJSON)["/some/other"]; !ok {
		t.Error("expected '/some/other' project entry to survive no-op prune")
	}
}

// TestPruneTrustEntry_AbsentFile verifies that pruning when the file does not exist
// is a no-op (returns nil).
func TestPruneTrustEntry_AbsentFile(t *testing.T) {
	dir := t.TempDir()
	claudeJSON := filepath.Join(dir, ".claude.json")
	agentDir := "/home/user/.agenc/missions/abc/agent"

	if err := pruneTrustEntry(claudeJSON, agentDir); err != nil {
		t.Fatalf("pruneTrustEntry on absent file: unexpected error: %v", err)
	}
}

// TestWriteThenPrune verifies the full write-then-prune round-trip, ensuring
// that prune after seed leaves the file without the targeted entry but
// preserves everything else.
func TestWriteThenPrune(t *testing.T) {
	dir := t.TempDir()
	claudeJSON := filepath.Join(dir, ".claude.json")
	agentDir := "/home/user/.agenc/missions/abc/agent"
	otherProject := "/home/user/other"

	// Seed an existing project entry.
	initial := map[string]interface{}{
		"projects": map[string]interface{}{
			otherProject: map[string]interface{}{"hasTrustDialogAccepted": true},
		},
	}
	data, _ := json.MarshalIndent(initial, "", "  ")
	if err := os.WriteFile(claudeJSON, append(data, '\n'), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	// Write the trust entry.
	if err := writeTrustEntry(claudeJSON, agentDir, nil); err != nil {
		t.Fatalf("writeTrustEntry: unexpected error: %v", err)
	}

	// Confirm it's present.
	if entry := trustEntryFromFile(t, claudeJSON, agentDir); entry == nil {
		t.Fatal("trust entry not found after write")
	}

	// Prune it.
	if err := pruneTrustEntry(claudeJSON, agentDir); err != nil {
		t.Fatalf("pruneTrustEntry: unexpected error: %v", err)
	}

	// Confirm it's gone.
	if entry := trustEntryFromFile(t, claudeJSON, agentDir); entry != nil {
		t.Error("trust entry still present after prune")
	}

	// Other project must survive.
	if _, ok := projectKeysFromFile(t, claudeJSON)[otherProject]; !ok {
		t.Error("other project entry was lost after prune")
	}

	assertNoTempFiles(t, dir)
}

// Note: the concurrent-clobber retry in writeTrustEntry is exercised in
// production (or by Kevin's manual self-test) — deterministically simulating
// a racing writer in a unit test would require injecting a hook between the
// rename and the verify read, which is more complexity than the value warrants.

// --- reconcileTrustEntries tests ---

// TestReconcileTrustEntries_SeedPrunePreserve verifies the three core invariants:
//  1. Existing mission agent dirs get a bare trust entry seeded into projects.
//  2. A stale projects key under the missions prefix that has no corresponding
//     mission is pruned.
//  3. Unrelated projects keys (user repos outside the missions prefix) are preserved.
func TestReconcileTrustEntries_SeedPrunePreserve(t *testing.T) {
	tmpDir := t.TempDir()
	claudeJSONPath := filepath.Join(tmpDir, ".claude.json")
	missionsPrefix := filepath.Join(tmpDir, "missions")

	activeMissionDir := filepath.Join(missionsPrefix, "aaaaaaaa-0000-0000-0000-000000000001", "agent")
	staleMissionDir := filepath.Join(missionsPrefix, "bbbbbbbb-0000-0000-0000-000000000002", "agent")
	userRepoDir := "/home/user/myproject" // outside missionsPrefix — must survive

	// Seed an initial file with a stale mission entry and a user repo entry.
	initial := map[string]interface{}{
		"numStartups": 42,
		"projects": map[string]interface{}{
			staleMissionDir: map[string]interface{}{
				"hasTrustDialogAccepted": true,
			},
			userRepoDir: map[string]interface{}{
				"hasTrustDialogAccepted": true,
				"enabledMcpjsonServers":  []string{"todoist"},
			},
		},
	}
	initialData, err := json.MarshalIndent(initial, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal initial content: %v", err)
	}
	if err := os.WriteFile(claudeJSONPath, append(initialData, '\n'), 0644); err != nil {
		t.Fatalf("failed to write initial claude.json: %v", err)
	}

	// Reconcile with only activeMissionDir as the live mission.
	if err := reconcileTrustEntries(claudeJSONPath, []string{activeMissionDir}, missionsPrefix); err != nil {
		t.Fatalf("reconcileTrustEntries returned error: %v", err)
	}

	// 1. Active mission dir must have hasTrustDialogAccepted=true.
	activeEntry := trustEntryFromFile(t, claudeJSONPath, activeMissionDir)
	if activeEntry == nil {
		t.Errorf("expected projects[%q] to be seeded, but it was absent", activeMissionDir)
	} else if trusted, _ := activeEntry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Errorf("expected hasTrustDialogAccepted=true for active mission, got %v", activeEntry["hasTrustDialogAccepted"])
	}

	// 2. Stale mission dir must be pruned.
	projectKeys := projectKeysFromFile(t, claudeJSONPath)
	if _, present := projectKeys[staleMissionDir]; present {
		t.Errorf("expected stale mission entry %q to be pruned", staleMissionDir)
	}

	// 3. User repo outside missions prefix must be preserved with original content.
	userEntry := trustEntryFromFile(t, claudeJSONPath, userRepoDir)
	if userEntry == nil {
		t.Errorf("expected user repo entry %q to be preserved", userRepoDir)
	} else {
		mcpServers, ok := userEntry["enabledMcpjsonServers"]
		if !ok {
			t.Errorf("expected enabledMcpjsonServers to survive on user repo entry")
		} else {
			servers, _ := mcpServers.([]interface{})
			if len(servers) != 1 || servers[0] != "todoist" {
				t.Errorf("expected enabledMcpjsonServers=[todoist], got %v", mcpServers)
			}
		}
	}

	// 4. Unrelated top-level keys must also be preserved.
	rootKeys := rootKeysFromFile(t, claudeJSONPath)
	if _, ok := rootKeys["numStartups"]; !ok {
		t.Errorf("expected top-level key 'numStartups' to be preserved")
	}

	assertNoTempFiles(t, tmpDir)
}

// TestReconcileTrustEntries_MissingFile verifies that a missing claude.json is
// created with the seeded entries and no other keys.
func TestReconcileTrustEntries_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	claudeJSONPath := filepath.Join(tmpDir, ".claude.json")
	missionsPrefix := filepath.Join(tmpDir, "missions")
	activeMissionDir := filepath.Join(missionsPrefix, "cccccccc-0000-0000-0000-000000000003", "agent")

	if err := reconcileTrustEntries(claudeJSONPath, []string{activeMissionDir}, missionsPrefix); err != nil {
		t.Fatalf("reconcileTrustEntries on missing file returned error: %v", err)
	}

	entry := trustEntryFromFile(t, claudeJSONPath, activeMissionDir)
	if entry == nil {
		t.Fatalf("expected %q to be seeded in new file", activeMissionDir)
	}
	if trusted, _ := entry["hasTrustDialogAccepted"].(bool); !trusted {
		t.Errorf("expected hasTrustDialogAccepted=true in seeded entry")
	}

	assertNoTempFiles(t, tmpDir)
}

// TestReconcileTrustEntries_EmptyMissions verifies that when there are no live
// missions, all missions-prefix entries are pruned while unrelated entries survive.
func TestReconcileTrustEntries_EmptyMissions(t *testing.T) {
	tmpDir := t.TempDir()
	claudeJSONPath := filepath.Join(tmpDir, ".claude.json")
	missionsPrefix := filepath.Join(tmpDir, "missions")
	staleMissionDir := filepath.Join(missionsPrefix, "dddddddd-0000-0000-0000-000000000004", "agent")
	userRepoDir := "/home/user/another-project"

	initial := map[string]interface{}{
		"projects": map[string]interface{}{
			staleMissionDir: map[string]interface{}{"hasTrustDialogAccepted": true},
			userRepoDir:     map[string]interface{}{"hasTrustDialogAccepted": true},
		},
	}
	data, err := json.MarshalIndent(initial, "", "  ")
	if err != nil {
		t.Fatalf("failed to marshal initial content: %v", err)
	}
	if err := os.WriteFile(claudeJSONPath, append(data, '\n'), 0644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	if err := reconcileTrustEntries(claudeJSONPath, []string{}, missionsPrefix); err != nil {
		t.Fatalf("reconcileTrustEntries with empty missions returned error: %v", err)
	}

	projectKeys := projectKeysFromFile(t, claudeJSONPath)
	if _, present := projectKeys[staleMissionDir]; present {
		t.Errorf("expected stale mission %q to be pruned when no missions exist", staleMissionDir)
	}
	if _, present := projectKeys[userRepoDir]; !present {
		t.Errorf("expected user repo %q to be preserved", userRepoDir)
	}

	assertNoTempFiles(t, tmpDir)
}
