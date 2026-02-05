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

func runMissionResume(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missions, err := db.ListMissions(false)
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

	var selected *missionPickerEntry
	if len(args) > 0 {
		matches := matchMissionEntries(entries, args)
		if len(matches) == 1 {
			fmt.Printf("Auto-selected: %s\n", matches[0].ShortID)
			selected = &matches[0]
		} else {
			picked, err := selectMissionsFzf(entries, "Select mission to resume: ", false, strings.Join(args, " "))
			if err != nil {
				return err
			}
			if len(picked) == 0 {
				return nil
			}
			selected = &picked[0]
		}
	} else {
		picked, err := selectMissionsFzf(entries, "Select mission to resume: ", false, "")
		if err != nil {
			return err
		}
		if len(picked) == 0 {
			return nil
		}
		selected = &picked[0]
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
