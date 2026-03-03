package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var missionDetachCmd = &cobra.Command{
	Use:   detachCmdStr + " [mission-id]",
	Short: "Detach a mission from the current tmux session",
	Long: `Detach a mission from the current tmux session.

Unlinks the mission's tmux window from your session. The mission keeps
running in the pool and can be re-attached later with 'agenc mission attach'.

Without arguments, opens an interactive fzf picker showing missions
linked to the current session.
With arguments, accepts a mission ID (short 8-char hex or full UUID).`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionDetach,
}

func init() {
	missionCmd.AddCommand(missionDetachCmd)
}

func runMissionDetach(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	tmuxSession := getCurrentTmuxSessionName()
	if tmuxSession == "" {
		return stacktrace.NewError("mission detach requires tmux; run inside a tmux session")
	}

	missions, err := client.ListMissions(false, "")
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	linkedMissions := filterLinkedMissions(missions, tmuxSession)
	if len(linkedMissions) == 0 {
		return stacktrace.NewError("no missions linked to this tmux session")
	}

	entries := buildMissionPickerEntries(linkedMissions, 100)

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
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s is not linked to this session", input)
		},
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Session, e.Repo}
		},
		FzfPrompt:         "Select mission to detach: ",
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
	fmt.Printf("Detaching mission: %s\n", database.ShortID(missionID))

	if err := client.DetachMission(missionID, tmuxSession); err != nil {
		return stacktrace.Propagate(err, "failed to detach mission")
	}

	return nil
}
