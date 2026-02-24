package cmd

import (
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/goccy/go-yaml"
	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
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
	TmuxTitle  string // tmux window title (empty if no tmux pane)
}

// shortIDPattern matches 8 hex characters (mission short ID).
var shortIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}$`)

// fullUUIDPattern matches a full UUID (8-4-4-4-12 hex format).
var fullUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// looksLikeMissionID returns true if the input appears to be a mission ID
// (either a short 8-char hex ID or a full UUID) rather than search terms.
func looksLikeMissionID(input string) bool {
	return shortIDPattern.MatchString(input) || fullUUIDPattern.MatchString(input)
}

// allLookLikeMissionIDs returns true if every element in the slice looks like
// a mission ID (short hex or full UUID).
func allLookLikeMissionIDs(inputs []string) bool {
	for _, input := range inputs {
		if !looksLikeMissionID(input) {
			return false
		}
	}
	return len(inputs) > 0
}

// getTmuxWindowTitle queries tmux for the window name associated with the given
// pane ID. The pane ID should be the numeric form without the "%" prefix (as
// stored in the database). Returns an empty string if the query fails or the
// pane no longer exists.
func getTmuxWindowTitle(paneID string) string {
	targetPane := "%" + paneID
	out, err := exec.Command("tmux", "display-message", "-p", "-t", targetPane, "#{window_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// buildMissionPickerEntries converts database missions to picker entries using
// the same formatting infrastructure as mission ls.
func buildMissionPickerEntries(missions []*database.Mission, sessionMaxLen int) []missionPickerEntry {
	entries := make([]missionPickerEntry, 0, len(missions))
	for _, m := range missions {
		sessionName := resolveSessionName(nil, m)
		status := getMissionStatus(m.ID, m.Status)
		repo := displayGitRepo(m.GitRepo)
		if config.IsMissionAdjutant(agencDirpath, m.ID) {
			repo = "🤖  Adjutant"
		}
		tmuxTitle := ""
		if m.TmuxPane != nil {
			tmuxTitle = getTmuxWindowTitle(*m.TmuxPane)
		}
		entries = append(entries, missionPickerEntry{
			MissionID:  m.ID,
			LastActive: formatLastActive(m.LastActive, m.LastHeartbeat, m.CreatedAt),
			ShortID:    m.ShortID,
			Status:     colorizeStatus(status),
			Session:    truncatePrompt(sessionName, sessionMaxLen),
			Repo:       repo,
			TmuxTitle:  tmuxTitle,
		})
	}
	return entries
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
// ensureConfigured() at most once per CLI invocation and, for non-server
// processes, checks whether the server needs a version-bump restart.
func getAgencContext() (string, error) {
	agencCtxOnce.Do(func() {
		_, agencCtxErr = ensureConfigured()
		if agencCtxErr != nil {
			return
		}
		if !server.IsServerProcess() {
			checkServerVersion(agencDirpath)
		}
	})
	return agencDirpath, agencCtxErr
}

// ============================================================================
// Server client helpers
// ============================================================================

// serverClient returns an HTTP client connected to the AgenC server. All CLI
// commands use this instead of opening the database directly.
func serverClient() (*server.Client, error) {
	if _, err := getAgencContext(); err != nil {
		return nil, err
	}
	ensureServerRunning(agencDirpath)
	socketFilepath := config.GetServerSocketFilepath(agencDirpath)
	return server.NewClient(socketFilepath), nil
}

// ============================================================================
// Config helpers
// ============================================================================

// lookupWindowTitle reads the config and returns the window title for the
// given repo, or empty string if not configured or on read error.
func lookupWindowTitle(agencDirpath string, gitRepoName string) string {
	if gitRepoName == "" {
		return ""
	}
	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return ""
	}
	return cfg.GetWindowTitle(gitRepoName)
}

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
