package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/wrapper"
)

var missionResumeCmd = &cobra.Command{
	Use:   resumeCmdStr + " [mission-id|search-terms...]",
	Short: "Unarchive (if needed) and resume a mission with claude --continue",
	Long: `Unarchive (if needed) and resume a mission with claude --continue.

Without arguments, opens an interactive fzf picker showing stopped missions.
With arguments, accepts a mission ID (short or full UUID) or search terms to
filter the list. If exactly one mission matches search terms, it is auto-selected.`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionResume,
}

func init() {
	missionCmd.AddCommand(missionResumeCmd)
}

func runMissionResume(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	stoppedMissions := filterStoppedMissions(missions)
	if len(stoppedMissions) == 0 {
		return stacktrace.NewError("no stopped missions to resume")
	}

	entries, err := buildMissionPickerEntries(db, stoppedMissions)
	if err != nil {
		return err
	}

	result, err := Resolve(strings.Join(args, " "), Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := db.ResolveMissionID(input)
			if err != nil {
				return missionPickerEntry{}, false, stacktrace.Propagate(err, "failed to resolve mission ID")
			}
			// Find the entry in our stopped missions list
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s is not stopped", input)
		},
		GetItems:    func() ([]missionPickerEntry, error) { return entries, nil },
		ExtractText: formatMissionMatchLine,
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Session, e.Repo}
		},
		FzfPrompt:   "Select mission to resume: ",
		FzfHeaders:  []string{"LAST ACTIVE", "ID", "SESSION", "REPO"},
		MultiSelect: false,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	selected := result.Items[0]

	// Print auto-select message only if search terms (not UUID) matched exactly one
	input := strings.Join(args, " ")
	if input != "" && !looksLikeMissionID(input) {
		fmt.Printf("Auto-selected: %s\n", selected.ShortID)
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

	windowTitle := lookupWindowTitle(agencDirpath, missionRecord.GitRepo)
	if config.IsMissionAssistant(agencDirpath, missionID) {
		windowTitle = "AgenC"
	}
	w := wrapper.NewWrapper(agencDirpath, missionID, missionRecord.GitRepo, windowTitle, "", db)
	return w.Run(true)
}
