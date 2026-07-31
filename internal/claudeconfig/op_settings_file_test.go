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

// setupWriteMissionOpSettingsTest returns a fresh temp agenc dir root and a
// mission ID. It deliberately does NOT pre-create the mission directory — this
// exercises the realistic spawn path where WriteMissionOpSettings must create
// its own mission dir before writing into it.
func setupWriteMissionOpSettingsTest(t *testing.T) (string, string) {
	t.Helper()

	agencDirpath := t.TempDir()
	missionID := "test-mission-uuid"

	return agencDirpath, missionID
}
