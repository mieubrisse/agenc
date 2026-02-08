package cmd

import (
	"regexp"
	"sync"

	"github.com/goccy/go-yaml"
	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
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
			Session:    truncatePrompt(sessionName, defaultPromptMaxLen),
			Repo:       displayGitRepo(m.GitRepo),
		})
	}
	return entries, nil
}

// formatMissionMatchLine returns a plain-text representation of a mission
// picker entry suitable for sequential substring matching.
func formatMissionMatchLine(entry missionPickerEntry) string {
	return entry.LastActive + " " + entry.ShortID + " " + entry.Session + " " + stripAnsiCodes(entry.Repo)
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
// Agenc context — lazy one-shot initialization
// ============================================================================

var (
	agencCtxOnce sync.Once
	agencCtxErr  error
)

// getAgencContext lazily ensures agenc is fully configured. It runs
// ensureConfigured() at most once per CLI invocation and, for non-daemon
// processes, checks whether the daemon needs a version-bump restart.
func getAgencContext() (string, error) {
	agencCtxOnce.Do(func() {
		_, agencCtxErr = ensureConfigured()
		if agencCtxErr != nil {
			return
		}
		if !daemon.IsDaemonProcess() {
			checkDaemonVersion(agencDirpath)
		}
	})
	return agencDirpath, agencCtxErr
}

// ============================================================================
// Database helpers
// ============================================================================

// openDB centralizes the database opening boilerplate used by every command
// that touches the mission database.
func openDB() (*database.DB, error) {
	if _, err := getAgencContext(); err != nil {
		return nil, err
	}
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
// only; use readConfigWithComments when the comment map is needed for write-back.
func readConfig() (*config.AgencConfig, error) {
	if _, err := getAgencContext(); err != nil {
		return nil, err
	}
	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read config")
	}
	return cfg, nil
}

// readConfigWithComments reads the config and returns the comment map needed
// for write-back operations that preserve YAML comments.
func readConfigWithComments() (*config.AgencConfig, yaml.CommentMap, error) {
	if _, err := getAgencContext(); err != nil {
		return nil, nil, err
	}
	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return nil, nil, stacktrace.Propagate(err, "failed to read config")
	}
	return cfg, cm, nil
}
