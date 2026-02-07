package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var missionRmCmd = &cobra.Command{
	Use:   rmCmdStr + " [mission-id|search-terms...]",
	Short: "Stop and permanently remove one or more missions",
	Long: `Stop and permanently remove one or more missions.

Without arguments, opens an interactive fzf picker showing all missions.
With arguments, accepts a mission ID (short or full UUID) or search terms to
filter the list. If exactly one mission matches search terms, it is auto-selected.`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionRm,
}

func init() {
	missionCmd.AddCommand(missionRmCmd)
}

func runMissionRm(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: true})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No missions to remove.")
		return nil
	}

	entries, err := buildMissionPickerEntries(db, missions)
	if err != nil {
		return err
	}

	input := strings.Join(args, " ")
	result, err := Resolve(input, Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := db.ResolveMissionID(input)
			if err != nil {
				return missionPickerEntry{}, false, stacktrace.Propagate(err, "failed to resolve mission ID")
			}
			// Find the entry in our missions list
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s not found", input)
		},
		GetItems:    func() ([]missionPickerEntry, error) { return entries, nil },
		ExtractText: formatMissionMatchLine,
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Status, e.Agent, e.Session, e.Repo}
		},
		FzfPrompt:   "Select missions to remove (TAB to multi-select): ",
		FzfHeaders:  []string{"LAST ACTIVE", "ID", "STATUS", "AGENT", "SESSION", "REPO"},
		MultiSelect: true,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	// Print auto-select message only if search terms (not UUID) matched exactly one
	if input != "" && !looksLikeMissionID(input) && len(result.Items) == 1 {
		fmt.Printf("Auto-selected: %s\n", result.Items[0].ShortID)
	}

	for _, entry := range result.Items {
		if err := removeMission(db, entry.MissionID); err != nil {
			return err
		}
	}
	return nil
}

// removeMission tears down a mission in the reverse order of `mission new`:
// mission new creates DB record then directory, so we remove directory then DB record.
func removeMission(db *database.DB, missionID string) error {
	if _, err := prepareMissionForAction(db, missionID); err != nil {
		return err
	}

	// Remove the mission directory (agent/ is just a directory copy, so RemoveAll handles it)
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	if _, err := os.Stat(missionDirpath); err == nil {
		if err := os.RemoveAll(missionDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to remove mission directory '%s'", missionDirpath)
		}
	}

	// Delete from database
	if err := db.DeleteMission(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to delete mission from database")
	}

	fmt.Printf("Removed mission: %s\n", database.ShortID(missionID))
	return nil
}
