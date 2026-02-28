package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/wrapper"
)

var resumeFocusFlag bool
var runWrapperFlag bool
var resumePromptFlag string

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
	missionResumeCmd.Flags().BoolVar(&runWrapperFlag, runWrapperFlagName, false, "run the wrapper process directly (internal use)")
	missionResumeCmd.Flags().StringVar(&resumePromptFlag, promptFlagName, "", "initial prompt (internal use)")
	missionResumeCmd.Flags().MarkHidden(runWrapperFlagName)
	missionResumeCmd.Flags().MarkHidden(promptFlagName)
}

func runMissionResume(cmd *cobra.Command, args []string) error {
	// Internal mode: run the wrapper directly in the current process.
	// Used by the server when spawning wrapper processes in tmux pool windows.
	if runWrapperFlag {
		if len(args) != 1 {
			return stacktrace.NewError("--run-wrapper requires exactly one mission ID argument")
		}
		return runWrapperDirect(args[0], resumePromptFlag)
	}

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

// runWrapperDirect runs the wrapper process directly in the current process.
// This is the code path used by tmux pool windows to actually start the
// wrapper that manages the Claude child process. The missionID must be a
// full UUID. The initialPrompt is optional; if non-empty, it is passed to
// Claude when starting a new conversation.
func runWrapperDirect(missionID string, initialPrompt string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	missionRecord, err := client.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	// Check if the wrapper is already running for this mission
	pidFilepath := config.GetMissionPIDFilepath(agencDirpath, missionID)
	pid, err := server.ReadPID(pidFilepath)
	if err == nil && server.IsProcessRunning(pid) {
		return stacktrace.NewError("mission '%s' is already running (wrapper PID %d)", missionID, pid)
	}

	// Check for old-format mission (no agent/ subdirectory)
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)
	if _, err := os.Stat(agentDirpath); os.IsNotExist(err) {
		return stacktrace.NewError(
			"mission '%s' uses the old directory format (no agent/ subdirectory); "+
				"please archive it with '%s %s %s %s' and create a new mission",
			missionID, agencCmdStr, missionCmdStr, archiveCmdStr, missionID,
		)
	}

	// Determine if this is a resume (existing conversation) or a fresh start
	hasConversation := claudeconfig.GetLastSessionID(agencDirpath, missionID) != ""

	windowTitle := lookupWindowTitle(agencDirpath, missionRecord.GitRepo)
	if config.IsMissionAdjutant(agencDirpath, missionID) {
		windowTitle = "🤖  Adjutant"
	}

	w := wrapper.NewWrapper(agencDirpath, missionID, missionRecord.GitRepo, windowTitle, initialPrompt)
	return w.Run(hasConversation)
}
