package cmd

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/database"
)

var loginCmd = &cobra.Command{
	Use:   loginCmdStr,
	Short: "Log in to Claude and update credentials for all missions",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Run claude login without CLAUDE_CONFIG_DIR so credentials are
		// written to the default Keychain entry ("Claude Code-credentials"),
		// which per-mission Keychain cloning reads from.
		claudeCmd := exec.Command("claude", "login")
		claudeCmd.Stdin = os.Stdin
		claudeCmd.Stdout = os.Stdout
		claudeCmd.Stderr = os.Stderr
		if err := claudeCmd.Run(); err != nil {
			return stacktrace.Propagate(err, "claude login failed")
		}

		// Update Keychain entries for all tracked missions
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		missions, err := db.ListMissions(database.ListMissionsParams{})
		if err != nil {
			return stacktrace.Propagate(err, "failed to list missions")
		}

		if len(missions) == 0 {
			return nil
		}

		fmt.Printf("Updating credentials for %d mission(s)...\n", len(missions))
		var failCount int
		for _, mission := range missions {
			configDirpath := claudeconfig.GetMissionClaudeConfigDirpath(agencDirpath, mission.ID)
			if err := claudeconfig.CloneKeychainCredentials(configDirpath); err != nil {
				fmt.Printf("  Warning: failed to update mission %s: %v\n", database.ShortID(mission.ID), err)
				failCount++
				continue
			}
		}

		if failCount > 0 {
			fmt.Printf("Updated %d/%d mission(s) (%d failed)\n", len(missions)-failCount, len(missions), failCount)
		} else {
			fmt.Printf("Updated %d mission(s)\n", len(missions))
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
