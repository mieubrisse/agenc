package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var stashPopCmd = &cobra.Command{
	Use:   popCmdStr,
	Short: "Restore missions from a stash",
	Long: `Restore all missions from a previously saved stash. Missions are
re-started and their windows are linked back into the tmux sessions
they were in at the time of the stash.

If there is only one stash, it is restored automatically. If multiple
stashes exist, an interactive picker is shown.`,
	RunE: runStashPop,
}

func init() {
	stashCmd.AddCommand(stashPopCmd)
}

type stashPickerEntry struct {
	StashID      string
	CreatedAt    string
	MissionCount int
}

func runStashPop(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	stashes, err := client.ListStashes()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list stashes")
	}

	if len(stashes) == 0 {
		fmt.Println("No stashed workspaces.")
		return nil
	}

	// If only one stash, use it directly
	var selectedStashID string
	if len(stashes) == 1 {
		selectedStashID = stashes[0].StashID
	} else {
		// Build picker entries
		entries := make([]stashPickerEntry, len(stashes))
		for i, s := range stashes {
			entries[i] = stashPickerEntry{
				StashID:      s.StashID,
				CreatedAt:    s.CreatedAt.Local().Format("2006-01-02 15:04:05"),
				MissionCount: s.MissionCount,
			}
		}

		result, err := Resolve("", Resolver[stashPickerEntry]{
			TryCanonical: func(input string) (stashPickerEntry, bool, error) {
				return stashPickerEntry{}, false, nil
			},
			GetItems: func() ([]stashPickerEntry, error) { return entries, nil },
			FormatRow: func(e stashPickerEntry) []string {
				return []string{e.CreatedAt, e.StashID, fmt.Sprintf("%d missions", e.MissionCount)}
			},
			FzfPrompt:         "Select stash to restore: ",
			FzfHeaders:        []string{"CREATED", "ID", "MISSIONS"},
			MultiSelect:       false,
			NotCanonicalError: "not a valid stash ID",
		})
		if err != nil {
			return err
		}

		if result.WasCancelled || len(result.Items) == 0 {
			return nil
		}
		selectedStashID = result.Items[0].StashID
	}

	popResp, err := client.PopStash(selectedStashID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to pop stash")
	}

	fmt.Printf("Restored %d mission(s).\n", popResp.MissionsRestored)
	return nil
}
