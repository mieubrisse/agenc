package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var inspectDirFlag bool

var missionInspectCmd = &cobra.Command{
	Use:   inspectCmdStr + " [mission-id|search-terms...]",
	Short: "Print information about a mission",
	Long: `Print information about a mission.

Without arguments, opens an interactive fzf picker to select a mission.
With arguments, accepts a mission ID (short or full UUID) or search terms to
filter the list. If exactly one mission matches search terms, it is auto-selected.`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionInspect,
}

func init() {
	missionInspectCmd.Flags().BoolVar(&inspectDirFlag, dirFlagName, false, "print only the mission directory path")
	missionCmd.AddCommand(missionInspectCmd)
}

func runMissionInspect(cmd *cobra.Command, args []string) error {
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
		fmt.Println("No missions.")
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
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Status, e.Session, e.Repo}
		},
		FzfPrompt:        "Select mission to inspect: ",
		FzfHeaders:       []string{"LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO"},
		MultiSelect:      false,
		NotCanonicalError: "not a valid mission ID",
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	selected := result.Items[0]

	// Print auto-select message only if search terms (not UUID) matched exactly one
	if input != "" && !looksLikeMissionID(input) {
		fmt.Printf("Auto-selected: %s\n", selected.ShortID)
	}

	return inspectMission(db, selected.MissionID)
}

func inspectMission(db *database.DB, missionID string) error {
	mission, err := db.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)

	if inspectDirFlag {
		fmt.Println(missionDirpath)
		return nil
	}

	fmt.Printf("ID:          %s\n", mission.ShortID)
	fmt.Printf("Full ID:     %s\n", mission.ID)
	fmt.Printf("Status:      %s\n", getMissionStatus(missionID, mission.Status))
	if config.IsMissionAssistant(agencDirpath, missionID) {
		fmt.Printf("Type:        💁‍♂️  AgenC Assistant\n")
	} else if mission.GitRepo != "" {
		fmt.Printf("Git repo:    %s\n", displayGitRepo(mission.GitRepo))
	}
	sessionName := resolveSessionName(db, mission)
	if sessionName == "" {
		sessionName = "--"
	}
	fmt.Printf("Session:     %s\n", sessionName)
	prompt := resolveMissionPrompt(db, mission)
	if prompt == "" {
		prompt = "--"
	}
	fmt.Printf("Prompt:      %s\n", prompt)
	fmt.Printf("Directory:   %s\n", missionDirpath)
	fmt.Printf("Created:     %s\n", mission.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated:     %s\n", mission.UpdatedAt.Format("2006-01-02 15:04:05"))

	return nil
}
