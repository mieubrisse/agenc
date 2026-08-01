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
// Called at mission-create and on reload (via the wrapper's spawnClaude).
func WriteMissionOpSettings(agencDirpath string, missionID string) error {
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)

	// Ensure the mission dir exists so the settings-file write below is self-
	// contained rather than depending on WriteAgencHookScripts' MkdirAll side effect.
	if err := os.MkdirAll(missionDirpath, 0755); err != nil {
		return stacktrace.Propagate(err, "failed to create mission directory '%s'", missionDirpath)
	}

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
