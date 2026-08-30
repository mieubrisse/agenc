package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/session"
)

var inspectDirFlag bool

var missionInspectCmd = &cobra.Command{
	Use:     inspectCmdStr + " [mission-id]",
	Aliases: []string{statusCmdStr},
	Short:   "Print information about a mission",
	Long: `Print information about a mission.

Without arguments, opens an interactive fzf picker to select a mission.
With arguments, accepts a mission ID (short 8-char hex or full UUID).`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionInspect,
}

func init() {
	missionInspectCmd.Flags().BoolVar(&inspectDirFlag, dirFlagName, false, "print only the mission directory path")
	missionCmd.AddCommand(missionInspectCmd)
}

func runMissionInspect(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(server.ListMissionsRequest{IncludeArchived: true})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No missions.")
		return nil
	}

	entries := buildMissionPickerEntries(missions, defaultPromptMaxLen)

	input := strings.Join(args, " ")
	result, err := Resolve(input, Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := client.ResolveMissionID(input)
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
			return []string{e.ShortID, e.LastPrompt, e.Status, e.Session, e.Repo}
		},
		FzfPrompt:         "Select mission to inspect: ",
		FzfHeaders:        []string{"ID", "LAST PROMPT", "STATUS", "SESSION", "REPO"},
		MultiSelect:       false,
		NotCanonicalError: "not a valid mission ID",
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get agenc directory path")
	}
	return inspectMission(agencDirpath, result.Items[0].MissionID)
}

// formatPeerAddress describes how Claude Code's SendMessage tool can reach the
// mission, distinguishing a mission with no live Claude session from one whose
// peer identity could not be determined.
func formatPeerAddress(mission *database.Mission) string {
	if mission.PeerName != "" {
		return fmt.Sprintf("%s  (tmux %s)", mission.PeerName, mission.PeerTmuxTarget)
	}
	if isMissionRunning(getMissionStatus(mission.ID, mission.Status, mission.ClaudeState)) {
		return "--  (running, but Claude Code reports no peer for it)"
	}
	return "--  (no live Claude session)"
}

func inspectMission(agencDirpath string, missionID string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}
	mission, err := client.GetMission(missionID)
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
	fmt.Printf("Status:      %s\n", getMissionStatus(missionID, mission.Status, mission.ClaudeState))
	cfg, _, _ := config.ReadAgencConfig(agencDirpath)
	isAdjutant := config.IsMissionAdjutant(agencDirpath, missionID)
	if isAdjutant {
		fmt.Printf("Type:        🤖  Adjutant\n")
	} else if mission.GitRepo != "" {
		fmt.Printf("Git repo:    %s\n", displayGitRepo(mission.GitRepo))
		repoDisplay := formatRepoDisplay(mission.GitRepo, false, cfg)
		if repoDisplay != displayGitRepo(mission.GitRepo) {
			fmt.Printf("Title:       %s\n", repoDisplay)
		}
	}
	sessionName := resolveSessionName(mission)
	if sessionName == "" {
		sessionName = "--"
	}
	fmt.Printf("Session:     %s\n", sessionName)
	fmt.Printf("Peer:        %s\n", formatPeerAddress(mission))
	prompt := mission.Prompt
	if prompt == "" {
		prompt = "--"
	}
	fmt.Printf("Prompt:      %s\n", prompt)
	fmt.Printf("Directory:   %s\n", missionDirpath)
	fmt.Printf("Created:     %s\n", mission.CreatedAt.Format("2006-01-02 15:04:05"))
	fmt.Printf("Updated:     %s\n", mission.UpdatedAt.Format("2006-01-02 15:04:05"))

	// List session UUIDs
	projectDirpath, err := claudeconfig.GetMissionProjectDirpath(agencDirpath, missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get project directory for mission %s", missionID)
	}
	sessionIDs := session.ListSessionIDs(projectDirpath)
	currentSessionID := claudeconfig.GetLastSessionID(agencDirpath, missionID)

	if len(sessionIDs) == 0 {
		fmt.Printf("Sessions:    --\n")
	} else {
		fmt.Printf("Sessions:    %d total\n", len(sessionIDs))
		for _, sid := range sessionIDs {
			marker := "  "
			suffix := ""
			if sid == currentSessionID {
				marker = "* "
				suffix = "  (current)"
			}
			fmt.Printf("             %s%s%s\n", marker, sid, suffix)
		}
	}

	return nil
}
