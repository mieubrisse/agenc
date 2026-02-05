package cmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/wrapper"
)

// ansiEscapePattern matches ANSI SGR escape sequences.
var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// stripAnsi removes ANSI escape sequences from a string.
func stripAnsi(s string) string {
	return ansiEscapePattern.ReplaceAllString(s, "")
}

var missionResumeCmd = &cobra.Command{
	Use:   resumeCmdStr + " [search-terms...]",
	Short: "Unarchive (if needed) and resume a mission with claude --continue",
	Long: `Unarchive (if needed) and resume a mission with claude --continue.

Without arguments, opens an interactive fzf picker showing stopped missions.
Positional arguments act as search terms to filter the list. If exactly one
mission matches, it is auto-selected.`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionResume,
}

func init() {
	missionCmd.AddCommand(missionResumeCmd)
}

// stoppedMissionEntry holds the display-ready fields for a stopped mission.
// Fields mirror the mission ls output (minus STATUS) to maintain visual
// consistency across commands.
type stoppedMissionEntry struct {
	MissionID  string
	LastActive string // formatted timestamp
	ShortID    string
	Agent      string // display-formatted (may contain ANSI)
	Session    string // session name (truncated)
	Repo       string // display-formatted (may contain ANSI)
}

func runMissionResume(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	entries, err := listStoppedMissions(db)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		return stacktrace.NewError("no stopped missions to resume")
	}

	var selected *stoppedMissionEntry
	if len(args) > 0 {
		matches := matchStoppedMissions(entries, args)
		if len(matches) == 1 {
			fmt.Printf("Auto-selected: %s\n", matches[0].ShortID)
			selected = &matches[0]
		} else {
			picked, err := selectStoppedMissionFzf(entries, strings.Join(args, " "))
			if err != nil {
				return err
			}
			if picked == nil {
				return nil
			}
			selected = picked
		}
	} else {
		picked, err := selectStoppedMissionFzf(entries, "")
		if err != nil {
			return err
		}
		if picked == nil {
			return nil
		}
		selected = picked
	}

	return resumeMission(db, selected.MissionID)
}

// resumeMission handles the per-mission resume logic: unarchive if needed,
// check wrapper state, validate directory format, and launch claude --continue.
func resumeMission(db *database.DB, missionID string) error {
	missionRecord, err := db.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	if missionRecord.Status == "archived" {
		if err := db.UnarchiveMission(missionID); err != nil {
			return stacktrace.Propagate(err, "failed to unarchive mission")
		}
		fmt.Printf("Unarchived mission: %s\n", database.ShortID(missionID))
	}

	// Check if the wrapper is already running for this mission
	pidFilepath := config.GetMissionPIDFilepath(agencDirpath, missionID)
	pid, err := daemon.ReadPID(pidFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read mission PID file")
	}
	if daemon.IsProcessRunning(pid) {
		return stacktrace.NewError("mission '%s' is already running (wrapper PID %d)", missionID, pid)
	}

	// Check for old-format mission (no agent/ subdirectory)
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)
	if _, err := os.Stat(agentDirpath); os.IsNotExist(err) {
		return stacktrace.NewError(
			"mission '%s' uses the old directory format (no agent/ subdirectory); "+
				"please archive it with '%s %s %s %s' and create a new mission",
			missionID, agencCmdStr, missionCmdStr, archiveCmdStr, missionID,
		)
	}

	fmt.Printf("Resuming mission: %s\n", database.ShortID(missionID))
	fmt.Println("Launching claude --continue...")

	w := wrapper.NewWrapper(agencDirpath, missionID, missionRecord.AgentTemplate, missionRecord.GitRepo, "", db)
	return w.Run(true)
}

// listStoppedMissions returns all stopped missions with their display fields.
// Uses the same formatting infrastructure as mission ls for visual consistency.
func listStoppedMissions(db *database.DB) ([]stoppedMissionEntry, error) {
	missions, err := db.ListMissions(false)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list missions")
	}

	cfg, _, cfgErr := config.ReadAgencConfig(agencDirpath)
	if cfgErr != nil {
		return nil, stacktrace.Propagate(cfgErr, "failed to read config")
	}
	nicknames := buildNicknameMap(cfg.AgentTemplates)
	claudeConfigDirpath := config.GetGlobalClaudeDirpath(agencDirpath)

	var entries []stoppedMissionEntry
	for _, m := range missions {
		if getMissionStatus(m.ID, m.Status) != "STOPPED" {
			continue
		}
		sessionName := resolveSessionName(claudeConfigDirpath, db, m)
		entries = append(entries, stoppedMissionEntry{
			MissionID:  m.ID,
			LastActive: formatLastActive(m.LastHeartbeat),
			ShortID:    m.ShortID,
			Agent:      displayAgentTemplate(m.AgentTemplate, nicknames),
			Session:    truncatePrompt(sessionName, defaultPromptMaxLen),
			Repo:       displayGitRepo(m.GitRepo),
		})
	}
	return entries, nil
}

// formatStoppedMissionMatchLine returns a plain-text representation of a
// stopped mission entry suitable for sequential substring matching.
func formatStoppedMissionMatchLine(entry stoppedMissionEntry) string {
	return entry.LastActive + " " + entry.ShortID + " " + stripAnsi(entry.Agent) + " " + entry.Session + " " + stripAnsi(entry.Repo)
}

// matchStoppedMissions filters entries by sequential case-insensitive
// substring matching against a plain-text representation of each entry.
func matchStoppedMissions(entries []stoppedMissionEntry, args []string) []stoppedMissionEntry {
	var matches []stoppedMissionEntry
	for _, entry := range entries {
		line := formatStoppedMissionMatchLine(entry)
		if matchesSequentialSubstrings(line, args) {
			matches = append(matches, entry)
		}
	}
	return matches
}

// selectStoppedMissionFzf presents stopped missions in an fzf picker and
// returns the selected entry. Returns nil if the user cancels.
// Column order matches mission ls output (minus STATUS) for visual consistency.
func selectStoppedMissionFzf(entries []stoppedMissionEntry, initialQuery string) (*stoppedMissionEntry, error) {
	// Build rows for the picker — column order matches mission ls (minus STATUS)
	var rows [][]string
	for _, e := range entries {
		rows = append(rows, []string{e.LastActive, e.ShortID, e.Agent, e.Session, e.Repo})
	}

	indices, err := runFzfPicker(FzfPickerConfig{
		Prompt:       "Select mission to resume: ",
		Headers:      []string{"LAST ACTIVE", "ID", "AGENT", "SESSION", "REPO"},
		Rows:         rows,
		MultiSelect:  false,
		InitialQuery: initialQuery,
	})
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH; install fzf or pass a mission ID as an argument")
	}
	if indices == nil {
		return nil, nil
	}

	return &entries[indices[0]], nil
}
