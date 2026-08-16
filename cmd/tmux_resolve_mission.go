package cmd

import (
	"fmt"
	"strings"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
	"github.com/spf13/cobra"
)

var tmuxResolveMissionCmd = &cobra.Command{
	Use:   resolveMissionCmdStr + " <pane-id>",
	Short: "Resolve a tmux pane to its mission UUID",
	Long: `Looks up which mission is running in the given tmux pane.

Prints the mission UUID to stdout if found, or prints nothing if no active
mission is associated with the pane. Always exits with code 0.

This command is used internally by tmux keybindings to determine the focused
mission.`,
	Args: cobra.ExactArgs(1),
	RunE: runTmuxResolveMission,
}

func init() {
	tmuxCmd.AddCommand(tmuxResolveMissionCmd)
}

func runTmuxResolveMission(cmd *cobra.Command, args []string) error {
	// Normalize: strip leading "%" if present. $TMUX_PANE includes it (%42),
	// but tmux format variables like #{pane_id} omit it (42). The database
	// stores just the number.
	paneID := strings.TrimPrefix(args[0], "%")
	if paneID == "" {
		// An empty pane ID isn't a lookup miss — it's not a lookup at all.
		// Without this guard it falls through to the /missions?tmux_pane=
		// server handler's `if tmuxPane != ""` check, which treats an empty
		// value as "no pane filter" and returns the general mission list
		// instead of an empty one; this command would then print
		// responses[0].ID — some unrelated mission — instead of nothing.
		return nil
	}

	dirpath, err := config.GetAgencDirpath()
	if err != nil {
		return nil // silently exit — no mission
	}

	// Try the server first
	socketFilepath := config.GetServerSocketFilepath(dirpath)
	client := server.NewClient(socketFilepath)
	var responses []server.MissionResponse
	if err := client.Get("/missions?tmux_pane="+paneID, &responses); err == nil {
		if len(responses) > 0 {
			fmt.Print(responses[0].ID)
		}
		return nil
	}

	// Fall back to direct database access
	dbFilepath := config.GetDatabaseFilepath(dirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return nil // silently exit — no mission
	}
	defer db.Close()

	mission, err := db.GetMissionByTmuxPane(paneID)
	if err != nil || mission == nil {
		return nil // not found — print nothing, exit 0
	}

	fmt.Print(mission.ID)
	return nil
}
