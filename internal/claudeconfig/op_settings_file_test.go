package claudeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/odyssey/agenc/internal/config"
)

func TestWriteMissionOpSettings_SettingsFileExists(t *testing.T) {
	agencDirpath, missionID := setupWriteMissionOpSettingsTest(t)

	if err := WriteMissionOpSettings(agencDirpath, missionID); err != nil {
		t.Fatalf("WriteMissionOpSettings returned unexpected error: %v", err)
	}

	settingsFilepath := config.GetMissionOpSettingsFilepath(agencDirpath, missionID)
	if _, err := os.Stat(settingsFilepath); err != nil {
		t.Fatalf("settings file should exist at %s: %v", settingsFilepath, err)
	}
}

func TestWriteMissionOpSettings_SettingsFileIsValidJSON(t *testing.T) {
	agencDirpath, missionID := setupWriteMissionOpSettingsTest(t)

	if err := WriteMissionOpSettings(agencDirpath, missionID); err != nil {
		t.Fatalf("WriteMissionOpSettings returned unexpected error: %v", err)
	}

	settingsFilepath := config.GetMissionOpSettingsFilepath(agencDirpath, missionID)
	data, err := os.ReadFile(settingsFilepath)
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings file is not valid JSON: %v", err)
	}
}

func TestWriteMissionOpSettings_SettingsHasHooksAndPermissions(t *testing.T) {
	agencDirpath, missionID := setupWriteMissionOpSettingsTest(t)

	if err := WriteMissionOpSettings(agencDirpath, missionID); err != nil {
		t.Fatalf("WriteMissionOpSettings returned unexpected error: %v", err)
	}

	settingsFilepath := config.GetMissionOpSettingsFilepath(agencDirpath, missionID)
	data, err := os.ReadFile(settingsFilepath)
	if err != nil {
		t.Fatalf("failed to read settings file: %v", err)
	}

	var settings map[string]json.RawMessage
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings file is not valid JSON: %v", err)
	}

	if _, ok := settings["hooks"]; !ok {
		t.Error("settings must contain 'hooks' key")
	}
	if _, ok := settings["permissions"]; !ok {
		t.Error("settings must contain 'permissions' key")
	}
}

func TestWriteMissionOpSettings_GuardScriptExists(t *testing.T) {
	agencDirpath, missionID := setupWriteMissionOpSettingsTest(t)

	if err := WriteMissionOpSettings(agencDirpath, missionID); err != nil {
		t.Fatalf("WriteMissionOpSettings returned unexpected error: %v", err)
	}

	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	scriptFilepath := filepath.Join(missionDirpath, AgencHooksDirname, RepoLibraryGuardScriptName)

	info, err := os.Stat(scriptFilepath)
	if err != nil {
		t.Fatalf("guard script should exist at %s: %v", scriptFilepath, err)
	}
	if info.Mode()&0111 == 0 {
		t.Errorf("guard script at %s must be executable, got mode %v", scriptFilepath, info.Mode())
	}
}

// setupWriteMissionOpSettingsTest creates a minimal agenc dir structure under a
// temp directory and returns (agencDirpath, missionID) ready for WriteMissionOpSettings.
func setupWriteMissionOpSettingsTest(t *testing.T) (string, string) {
	t.Helper()

	agencDirpath := t.TempDir()
	missionID := "test-mission-uuid"

	// Create the mission directory — WriteMissionOpSettings writes files into it
	// but does not create the mission dir itself.
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	if err := os.MkdirAll(missionDirpath, 0755); err != nil {
		t.Fatalf("failed to create mission dir %s: %v", missionDirpath, err)
	}

	return agencDirpath, missionID
}
