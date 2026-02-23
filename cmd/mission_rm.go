package cmd

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
)

var missionRmCmd = &cobra.Command{
	Use:   rmCmdStr + " [mission-id...]",
	Short: "Stop and permanently remove one or more missions",
	Long: `Stop and permanently remove one or more missions.

Without arguments, opens an interactive fzf picker showing all missions.
With arguments, accepts one or more mission IDs (short 8-char hex or full UUID).`,
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

	// When multiple args are provided and each looks like a mission ID,
	// resolve and remove each one directly without going through the picker.
	if len(args) > 1 && allLookLikeMissionIDs(args) {
		for _, idArg := range args {
			missionID, err := db.ResolveMissionID(idArg)
			if err != nil {
				return stacktrace.Propagate(err, "failed to resolve mission ID '%s'", idArg)
			}
			if err := removeMission(db, missionID); err != nil {
				return err
			}
		}
		return nil
	}

	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: true})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No missions to remove.")
		return nil
	}

	entries, err := buildMissionPickerEntries(db, missions, defaultPromptMaxLen)
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
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Status, e.Session, e.Repo}
		},
		FzfPrompt:         "Select missions to remove (TAB to multi-select): ",
		FzfHeaders:        []string{"LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO"},
		MultiSelect:       true,
		NotCanonicalError: "not a valid mission ID",
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	for _, entry := range result.Items {
		if err := removeMission(db, entry.MissionID); err != nil {
			return err
		}
	}
	return nil
}

// removeMission tears down a mission. Tries the server DELETE endpoint first,
// falling back to direct filesystem and database operations.
func removeMission(db *database.DB, missionID string) error {
	// Try the server first
	socketFilepath := config.GetServerSocketFilepath(agencDirpath)
	client := server.NewClient(socketFilepath)
	if err := client.Delete("/missions/" + missionID); err == nil {
		fmt.Printf("Removed mission: %s\n", database.ShortID(missionID))
		return nil
	}

	// Fall back to direct removal
	return removeMissionDirect(db, missionID)
}

// removeMissionDirect tears down a mission via direct filesystem and database
// operations when the server is unreachable.
func removeMissionDirect(db *database.DB, missionID string) error {
	if _, err := prepareMissionForAction(db, missionID); err != nil {
		return err
	}

	// Clean up per-mission Keychain credentials from the old auth system.
	// New missions don't create these entries, but old missions may still have them.
	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(agencDirpath, missionID)
	if err := claudeconfig.DeleteKeychainCredentials(claudeConfigDirpath); err != nil {
		log.Printf("Warning: failed to delete Keychain credentials for mission %s: %v", database.ShortID(missionID), err)
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
