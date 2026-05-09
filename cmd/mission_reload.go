package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
)

var (
	reloadPromptFlag string
	reloadAsyncFlag  bool
)

var missionReloadCmd = &cobra.Command{
	Use:   reloadCmdStr + " [mission-id]",
	Short: "Reload a mission in-place (preserves tmux pane)",
	Long: `Reload a mission in-place (preserves tmux pane).

Stops the mission wrapper and restarts it in the same tmux pane, preserving
window position, title, and conversation state. Useful after updating the
mission config or upgrading the agenc binary.

Without arguments, opens an interactive fzf picker showing running missions.
With an argument, accepts a mission ID (short 8-char hex or full UUID).

The --prompt flag, when set, is fed to Claude as a follow-up message that
runs immediately after the reload completes. The mission must have a live
tmux pane for --prompt to apply.

The --async flag queues the reload to fire on Claude's next idle (when its
current turn finishes) instead of bouncing immediately. AGENTS RELOADING
THEMSELVES SHOULD ALWAYS USE --async: a synchronous self-reload kills
Claude mid-tool-call, which discards the bash tool result from Claude's
conversation history. Async preserves the tool call and the prompt arrives
cleanly on the next turn. Returns 202 Accepted; if Claude is already idle,
the reload fires immediately.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runMissionReload,
}

func init() {
	missionCmd.AddCommand(missionReloadCmd)
	missionReloadCmd.Flags().StringVar(&reloadPromptFlag, promptFlagName, "", "follow-up prompt to send after reload (requires a mission with a live tmux pane)")
	missionReloadCmd.Flags().BoolVar(&reloadAsyncFlag, asyncFlagName, false, "queue the reload for Claude's next idle (REQUIRED when an agent reloads itself, to preserve the calling tool result)")
}

func runMissionReload(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	if len(args) == 1 {
		input := args[0]
		if !looksLikeMissionID(input) {
			return stacktrace.NewError("not a valid mission ID: %s", input)
		}
		missionID, err := client.ResolveMissionID(input)
		if err != nil {
			return stacktrace.Propagate(err, "failed to resolve mission ID")
		}
		if err := client.ReloadMission(missionID, reloadPromptFlag, reloadAsyncFlag); err != nil {
			return stacktrace.Propagate(err, "failed to reload mission %s", database.ShortID(missionID))
		}
		fmt.Println(reloadResultMessage(missionID, reloadAsyncFlag))
		return nil
	}

	// No args: list running missions and show fzf single-select picker
	missions, err := client.ListMissions(server.ListMissionsRequest{})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	runningMissions := filterRunningMissions(missions)
	if len(runningMissions) == 0 {
		fmt.Println("No running missions to reload.")
		return nil
	}

	entries := buildMissionPickerEntries(runningMissions, defaultPromptMaxLen)

	result, err := Resolve("", Resolver[missionPickerEntry]{
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.ShortID, e.LastPrompt, e.Session, e.Repo}
		},
		FzfPrompt:  "Select mission to reload: ",
		FzfHeaders: []string{"ID", "LAST PROMPT", "SESSION", "REPO"},
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	entry := result.Items[0]
	if err := client.ReloadMission(entry.MissionID, reloadPromptFlag, reloadAsyncFlag); err != nil {
		return stacktrace.Propagate(err, "failed to reload mission %s", entry.ShortID)
	}
	fmt.Println(reloadResultMessage(entry.MissionID, reloadAsyncFlag))
	return nil
}

func reloadResultMessage(missionID string, async bool) string {
	if async {
		return fmt.Sprintf("Reload queued for mission '%s' (will fire on Claude's next idle)", database.ShortID(missionID))
	}
	return fmt.Sprintf("Mission '%s' reloaded", database.ShortID(missionID))
}
