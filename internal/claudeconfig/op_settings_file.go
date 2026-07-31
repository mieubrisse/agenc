package claudeconfig

import (
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/odyssey/agenc/internal/config"
)

// WriteMissionOpSettings writes the per-mission operational settings file and
// the AgenC hook scripts required by that settings file.
//
// It writes:
//   - <missionDir>/agenc-hooks/repo-library-guard.sh  (via WriteAgencHookScripts)
//   - <missionDir>/agenc-settings.json                (via BuildOperationalSettings)
//
// The hook-scripts base dir passed to both helpers is the mission dir, so the
// guard-hook absolute path embedded in the settings file resolves to the script
// that was actually written.
//
// Called by Task 4 (State Y flip) at mission-create and on reload.
//
//nolint:unused // wired at the State Y flip (Task 4)
func WriteMissionOpSettings(agencDirpath string, missionID string) error {
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)

	// Write the guard script into <missionDir>/agenc-hooks/
	if err := WriteAgencHookScripts(missionDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to write AgenC hook scripts for mission '%s'", missionID)
	}

	// Build the settings JSON. hookScriptsBaseDirpath == missionDirpath so that
	// the guard-hook path in the output matches where WriteAgencHookScripts wrote it.
	settingsBytes, err := BuildOperationalSettings(agencDirpath, agentDirpath, missionDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to build operational settings for mission '%s'", missionID)
	}

	settingsFilepath := config.GetMissionOpSettingsFilepath(agencDirpath, missionID)
	if err := os.WriteFile(settingsFilepath, settingsBytes, 0644); err != nil {
		return stacktrace.Propagate(err, "failed to write operational settings file '%s'", settingsFilepath)
	}

	return nil
}
