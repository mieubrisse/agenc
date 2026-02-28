package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
)

var resumeFocusFlag bool

var missionResumeCmd = &cobra.Command{
	Use:   resumeCmdStr + " [mission-id]",
	Short: "Unarchive (if needed) and resume a mission with claude --continue",
	Long: `Unarchive (if needed) and resume a mission with claude --continue.

Without arguments, opens an interactive fzf picker showing stopped missions.
With arguments, accepts a mission ID (short 8-char hex or full UUID).`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionResume,
}

func init() {
	missionCmd.AddCommand(missionResumeCmd)
	missionResumeCmd.Flags().BoolVar(&resumeFocusFlag, focusFlagName, false, "focus the mission's tmux window after attaching")
}

func runMissionResume(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(false, "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	stoppedMissions := filterStoppedMissions(missions)
	if len(stoppedMissions) == 0 {
		return stacktrace.NewError("no stopped missions to resume")
	}

	entries := buildMissionPickerEntries(stoppedMissions, 100)

	result, err := Resolve(strings.Join(args, " "), Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := client.ResolveMissionID(input)
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
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Session, e.Repo}
		},
		FzfPrompt:         "Select mission to resume: ",
		FzfHeaders:        []string{"LAST ACTIVE", "ID", "SESSION", "REPO"},
		MultiSelect:       false,
		NotCanonicalError: "not a valid mission ID",
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	return resumeMission(client, result.Items[0].MissionID)
}

// resumeMission handles the per-mission resume logic: unarchive if needed
// and attach via the server (which ensures the wrapper is running in the
// tmux pool and links the window into the caller's session).
func resumeMission(client *server.Client, missionID string) error {
	tmuxSession := getCurrentTmuxSessionName()
	if tmuxSession == "" {
		return stacktrace.NewError("mission resume requires tmux; run inside a tmux session")
	}

	missionRecord, err := client.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	if missionRecord.Status == "archived" {
		if err := client.UnarchiveMission(missionID); err != nil {
			return stacktrace.Propagate(err, "failed to unarchive mission")
		}
		fmt.Printf("Unarchived mission: %s\n", database.ShortID(missionID))
	}

	// Migrate old .assistant marker if present
	if err := config.MigrateAssistantMarkerIfNeeded(agencDirpath, missionID); err != nil {
		return stacktrace.Propagate(err, "failed to migrate assistant marker")
	}

	fmt.Printf("Resuming mission: %s\n", database.ShortID(missionID))

	if err := client.AttachMission(missionID, tmuxSession); err != nil {
		return stacktrace.Propagate(err, "failed to attach mission")
	}

	if resumeFocusFlag {
		focusMissionWindow(missionRecord.ShortID, tmuxSession)
	}

	return nil
}
