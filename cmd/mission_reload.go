package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var missionReloadCmd = &cobra.Command{
	Use:   reloadCmdStr + " [mission-id...]",
	Short: "Reload one or more missions in-place (preserves tmux pane)",
	Long: `Reload one or more missions in-place (preserves tmux pane).

Stops the mission wrapper and restarts it in the same tmux pane, preserving
window position, title, and conversation state. This is useful after updating
the mission config or upgrading the agenc binary.

Without arguments, opens an interactive fzf picker showing running missions.
With arguments, accepts a mission ID (short 8-char hex or full UUID).

For missions running in tmux, uses 'remain-on-exit' and 'respawn-pane' to
preserve the exact pane position. For missions not in tmux, falls back to
stop + resume workflow.`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionReload,
}

func init() {
	missionCmd.AddCommand(missionReloadCmd)
}

func runMissionReload(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	runningMissions := filterRunningMissions(missions)
	if len(runningMissions) == 0 {
		fmt.Println("No running missions to reload.")
		return nil
	}

	entries, err := buildMissionPickerEntries(db, runningMissions)
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
			// Find the entry in our running missions list
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s is not running", input)
		},
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Session, e.Repo}
		},
		FzfPrompt:         "Select missions to reload (TAB to multi-select): ",
		FzfHeaders:        []string{"LAST ACTIVE", "ID", "SESSION", "REPO"},
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
		if err := reloadMission(db, entry.MissionID); err != nil {
			return err
		}
	}
	return nil
}

// reloadMission handles the reload logic for a single mission. It detects
// whether the mission is running in tmux and routes to the appropriate reload
// path. Returns an error if the mission doesn't exist, is archived, or uses
// the old directory format.
func reloadMission(db *database.DB, missionID string) error {
	missionRecord, err := db.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	if missionRecord.Status == "archived" {
		return stacktrace.NewError("cannot reload archived mission '%s'", database.ShortID(missionID))
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

	// Detect tmux context: if tmux_pane is set, verify pane still exists
	if missionRecord.TmuxPane != nil && *missionRecord.TmuxPane != "" {
		paneID := *missionRecord.TmuxPane
		if !tmuxPaneExists(paneID) {
			return stacktrace.NewError(
				"mission's tmux pane no longer exists; use '%s %s %s %s' to restart in a new window",
				agencCmdStr, missionCmdStr, resumeCmdStr, database.ShortID(missionID),
			)
		}
		return reloadMissionInTmux(missionRecord, paneID)
	}

	// Non-tmux path: warn and fall back to stop + resume
	fmt.Printf("⚠️  Mission is not running in tmux; reload will not preserve pane position\n")
	if err := stopMissionWrapper(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to stop wrapper")
	}
	return resumeMission(db, missionID)
}

// reloadMissionInTmux performs an in-place reload using tmux primitives:
// remain-on-exit to keep the pane alive, stop wrapper, respawn-pane to restart.
func reloadMissionInTmux(mission *database.Mission, paneID string) error {
	// Resolve window ID from pane ID
	windowID, err := tmuxGetWindowID(paneID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve window ID for pane %s", paneID)
	}

	// Set remain-on-exit on for the window to prevent pane from closing
	if err := tmuxSetRemainOnExit(windowID, true); err != nil {
		return stacktrace.Propagate(err, "failed to set remain-on-exit")
	}

	// Ensure we restore remain-on-exit even if subsequent steps fail
	defer func() {
		_ = tmuxSetRemainOnExit(windowID, false)
	}()

	// Stop wrapper gracefully (idempotent if already stopped)
	if err := stopMissionWrapper(mission.ID); err != nil {
		return stacktrace.Propagate(err, "failed to stop wrapper")
	}

	// Respawn the pane with agenc mission resume
	agencBinpath, err := os.Executable()
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve agenc binary path")
	}

	resumeCommand := fmt.Sprintf("'%s' %s %s %s", agencBinpath, missionCmdStr, resumeCmdStr, mission.ID)
	if err := tmuxRespawnPane(paneID, resumeCommand); err != nil {
		return stacktrace.Propagate(err, "failed to respawn pane")
	}

	// Restore remain-on-exit off (also covered by defer, but explicit for clarity)
	if err := tmuxSetRemainOnExit(windowID, false); err != nil {
		// Non-fatal: pane is already restarted, this is just cleanup
		fmt.Printf("⚠️  Warning: failed to restore remain-on-exit setting: %v\n", err)
	}

	fmt.Printf("Mission '%s' reloaded in-place\n", database.ShortID(mission.ID))
	return nil
}

// ============================================================================
// Tmux helper functions
// ============================================================================

// tmuxPaneExists checks whether a tmux pane with the given ID still exists.
// The pane ID should be the numeric form without the "%" prefix (as stored in DB).
func tmuxPaneExists(paneID string) bool {
	targetPane := "%" + paneID
	cmd := exec.Command("tmux", "display-message", "-p", "-t", targetPane, "#{pane_id}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	// Output should be "%<paneID>" if the pane exists
	return strings.TrimSpace(string(output)) == targetPane
}

// tmuxGetWindowID resolves the window ID for a given pane ID.
// The pane ID should be the numeric form without the "%" prefix (as stored in DB).
// Returns the window ID with "@" prefix (e.g., "@12").
func tmuxGetWindowID(paneID string) (string, error) {
	targetPane := "%" + paneID
	cmd := exec.Command("tmux", "display-message", "-p", "-t", targetPane, "#{window_id}")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", stacktrace.NewError("tmux display-message failed: %v (output: %s)", err, string(output))
	}
	windowID := strings.TrimSpace(string(output))
	if windowID == "" {
		return "", stacktrace.NewError("empty window ID returned for pane %s", paneID)
	}
	return windowID, nil
}

// tmuxSetRemainOnExit sets the remain-on-exit window option.
// When enabled, panes stay alive after the process exits (shown as "dead").
// The window ID should include the "@" prefix (e.g., "@12").
func tmuxSetRemainOnExit(windowID string, enable bool) error {
	value := "off"
	if enable {
		value = "on"
	}
	cmd := exec.Command("tmux", "set-option", "-w", "-t", windowID, "remain-on-exit", value)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.NewError("tmux set-option failed: %v (output: %s)", err, string(output))
	}
	return nil
}

// tmuxRespawnPane kills any existing process in the pane and respawns with the
// given command. The pane ID should be the numeric form without the "%" prefix.
func tmuxRespawnPane(paneID string, command string) error {
	targetPane := "%" + paneID
	cmd := exec.Command("tmux", "respawn-pane", "-k", "-t", targetPane, command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return stacktrace.NewError("tmux respawn-pane failed: %v (output: %s)", err, string(output))
	}
	return nil
}
