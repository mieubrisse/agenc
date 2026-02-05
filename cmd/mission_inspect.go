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
	Use:   inspectCmdStr + " <mission-id>",
	Short: "Print information about a mission",
	Args:  cobra.ExactArgs(1),
	RunE:  runMissionInspect,
}

func init() {
	missionInspectCmd.Flags().BoolVar(&inspectDirFlag, dirFlagName, false, "print only the mission directory path")
	missionCmd.AddCommand(missionInspectCmd)
}

func runMissionInspect(cmd *cobra.Command, args []string) error {
	return resolveAndRunForMission(args[0], func(db *database.DB, missionID string) error {
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
	})
}
