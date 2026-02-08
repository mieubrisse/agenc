package cmd

import (
	"fmt"
	"os/exec"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var tmuxRmCmd = &cobra.Command{
	Use:   rmCmdStr,
	Short: "Destroy the AgenC tmux session, stopping all running missions",
	Long: `Destroy the AgenC tmux session. Killing the session sends SIGHUP to all
wrapper processes, which triggers graceful shutdown (forwarding to Claude,
waiting for exit, cleaning up PID files).`,
	Args: cobra.NoArgs,
	RunE: runTmuxRm,
}

func init() {
	tmuxCmd.AddCommand(tmuxRmCmd)
}

func runTmuxRm(cmd *cobra.Command, args []string) error {
	if !tmuxSessionExists(tmuxSessionName) {
		fmt.Println("No agenc tmux session found.")
		return nil
	}

	// Kill the tmux session. This sends SIGHUP to all processes in the session.
	// The wrapper handles SIGHUP gracefully (forwards to Claude, waits for exit,
	// runs deferred cleanup including PID file removal).
	if err := exec.Command("tmux", "kill-session", "-t", tmuxSessionName).Run(); err != nil {
		return stacktrace.Propagate(err, "failed to kill tmux session")
	}

	fmt.Println("AgenC tmux session destroyed.")
	return nil
}
