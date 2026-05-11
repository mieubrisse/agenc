package cmd

import (
	"os"
	"os/exec"
	"regexp"
	"strings"

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
	LastPrompt string // formatted timestamp from last_user_prompt_at; "--" when nil
	ShortID    string
	Status     string // colorized status (RUNNING/STOPPED/ARCHIVED)
	Session    string // session name (truncated)
	Repo       string // display-formatted (may contain ANSI)
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

// buildMissionPickerEntries converts database missions to picker entries using
// the same formatting infrastructure as mission ls. The session name follows
// the same priority chain as tmux window title reconciliation:
// ResolvedSessionTitle (custom_title > agenc_custom_title > auto_summary) > prompt.
func buildMissionPickerEntries(missions []*database.Mission, sessionMaxLen int) []missionPickerEntry {
	cfg, _ := readConfig()
	entries := make([]missionPickerEntry, 0, len(missions))
	for _, m := range missions {
		sessionName := resolveSessionName(m)
		status := getMissionStatus(m.ID, m.Status, m.ClaudeState)
		repo := formatRepoDisplay(m.GitRepo, m.IsAdjutant, cfg)
		entries = append(entries, missionPickerEntry{
			MissionID:  m.ID,
			LastPrompt: formatLastPrompt(m.LastUserPromptAt, m.CreatedAt),
			ShortID:    m.ShortID,
			Status:     colorizeStatus(status),
			Session:    truncatePrompt(sessionName, sessionMaxLen),
			Repo:       repo,
		})
	}
	return entries
}

// filterLinkedMissions returns only running missions whose tmux pane is linked
// into the given tmux session.
func filterLinkedMissions(missions []*database.Mission, tmuxSession string) []*database.Mission {
	linkedPanes := getSessionPaneIDs(tmuxSession)
	var filtered []*database.Mission
	for _, m := range missions {
		if m.TmuxPane == nil {
			continue
		}
		if !linkedPanes[*m.TmuxPane] {
			continue
		}
		if !isMissionRunning(getMissionStatus(m.ID, m.Status, m.ClaudeState)) {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

// getSessionPaneIDs returns the set of tmux pane IDs (without the "%" prefix)
// present in the given tmux session. Returns an empty map if the session doesn't
// exist or tmux is not running.
func getSessionPaneIDs(tmuxSession string) map[string]bool {
	out, err := exec.Command("tmux", "list-panes", "-s", "-t", tmuxSession, "-F", "#{pane_id}").Output()
	if err != nil {
		return map[string]bool{}
	}
	panes := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		panes[strings.TrimPrefix(line, "%")] = true
	}
	return panes
}

// filterRunningMissions returns only missions that are currently running.
func filterRunningMissions(missions []*database.Mission) []*database.Mission {
	var filtered []*database.Mission
	for _, m := range missions {
		if isMissionRunning(getMissionStatus(m.ID, m.Status, m.ClaudeState)) {
			filtered = append(filtered, m)
		}
	}
	return filtered
}

// ============================================================================
// Server client helpers
// ============================================================================

// serverClient returns an HTTP client connected to the AgenC server. All CLI
// commands that delegate work to the server use this. It only resolves the
// agenc directory (read-only) and ensures the server is running — it does NOT
// call ensureConfigured() or EnsureDirStructure(), so it is safe to call from
// sandboxed environments that cannot write to ~/.agenc.
func serverClient() (*server.Client, error) {
	dirpath, err := config.GetAgencDirpath()
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get agenc directory path")
	}
	ensureServerRunning()
	socketFilepath := config.GetServerSocketFilepath(dirpath)
	return server.NewClient(socketFilepath), nil
}

// ============================================================================
// Config helpers
// ============================================================================

// getCallingPaneID returns the tmux pane ID of the calling process, with the
// "%" prefix stripped. Returns "" if not inside tmux.
//
// Prefers $AGENC_CALLING_PANE_ID (the underlying pane, forwarded by the palette
// through keybindings and display-popup -e) over $TMUX_PANE (which may be a
// temporary popup pane that can't be resolved to a session).
//
// See "Calling pane resolution" in docs/system-architecture.md for the three
// execution contexts and how the pane ID reaches this function in each.
func getCallingPaneID() string {
	if pane := os.Getenv("AGENC_CALLING_PANE_ID"); pane != "" {
		return strings.TrimPrefix(pane, "%")
	}
	return strings.TrimPrefix(os.Getenv("TMUX_PANE"), "%")
}

// getCurrentTmuxSessionName returns the name of the tmux session the caller
// is running in. Returns an empty string if not inside tmux or if the tmux
// server can't be reached (e.g. sandbox restriction).
//
// Prefer getCallingPaneID() for server requests — it reads an env var instead
// of querying the tmux socket, making it sandbox-safe.
func getCurrentTmuxSessionName() string {
	if os.Getenv("TMUX") == "" {
		return ""
	}
	out, err := exec.Command("tmux", "display-message", "-p", "#{session_name}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// getCallingSessionName returns the tmux session the user is currently
// attached to. For attach/detach, this is the authoritative answer to "which
// session should the mission window be linked/unlinked into?" — pane IDs are
// not, because a mission's pane can be linked into multiple sessions
// simultaneously.
//
// Prefers $AGENC_CALLING_SESSION_NAME (forwarded by the keybinding generator
// and palette dispatch from tmux's #{session_name} format at key-press time)
// over a direct `tmux display-message` query. The env var is preferred because
// it is captured in the user's client context — independent of any popup or
// run-shell wrapper the command may be running inside.
func getCallingSessionName() string {
	if session := os.Getenv("AGENC_CALLING_SESSION_NAME"); session != "" {
		return session
	}
	return getCurrentTmuxSessionName()
}

// readConfig centralizes the config reading boilerplate. It returns the config
// only; use readConfigWithComments when the comment map is needed for write-back.
func readConfig() (*config.AgencConfig, error) {
	agencDirpath, err := ensureConfigured()
	if err != nil {
		return nil, err
	}
	checkServerVersion(agencDirpath)
	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to read config")
	}
	return cfg, nil
}

// readConfigWithComments reads the config and returns the comment map needed
// for write-back operations that preserve YAML comments.
// Also acquires an advisory file lock on config.yml.lock to prevent concurrent
// read-modify-write races. The returned release function MUST be called (via
// defer) when the caller is done writing — it releases the lock.
func readConfigWithComments() (*config.AgencConfig, yaml.CommentMap, func(), error) {
	agencDirpath, err := ensureConfigured()
	if err != nil {
		return nil, nil, nil, err
	}
	checkServerVersion(agencDirpath)

	release, err := config.AcquireConfigLock(agencDirpath)
	if err != nil {
		return nil, nil, nil, stacktrace.Propagate(err, "failed to acquire config lock")
	}

	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		release()
		return nil, nil, nil, stacktrace.Propagate(err, "failed to read config")
	}
	return cfg, cm, release, nil
}

// resolveCronID resolves a cron name or UUID to a cron ID using the server API.
// It first tries to match the argument as a cron name. If no match is found,
// it treats the argument as a UUID directly.
func resolveCronID(client *server.Client, nameOrID string) (string, error) {
	crons, err := client.ListCrons()
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to list crons")
	}

	for _, c := range crons {
		if c.Name == nameOrID {
			if c.ID == "" {
				return "", stacktrace.NewError("cron job '%s' has no ID — re-create it or add an 'id' field to config.yml", nameOrID)
			}
			return c.ID, nil
		}
	}

	// No name match — treat as UUID. Check if it matches any known cron ID.
	for _, c := range crons {
		if c.ID == nameOrID {
			return nameOrID, nil
		}
	}

	return "", stacktrace.NewError("cron job '%s' not found", nameOrID)
}
