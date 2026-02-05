package cmd

import (
	"regexp"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

// ============================================================================
// Mission fzf picker infrastructure
// ============================================================================

// missionPickerEntry holds the display-ready fields for a mission in fzf pickers.
// Fields mirror the mission ls output to maintain visual consistency across commands.
type missionPickerEntry struct {
	MissionID  string
	LastActive string // formatted timestamp
	ShortID    string
	Status     string // colorized status (RUNNING/STOPPED/ARCHIVED)
	Agent      string // display-formatted (may contain ANSI)
	Session    string // session name (truncated)
	Repo       string // display-formatted (may contain ANSI)
}

// ansiPattern matches ANSI SGR escape sequences for stripping colors.
var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// shortIDPattern matches 8 hex characters (mission short ID).
var shortIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}$`)

// fullUUIDPattern matches a full UUID (8-4-4-4-12 hex format).
var fullUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// looksLikeMissionID returns true if the input appears to be a mission ID
// (either a short 8-char hex ID or a full UUID) rather than search terms.
func looksLikeMissionID(input string) bool {
	return shortIDPattern.MatchString(input) || fullUUIDPattern.MatchString(input)
}

// stripAnsiCodes removes ANSI escape sequences from a string.
func stripAnsiCodes(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

// buildMissionPickerEntries converts database missions to picker entries using
// the same formatting infrastructure as mission ls.
func buildMissionPickerEntries(db *database.DB, missions []*database.Mission) ([]missionPickerEntry, error) {
	cfg, err := readConfig()
	if err != nil {
		return nil, err
	}
	nicknames := buildNicknameMap(cfg.AgentTemplates)
	claudeConfigDirpath := config.GetGlobalClaudeDirpath(agencDirpath)

	entries := make([]missionPickerEntry, 0, len(missions))
	for _, m := range missions {
		sessionName := resolveSessionName(claudeConfigDirpath, db, m)
		status := getMissionStatus(m.ID, m.Status)
		entries = append(entries, missionPickerEntry{
			MissionID:  m.ID,
			LastActive: formatLastActive(m.LastHeartbeat),
			ShortID:    m.ShortID,
			Status:     colorizeStatus(status),
			Agent:      displayAgentTemplate(m.AgentTemplate, nicknames),
			Session:    truncatePrompt(sessionName, defaultPromptMaxLen),
			Repo:       displayGitRepo(m.GitRepo),
		})
	}
	return entries, nil
}

// formatMissionMatchLine returns a plain-text representation of a mission
// picker entry suitable for sequential substring matching.
func formatMissionMatchLine(entry missionPickerEntry) string {
	return entry.LastActive + " " + entry.ShortID + " " + stripAnsiCodes(entry.Agent) + " " + entry.Session + " " + stripAnsiCodes(entry.Repo)
}

// filterStoppedMissions returns only missions that are currently stopped.
func filterStoppedMissions(missions []*database.Mission) []*database.Mission {
	var filtered []*database.Mission
	for _, m := range missions {
		if getMissionStatus(m.ID, m.Status) == "STOPPED" {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// filterRunningMissions returns only missions that are currently running.
func filterRunningMissions(missions []*database.Mission) []*database.Mission {
	var filtered []*database.Mission
	for _, m := range missions {
		if getMissionStatus(m.ID, m.Status) == "RUNNING" {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// ============================================================================
// Database helpers
// ============================================================================

// openDB centralizes the database opening boilerplate used by every command
// that touches the mission database.
func openDB() (*database.DB, error) {
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to open database")
	}
	return db, nil
}

// resolveAndRunForMission handles the common pattern for commands that always
// receive exactly one mission ID: open DB, resolve the ID, run the action.
func resolveAndRunForMission(rawID string, fn func(*database.DB, string) error) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missionID, err := db.ResolveMissionID(rawID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve mission ID")
	}

	return fn(db, missionID)
}

// prepareMissionForAction verifies a mission exists and stops its wrapper.
// This is the common setup needed before destructive operations (remove, archive).
func prepareMissionForAction(db *database.DB, missionID string) (*database.Mission, error) {
	mission, err := db.GetMission(missionID)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get mission")
	}

	if err := stopMissionWrapper(missionID); err != nil {
		return nil, stacktrace.Propagate(err, "failed to stop wrapper for mission '%s'", missionID)
	}

	return mission, nil
}

// ============================================================================
// Config helpers
// ============================================================================

// readConfig centralizes the config reading boilerplate. It returns the config
// only; use config.ReadAgencConfig directly when the config manager is needed
// for modifications.
func readConfig() (*config.AgencConfig, error) {
	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read config")
	}
	return cfg, nil
}
