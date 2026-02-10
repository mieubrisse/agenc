package cmd

import (
	"fmt"
	"strings"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
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

	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return nil // silently exit — no mission
	}

	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
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
