package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var inspectDirFlag bool

var missionInspectCmd = &cobra.Command{
	Use:   inspectCmdStr + " [mission-id]",
	Short: "Print information about a mission",
	Long: `Print information about a mission.

Without arguments, opens an interactive fzf picker to select a mission.
With an argument, inspects the specified mission by ID.`,
	Args: cobra.MaximumNArgs(1),
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

	var missionID string
	if len(args) > 0 {
		resolved, err := db.ResolveMissionID(args[0])
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve mission ID")
		}
		missionID = resolved
	} else {
		selected, err := selectMissionToInspect(db)
		if err != nil {
			return err
		}
		if selected == "" {
			return nil
		}
		missionID = selected
	}

	return inspectMission(db, missionID)
}

func selectMissionToInspect(db *database.DB) (string, error) {
	missions, err := db.ListMissions(true)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No missions.")
		return "", nil
	}

	entries, err := buildMissionPickerEntries(db, missions)
	if err != nil {
		return "", err
	}

	selected, err := selectMissionsFzf(entries, "Select mission to inspect: ", false, "")
	if err != nil {
		return "", err
	}
	if len(selected) == 0 {
		return "", nil
	}

	return selected[0].MissionID, nil
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

	cfg, _, cfgErr := config.ReadAgencConfig(agencDirpath)
	if cfgErr != nil {
		return stacktrace.Propagate(cfgErr, "failed to read config")
	}
	nicknames := buildNicknameMap(cfg.AgentTemplates)

	fmt.Printf("ID:          %s\n", mission.ShortID)
	fmt.Printf("Full ID:     %s\n", mission.ID)
	fmt.Printf("Status:      %s\n", getMissionStatus(missionID, mission.Status))
	fmt.Printf("Agent:       %s\n", displayAgentTemplate(mission.AgentTemplate, nicknames))
	if mission.GitRepo != "" {
		fmt.Printf("Git repo:    %s\n", displayGitRepo(mission.GitRepo))
	}
	claudeConfigDirpath := config.GetGlobalClaudeDirpath(agencDirpath)
	sessionName := resolveSessionName(claudeConfigDirpath, db, mission)
	if sessionName == "" {
		sessionName = "--"
	}
	fmt.Printf("Session:     %s\n", sessionName)
	prompt := resolveMissionPrompt(db, agencDirpath, mission)
	if prompt == "" {
		prompt = "--"
	}
	fmt.Printf("Prompt:      %s\n", prompt)
	fmt.Printf("Directory:   %s\n", missionDirpath)
	fmt.Printf("Created:     %s\n", mission.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated:     %s\n", mission.UpdatedAt.Format("2006-01-02 15:04:05"))

	return nil
}
