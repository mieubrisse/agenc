package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var attachNoFocusFlag bool

var missionAttachCmd = &cobra.Command{
	Use:   attachCmdStr + " [mission-id]",
	Short: "Attach a mission to the current tmux session",
	Long: `Attach a mission to the current tmux session.

Links the mission's tmux window into your session and focuses it.
If the mission is already linked, just focuses the window.
Stopped missions are automatically resumed; archived missions are unarchived first.

Without arguments, opens an interactive fzf picker showing all missions.
With arguments, accepts a mission ID (short 8-char hex or full UUID).`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionAttach,
}

func init() {
	missionCmd.AddCommand(missionAttachCmd)
	missionAttachCmd.Flags().BoolVar(&attachNoFocusFlag, noFocusFlagName, false, "don't focus the mission's tmux window after attaching")
}

func runMissionAttach(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	tmuxSession := getCurrentTmuxSessionName()
	if tmuxSession == "" {
		return stacktrace.NewError("mission attach requires tmux; run inside a tmux session")
	}

	missions, err := client.ListMissions(true, "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		return stacktrace.NewError("no missions to attach")
	}

	sortMissionsForPicker(missions)
	entries := buildMissionPickerEntries(missions, 100)

	result, err := Resolve(strings.Join(args, " "), Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := client.ResolveMissionID(input)
			if err != nil {
				return missionPickerEntry{}, false, stacktrace.Propagate(err, "failed to resolve mission ID")
			}
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s not found", input)
		},
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Session, e.Repo}
		},
		FzfPrompt:         "Select mission to attach: ",
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

	missionID := result.Items[0].MissionID

	// Migrate old .assistant marker if present
	if err := config.MigrateAssistantMarkerIfNeeded(agencDirpath, missionID); err != nil {
		return stacktrace.Propagate(err, "failed to migrate assistant marker")
	}

	fmt.Printf("Attaching mission: %s\n", database.ShortID(missionID))

	if err := client.AttachMission(missionID, tmuxSession, attachNoFocusFlag); err != nil {
		return stacktrace.Propagate(err, "failed to attach mission")
	}

	return nil
}
