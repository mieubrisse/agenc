package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var doctorCmd = &cobra.Command{
	Use:   doctorCmdStr,
	Short: "Check for common configuration issues",
	Args:  cobra.NoArgs,
	RunE:  runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

// checkResult represents the outcome of a single doctor check.
type checkResult struct {
	name    string
	passed  bool
	message string // shown when the check does not pass
}

func runDoctor(cmd *cobra.Command, args []string) error {
	checks := []checkResult{
		checkTmuxKeybindingsInjected(),
		checkOAuthTokenPermissions(),
		checkWrapperSocketPermissions(),
	}

	allPassed := true
	for _, check := range checks {
		if check.passed {
			fmt.Printf("  OK  %s\n", check.name)
		} else {
			allPassed = false
			fmt.Printf("  --  %s\n", check.name)
			fmt.Printf("      %s\n", check.message)
		}
	}

	if allPassed {
		fmt.Println("\nAll checks passed.")
	}

	return nil
}

// checkTmuxKeybindingsInjected verifies that the user's tmux.conf contains
// the AgenC keybindings sentinel block.
func checkTmuxKeybindingsInjected() checkResult {
	name := "tmux keybindings injected"

	tmuxConfFilepath, exists, err := findTmuxConfFilepath()
	if err != nil {
		return checkResult{
			name:    name,
			passed:  false,
			message: fmt.Sprintf("could not locate tmux.conf: %v", err),
		}
	}

	if !exists {
		return checkResult{
			name:    name,
			passed:  false,
			message: fmt.Sprintf("no tmux.conf found; run '%s %s %s' to install keybindings", agencCmdStr, tmuxCmdStr, injectCmdStr),
		}
	}

	content, err := os.ReadFile(tmuxConfFilepath)
	if err != nil {
		return checkResult{
			name:    name,
			passed:  false,
			message: fmt.Sprintf("could not read %s: %v", tmuxConfFilepath, err),
		}
	}

	if strings.Contains(string(content), sentinelBegin) {
		return checkResult{name: name, passed: true}
	}

	return checkResult{
		name:    name,
		passed:  false,
		message: fmt.Sprintf("run '%s %s %s' to install keybindings", agencCmdStr, tmuxCmdStr, injectCmdStr),
	}
}

// checkOAuthTokenPermissions verifies that the OAuth token file has
// restrictive permissions (mode 0600) to prevent leakage.
//
// Under State Y, an absent token file is normal and expected: AgenC missions
// default to native Claude authentication (set up via 'claude auth login').
// The token file is only present when the user has explicitly stored a
// long-lived token via 'agenc token set' as a State-X fallback.
func checkOAuthTokenPermissions() checkResult {
	name := "OAuth token file permissions"

	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return checkResult{
			name:    name,
			passed:  false,
			message: fmt.Sprintf("could not determine agenc directory: %v", err),
		}
	}

	tokenFilepath := config.GetOAuthTokenFilepath(agencDirpath)
	info, err := os.Stat(tokenFilepath)
	if err != nil {
		if os.IsNotExist(err) {
			// No token file is fine under State Y — native Claude auth is the default.
			// Users who need an explicit token can run: agenc token set <token>
			return checkResult{name: name, passed: true}
		}
		return checkResult{
			name:    name,
			passed:  false,
			message: fmt.Sprintf("could not stat %s: %v", tokenFilepath, err),
		}
	}

	mode := info.Mode().Perm()
	if mode != 0600 {
		return checkResult{
			name:    name,
			passed:  false,
			message: fmt.Sprintf("OAuth token file has permissions %o (should be 0600); fix with: chmod 0600 %s", mode, tokenFilepath),
		}
	}

	return checkResult{name: name, passed: true}
}

// checkWrapperSocketPermissions verifies that active wrapper sockets have
// restrictive permissions (mode 0600) to prevent unauthorized control.
func checkWrapperSocketPermissions() checkResult {
	name := "wrapper socket permissions"

	agencDirpath, err := config.GetAgencDirpath()
	if err != nil {
		return checkResult{
			name:    name,
			passed:  false,
			message: fmt.Sprintf("could not determine agenc directory: %v", err),
		}
	}

	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return checkResult{
			name:    name,
			passed:  false,
			message: fmt.Sprintf("could not open database: %v", err),
		}
	}
	defer db.Close()

	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return checkResult{
			name:    name,
			passed:  false,
			message: fmt.Sprintf("could not list missions: %v", err),
		}
	}

	// Check permissions on all wrapper sockets for active missions
	var badSockets []string
	for _, mission := range missions {
		socketFilepath := config.GetMissionSocketFilepath(agencDirpath, mission.ID)
		info, err := os.Stat(socketFilepath)
		if err != nil {
			if os.IsNotExist(err) {
				// Socket doesn't exist — wrapper not running, which is fine
				continue
			}
			return checkResult{
				name:    name,
				passed:  false,
				message: fmt.Sprintf("could not stat %s: %v", socketFilepath, err),
			}
		}

		mode := info.Mode().Perm()
		if mode != 0600 {
			badSockets = append(badSockets, fmt.Sprintf("%s (%s, mode %o)", mission.ShortID, socketFilepath, mode))
		}
	}

	if len(badSockets) > 0 {
		message := "wrapper sockets have overly permissive permissions (should be 0600):\n"
		for _, socket := range badSockets {
			message += fmt.Sprintf("      %s\n", socket)
		}
		message += "      Fix: restart the affected missions"
		return checkResult{
			name:    name,
			passed:  false,
			message: message,
		}
	}

	return checkResult{name: name, passed: true}
}
