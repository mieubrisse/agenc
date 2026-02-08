package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
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
