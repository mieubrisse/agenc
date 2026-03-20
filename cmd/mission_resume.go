package cmd

import (
	"bufio"
	"fmt"
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/wrapper"
)

var runWrapperFlag bool
var resumePromptFlag string

var missionResumeCmd = &cobra.Command{
	Use:    resumeCmdStr + " [mission-id]",
	Short:  "Internal: run the wrapper process directly",
	Hidden: true,
	Args:   cobra.ArbitraryArgs,
	RunE:   runMissionResume,
}

func init() {
	missionCmd.AddCommand(missionResumeCmd)
	missionResumeCmd.Flags().BoolVar(&runWrapperFlag, runWrapperFlagName, false, "run the wrapper process directly (internal use)")
	missionResumeCmd.Flags().StringVar(&resumePromptFlag, promptFlagName, "", "initial prompt (internal use)")
	missionResumeCmd.Flags().MarkHidden(runWrapperFlagName)
	missionResumeCmd.Flags().MarkHidden(promptFlagName)
}

func runMissionResume(cmd *cobra.Command, args []string) error {
	if runWrapperFlag {
		if len(args) != 1 {
			return stacktrace.NewError("--run-wrapper requires exactly one mission ID argument")
		}
		return runWrapperDirect(args[0], resumePromptFlag)
	}

	// Interactive resume has been folded into "mission attach".
	return stacktrace.NewError("use 'agenc mission attach' instead; 'mission resume' is now internal-only (--run-wrapper)")
}

// runWrapperDirect runs the wrapper process directly in the current process.
// This is the code path used by tmux pool windows to actually start the
// wrapper that manages the Claude child process. The missionID must be a
// full UUID. The initialPrompt is optional; if non-empty, it is passed to
// Claude when starting a new conversation.
//
// On error, this function pauses with a "Press Enter" prompt so the user
// can read the error message before the tmux pane closes.
func runWrapperDirect(missionID string, initialPrompt string) error {
	if err := doRunWrapperDirect(missionID, initialPrompt); err != nil {
		// Print the error and pause so the user can see it before the tmux
		// pane closes. Without this, wrapper startup errors vanish instantly
		// because the pane is destroyed when the process exits.
		fmt.Fprintf(os.Stderr, "\nWrapper failed: %v\n\nPress Enter to close this window.\n", err)
		bufio.NewReader(os.Stdin).ReadBytes('\n')
		return err
	}
	return nil
}

func doRunWrapperDirect(missionID string, initialPrompt string) error {
	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve agenc directory")
	}

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

	w := wrapper.NewWrapper(agencDirpath, missionID, missionRecord.GitRepo, initialPrompt)
	return w.Run(hasConversation)
}
