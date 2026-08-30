package claudeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"

	"github.com/odyssey/agenc/internal/config"
)

const (
	// claudeHomeDirname is Claude Code's config directory under the user's home.
	claudeHomeDirname = ".claude"

	// sessionsRegistryDirname is the directory under Claude Code's config
	// directory where it writes one record per live session.
	sessionsRegistryDirname = "sessions"

	// registryRecordFileExtension is the extension on each registry record,
	// whose basename is the session's PID.
	registryRecordFileExtension = ".json"
)

// MissionSession is a Claude Code session-registry record that belongs to an
// AgenC mission. Claude Code writes one JSON file per session to
// ~/.claude/sessions/<pid>.json; the same registry backs Claude Code's
// ListAgents tool, which is why PeerName and TmuxTarget are exactly the
// strings ListAgents prints.
//
// This struct is the only place in AgenC that knows the registry's shape, so
// that a Claude Code format change lands in exactly one file.
type MissionSession struct {
	// MissionID is the AgenC mission the session is running in, derived from
	// the record's working directory. Not a registry field.
	MissionID string `json:"-"`

	// PID is the Claude Code process ID. Claude Code can leave a record behind
	// after its process exits, so callers must check this PID for liveness.
	PID int `json:"pid"`

	// Cwd is the directory Claude Code was launched in. For a mission this is
	// the mission's agent/ directory, which is how a record is joined back to
	// a mission.
	Cwd string `json:"cwd"`

	// PeerName is the session's derived peer name (e.g. "agent-da") — the name
	// Claude Code's ListAgents tool prints and SendMessage addresses.
	PeerName string `json:"name"`

	// TmuxTarget is the session's tmux location in full "session:@window.%pane"
	// form (e.g. "hyperspace:@126.%134"), exactly as ListAgents prints it. Note
	// this is NOT the bare pane ID that AgenC stores on a mission.
	TmuxTarget string `json:"tmux"`
}

// ListMissionSessions reads Claude Code's session registry and returns the
// records whose working directory is an AgenC mission.
//
// Records for non-mission Claude sessions are skipped, as are records that
// cannot be read or parsed — one malformed file must not blind AgenC to every
// other session. An absent registry yields an empty slice, not an error.
//
// Records are returned as they appear on disk. Claude Code can leave a record
// behind after its process dies, so callers must filter on PID liveness.
func ListMissionSessions(agencDirpath string) ([]MissionSession, error) {
	homeDirpath, err := os.UserHomeDir()
	if err != nil {
		return nil, stacktrace.Propagate(err, "An error occurred determining the home directory, which is needed to locate Claude Code's session registry")
	}
	registryDirpath := filepath.Join(homeDirpath, claudeHomeDirname, sessionsRegistryDirname)
	return listMissionSessionsInDir(registryDirpath, agencDirpath)
}

// listMissionSessionsInDir is the testable core of ListMissionSessions, with
// the registry directory injected.
func listMissionSessionsInDir(registryDirpath string, agencDirpath string) ([]MissionSession, error) {
	entries, err := os.ReadDir(registryDirpath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, stacktrace.Propagate(err, "An error occurred reading Claude Code's session registry directory '%v'", registryDirpath)
	}

	missionsDirpath, err := filepath.Abs(config.GetMissionsDirpath(agencDirpath))
	if err != nil {
		return nil, stacktrace.Propagate(err, "An error occurred absolutizing the missions directory derived from agenc directory '%v'", agencDirpath)
	}

	missionSessions := []MissionSession{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), registryRecordFileExtension) {
			continue
		}

		recordFilepath := filepath.Join(registryDirpath, entry.Name())
		recordBytes, err := os.ReadFile(recordFilepath)
		if err != nil {
			continue
		}

		var missionSession MissionSession
		if err := json.Unmarshal(recordBytes, &missionSession); err != nil {
			continue
		}

		missionID, isMission := parseMissionIDFromAgentDirpath(missionsDirpath, missionSession.Cwd)
		if !isMission {
			continue
		}
		missionSession.MissionID = missionID

		missionSessions = append(missionSessions, missionSession)
	}

	return missionSessions, nil
}

// parseMissionIDFromAgentDirpath extracts a mission ID from a Claude Code
// session's working directory, which for a mission is
// <missionsDirpath>/<mission-id>/agent. Returns false for any other directory.
//
// The match is deliberately strict about segment count: a nested AgenC
// installation (a test environment living inside a mission's agent directory)
// produces paths that share the prefix but carry extra segments, and those
// must not be mistaken for missions of the outer installation.
func parseMissionIDFromAgentDirpath(missionsDirpath string, cwd string) (string, bool) {
	if cwd == "" {
		return "", false
	}

	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return "", false
	}

	relPath, err := filepath.Rel(missionsDirpath, absCwd)
	if err != nil {
		return "", false
	}

	segments := strings.Split(relPath, string(filepath.Separator))
	const expectedSegmentCount = 2 // <mission-id>/agent
	if len(segments) != expectedSegmentCount || segments[1] != config.AgentDirname {
		return "", false
	}

	missionID := segments[0]
	if missionID == "" || missionID == "." || missionID == ".." {
		return "", false
	}
	return missionID, true
}
