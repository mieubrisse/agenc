package cmd

import (
	"fmt"
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

// missionPickerOptions configures the fzf picker behavior.
type missionPickerOptions struct {
	Prompt       string
	MultiSelect  bool
	InitialQuery string
	ShowStatus   bool // if true, includes the STATUS column
}

// selectMissionsFzf presents missions in an fzf picker with the standard
// column layout (matching mission ls). Returns selected entries.
// Returns nil, nil if the user cancels.
func selectMissionsFzf(entries []missionPickerEntry, opts missionPickerOptions) ([]missionPickerEntry, error) {
	if len(entries) == 0 {
		return nil, nil
	}

	// Build rows and headers based on options
	var headers []string
	var rows [][]string

	if opts.ShowStatus {
		headers = []string{"LAST ACTIVE", "ID", "STATUS", "AGENT", "SESSION", "REPO"}
		for _, e := range entries {
			rows = append(rows, []string{e.LastActive, e.ShortID, e.Status, e.Agent, e.Session, e.Repo})
		}
	} else {
		headers = []string{"LAST ACTIVE", "ID", "AGENT", "SESSION", "REPO"}
		for _, e := range entries {
			rows = append(rows, []string{e.LastActive, e.ShortID, e.Agent, e.Session, e.Repo})
		}
	}

	indices, err := runFzfPicker(FzfPickerConfig{
		Prompt:       opts.Prompt,
		Headers:      headers,
		Rows:         rows,
		MultiSelect:  opts.MultiSelect,
		InitialQuery: opts.InitialQuery,
	})
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH; pass mission IDs as arguments instead")
	}
	if indices == nil {
		return nil, nil
	}

	selected := make([]missionPickerEntry, 0, len(indices))
	for _, idx := range indices {
		selected = append(selected, entries[idx])
	}
	return selected, nil
}

// formatMissionMatchLine returns a plain-text representation of a mission
// picker entry suitable for sequential substring matching.
func formatMissionMatchLine(entry missionPickerEntry) string {
	return entry.LastActive + " " + entry.ShortID + " " + stripAnsiCodes(entry.Agent) + " " + entry.Session + " " + stripAnsiCodes(entry.Repo)
}

// extractMissionShortIDs returns the short IDs from a slice of picker entries.
func extractMissionShortIDs(entries []missionPickerEntry) []string {
	ids := make([]string, len(entries))
	for i, e := range entries {
		ids[i] = e.ShortID
	}
	return ids
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

// resolveAndRunForEachMission handles the common pattern of: if no args use
// fzf selector, resolve all IDs (fail fast), then run action on each.
func resolveAndRunForEachMission(
	args []string,
	selectFn func(*database.DB) ([]string, error),
	actionFn func(*database.DB, string) error,
) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missionIDs := args
	if len(missionIDs) == 0 {
		selectedIDs, err := selectFn(db)
		if err != nil {
			return err
		}
		if len(selectedIDs) == 0 {
			return nil
		}
		missionIDs = selectedIDs
	}

	// Resolve all IDs up front (fail fast on any bad input)
	resolvedIDs := make([]string, 0, len(missionIDs))
	for _, rawID := range missionIDs {
		resolved, err := db.ResolveMissionID(rawID)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve mission ID")
		}
		resolvedIDs = append(resolvedIDs, resolved)
	}

	for _, missionID := range resolvedIDs {
		if err := actionFn(db, missionID); err != nil {
			return err
		}
	}

	return nil
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

// missionSelectConfig configures the interactive mission selection picker.
type missionSelectConfig struct {
	IncludeArchived bool                                                 // pass to db.ListMissions
	Filter          func([]*database.Mission) []*database.Mission        // optional post-fetch filter
	EmptyMessage    string                                               // message when no missions match
	Prompt          string                                               // fzf prompt
	ShowStatus      bool                                                 // whether to show STATUS column
}

// selectMissionsInteractive presents an fzf picker for mission selection.
// Returns the selected mission short IDs, or nil if no missions or user cancels.
func selectMissionsInteractive(db *database.DB, cfg missionSelectConfig) ([]string, error) {
	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: cfg.IncludeArchived})
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list missions")
	}

	if cfg.Filter != nil {
		missions = cfg.Filter(missions)
	}

	if len(missions) == 0 {
		if cfg.EmptyMessage != "" {
			fmt.Println(cfg.EmptyMessage)
		}
		return nil, nil
	}

	entries, err := buildMissionPickerEntries(db, missions)
	if err != nil {
		return nil, err
	}

	selected, err := selectMissionsFzf(entries, missionPickerOptions{
		Prompt:      cfg.Prompt,
		MultiSelect: true,
		ShowStatus:  cfg.ShowStatus,
	})
	if err != nil {
		return nil, err
	}

	return extractMissionShortIDs(selected), nil
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
