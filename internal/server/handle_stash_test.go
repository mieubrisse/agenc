package server

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/odyssey/agenc/internal/config"
)

func TestReadWriteStashFile(t *testing.T) {
	tmpDir := t.TempDir()

	stash := &StashFile{
		CreatedAt: time.Date(2026, 3, 2, 10, 30, 0, 0, time.UTC),
		Missions: []StashMission{
			{MissionID: "mission-1", LinkedSessions: []string{"agenc", "other"}},
			{MissionID: "mission-2", LinkedSessions: nil},
		},
	}

	if err := writeStashFile(tmpDir, "2026-03-02T10-30-00", stash); err != nil {
		t.Fatalf("failed to write stash file: %v", err)
	}

	// Verify stash directory was created
	stashDirpath := config.GetStashDirpath(tmpDir)
	if _, err := os.Stat(stashDirpath); os.IsNotExist(err) {
		t.Fatal("stash directory was not created")
	}

	// Read it back
	stashFilepath := filepath.Join(stashDirpath, "2026-03-02T10-30-00.json")
	result, err := readStashFile(stashFilepath)
	if err != nil {
		t.Fatalf("failed to read stash file: %v", err)
	}

	if !result.CreatedAt.Equal(stash.CreatedAt) {
		t.Errorf("expected created_at %v, got %v", stash.CreatedAt, result.CreatedAt)
	}
	if len(result.Missions) != 2 {
		t.Fatalf("expected 2 missions, got %d", len(result.Missions))
	}
	if result.Missions[0].MissionID != "mission-1" {
		t.Errorf("expected mission-1, got %s", result.Missions[0].MissionID)
	}
	if len(result.Missions[0].LinkedSessions) != 2 {
		t.Errorf("expected 2 linked sessions, got %d", len(result.Missions[0].LinkedSessions))
	}
	if result.Missions[1].MissionID != "mission-2" {
		t.Errorf("expected mission-2, got %s", result.Missions[1].MissionID)
	}
}

func TestFindMostRecentStash(t *testing.T) {
	tmpDir := t.TempDir()
	stashDirpath := config.GetStashDirpath(tmpDir)
	if err := os.MkdirAll(stashDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	// Create stash files with different CreatedAt timestamps
	olderStash := &StashFile{
		CreatedAt: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		Missions:  []StashMission{{MissionID: "m1"}},
	}
	newerStash := &StashFile{
		CreatedAt: time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC),
		Missions:  []StashMission{{MissionID: "m2"}},
	}

	if err := writeStashFile(tmpDir, "older", olderStash); err != nil {
		t.Fatal(err)
	}
	if err := writeStashFile(tmpDir, "newer", newerStash); err != nil {
		t.Fatal(err)
	}

	result, err := findMostRecentStash(stashDirpath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "newer" {
		t.Errorf("expected 'newer', got %q", result)
	}
}

func TestFindMostRecentStash_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()
	stashDirpath := config.GetStashDirpath(tmpDir)
	if err := os.MkdirAll(stashDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	_, err := findMostRecentStash(stashDirpath)
	if err == nil {
		t.Fatal("expected error for empty stash dir")
	}
}

func TestFindMostRecentStash_NonexistentDir(t *testing.T) {
	_, err := findMostRecentStash("/nonexistent/path")
	if err == nil {
		t.Fatal("expected error for nonexistent dir")
	}
}

func TestHandleListStashes_EmptyDir(t *testing.T) {
	tmpDir := t.TempDir()

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/stash", nil)
	w := httptest.NewRecorder()

	err := srv.handleListStashes(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}

	var entries []StashListEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestHandleListStashes_WithFiles(t *testing.T) {
	tmpDir := t.TempDir()
	stashDirpath := config.GetStashDirpath(tmpDir)
	if err := os.MkdirAll(stashDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	// Write two stash files
	older := &StashFile{
		CreatedAt: time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC),
		Missions:  []StashMission{{MissionID: "m1"}},
	}
	newer := &StashFile{
		CreatedAt: time.Date(2026, 3, 2, 10, 0, 0, 0, time.UTC),
		Missions:  []StashMission{{MissionID: "m2"}, {MissionID: "m3"}},
	}

	if err := writeStashFile(tmpDir, "older", older); err != nil {
		t.Fatal(err)
	}
	if err := writeStashFile(tmpDir, "newer", newer); err != nil {
		t.Fatal(err)
	}

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/stash", nil)
	w := httptest.NewRecorder()

	err := srv.handleListStashes(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var entries []StashListEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Should be sorted most recent first
	if entries[0].StashID != "newer" {
		t.Errorf("expected first entry to be 'newer', got %q", entries[0].StashID)
	}
	if entries[0].MissionCount != 2 {
		t.Errorf("expected 2 missions in newer stash, got %d", entries[0].MissionCount)
	}
	if entries[1].StashID != "older" {
		t.Errorf("expected second entry to be 'older', got %q", entries[1].StashID)
	}
	if entries[1].MissionCount != 1 {
		t.Errorf("expected 1 mission in older stash, got %d", entries[1].MissionCount)
	}
}

func TestHandleListStashes_SkipsNonJSON(t *testing.T) {
	tmpDir := t.TempDir()
	stashDirpath := config.GetStashDirpath(tmpDir)
	if err := os.MkdirAll(stashDirpath, 0755); err != nil {
		t.Fatal(err)
	}

	// Write a valid stash file and a non-JSON file
	stash := &StashFile{
		CreatedAt: time.Now().UTC(),
		Missions:  []StashMission{{MissionID: "m1"}},
	}
	if err := writeStashFile(tmpDir, "valid", stash); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stashDirpath, "readme.txt"), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	srv := &Server{agencDirpath: tmpDir, logger: log.New(os.Stderr, "", 0)}
	req := httptest.NewRequest("GET", "/stash", nil)
	w := httptest.NewRecorder()

	err := srv.handleListStashes(w, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var entries []StashListEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(entries) != 1 {
		t.Errorf("expected 1 entry (skipping non-JSON), got %d", len(entries))
	}
}

func TestReadStashFile_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	badFilepath := filepath.Join(tmpDir, "bad.json")
	if err := os.WriteFile(badFilepath, []byte("not valid json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := readStashFile(badFilepath)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestReadStashFile_Nonexistent(t *testing.T) {
	_, err := readStashFile("/nonexistent/file.json")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestStashFileFormat(t *testing.T) {
	// Verify the JSON format matches the design doc
	stash := &StashFile{
		CreatedAt: time.Date(2026, 3, 2, 10, 30, 0, 0, time.UTC),
		Missions: []StashMission{
			{MissionID: "abc-123", LinkedSessions: []string{"agenc", "my-session"}},
			{MissionID: "def-456", LinkedSessions: nil},
		},
	}

	data, err := json.MarshalIndent(stash, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"created_at"`) {
		t.Error("missing created_at field")
	}
	if !strings.Contains(jsonStr, `"mission_id"`) {
		t.Error("missing mission_id field")
	}
	if !strings.Contains(jsonStr, `"linked_sessions"`) {
		t.Error("missing linked_sessions field")
	}
}
