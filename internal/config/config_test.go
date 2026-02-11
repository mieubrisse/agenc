package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsMissionAssistant(t *testing.T) {
	t.Run("returns false without marker file", func(t *testing.T) {
		agencDirpath := t.TempDir()
		missionID := "test-mission-id"

		// Create mission directory but no marker file
		missionDirpath := filepath.Join(agencDirpath, MissionsDirname, missionID)
		if err := os.MkdirAll(missionDirpath, 0755); err != nil {
			t.Fatalf("failed to create mission dir: %v", err)
		}

		if IsMissionAssistant(agencDirpath, missionID) {
			t.Error("expected false when marker file does not exist")
		}
	})

	t.Run("returns true with marker file", func(t *testing.T) {
		agencDirpath := t.TempDir()
		missionID := "test-mission-id"

		// Create mission directory with marker file
		missionDirpath := filepath.Join(agencDirpath, MissionsDirname, missionID)
		if err := os.MkdirAll(missionDirpath, 0755); err != nil {
			t.Fatalf("failed to create mission dir: %v", err)
		}

		markerFilepath := filepath.Join(missionDirpath, AssistantMarkerFilename)
		if err := os.WriteFile(markerFilepath, []byte{}, 0644); err != nil {
			t.Fatalf("failed to write marker file: %v", err)
		}

		if !IsMissionAssistant(agencDirpath, missionID) {
			t.Error("expected true when marker file exists")
		}
	})

	t.Run("returns false when mission dir does not exist", func(t *testing.T) {
		agencDirpath := t.TempDir()
		missionID := "nonexistent-mission"

		if IsMissionAssistant(agencDirpath, missionID) {
			t.Error("expected false when mission directory does not exist")
		}
	})
}

func TestGetMissionAssistantMarkerFilepath(t *testing.T) {
	agencDirpath := "/home/user/.agenc"
	missionID := "abc-123"

	result := GetMissionAssistantMarkerFilepath(agencDirpath, missionID)
	expected := filepath.Join(agencDirpath, MissionsDirname, missionID, AssistantMarkerFilename)

	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}
