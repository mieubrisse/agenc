package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/mattn/go-isatty"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/wrapper"
)

var loginCmd = &cobra.Command{
	Use:   loginCmdStr,
	Short: "Log in to Claude and update credentials for all missions",
	RunE: func(cmd *cobra.Command, args []string) error {
		if !isatty.IsTerminal(os.Stdin.Fd()) {
			return stacktrace.NewError("'%s %s' requires a terminal for interactive authentication",
				agencCmdStr, loginCmdStr,
			)
		}

		// Run claude login in a disposable temp directory so that no
		// project-level .claude/ config interferes. CLAUDE_CONFIG_DIR is
		// intentionally unset so credentials are written to the default
		// Keychain entry ("Claude Code-credentials"), which per-mission
		// Keychain cloning reads from.
		tmpDirpath, err := os.MkdirTemp("", "agenc-login-*")
		if err != nil {
			return stacktrace.Propagate(err, "failed to create temp directory for login")
		}
		defer os.RemoveAll(tmpDirpath)

		fmt.Println("A Claude shell will open. Once inside:")
		fmt.Println("  1. Type /login")
		fmt.Println("  2. Click \"Authorize\" in the browser")
		fmt.Println("  3. Exit Claude (Ctrl-C or /exit)")
		fmt.Println()
		fmt.Println("agenc will then propagate the credentials to all your missions.")
		fmt.Print("\nPress ENTER to continue...")
		bufio.NewReader(os.Stdin).ReadBytes('\n')

		// Snapshot existing global credentials before login so we can
		// preserve MCP OAuth tokens. claude login overwrites the global
		// Keychain entry with a fresh blob that only contains claudeAiOauth,
		// destroying any mcpOAuth tokens that were merged back from missions.
		preLoginCreds, _ := claudeconfig.ReadKeychainCredentials(claudeconfig.GlobalCredentialServiceName)

		claudeCmd := exec.Command("claude")
		claudeCmd.Dir = tmpDirpath
		claudeCmd.Stdin = os.Stdin
		claudeCmd.Stdout = os.Stdout
		claudeCmd.Stderr = os.Stderr
		if err := claudeCmd.Run(); err != nil {
			return stacktrace.Propagate(err, "claude login failed")
		}

		// Restore MCP OAuth tokens that were wiped by claude login.
		// MergeCredentialJSON(base=old, overlay=new): overlay wins for
		// non-mcpOAuth keys (so fresh claudeAiOauth is kept), while
		// mcpOAuth entries from the old snapshot are preserved since they
		// only exist on the base side.
		if preLoginCreds != "" {
			postLoginCreds, readErr := claudeconfig.ReadKeychainCredentials(claudeconfig.GlobalCredentialServiceName)
			if readErr == nil {
				merged, changed, mergeErr := claudeconfig.MergeCredentialJSON([]byte(preLoginCreds), []byte(postLoginCreds))
				if mergeErr == nil && changed {
					if writeErr := claudeconfig.WriteKeychainCredentials(claudeconfig.GlobalCredentialServiceName, string(merged)); writeErr != nil {
						fmt.Printf("Warning: failed to restore MCP OAuth tokens: %v\n", writeErr)
					}
				}
			}
		}

		// Signal running missions to restart with fresh credentials.
		// Stopped missions will pick up new credentials automatically
		// when they are next resumed (the wrapper clones at spawn time).
		db, err := openDB()
		if err != nil {
			return err
		}
		defer db.Close()

		missions, err := db.ListMissions(database.ListMissionsParams{})
		if err != nil {
			return stacktrace.Propagate(err, "failed to list missions")
		}

		runningMissions := filterRunningMissions(missions)
		if len(runningMissions) == 0 {
			fmt.Println("No running missions. Stopped missions will pick up new credentials on next resume.")
			return nil
		}

		fmt.Printf("Signaling %d running mission(s) to reload credentials...\n", len(runningMissions))
		var signalCount int
		for _, m := range runningMissions {
			socketFilepath := config.GetMissionSocketFilepath(agencDirpath, m.ID)
			_, err := wrapper.SendCommand(socketFilepath, wrapper.Command{
				Command: "restart",
				Mode:    "graceful",
				Reason:  "credentials_changed",
			})
			if err != nil {
				if errors.Is(err, wrapper.ErrWrapperNotRunning) {
					// Race condition: wrapper exited between status check and socket send
					continue
				}
				fmt.Printf("  Warning: failed to signal mission %s: %v\n", database.ShortID(m.ID), err)
				continue
			}
			signalCount++
		}

		fmt.Printf("Signaled %d running mission(s) to reload credentials. Stopped missions will pick up new credentials on next resume.\n", signalCount)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(loginCmd)
}
