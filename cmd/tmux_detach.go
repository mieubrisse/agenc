package cmd

import (
	"os"
	"os/exec"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var tmuxDetachCmd = &cobra.Command{
	Use:   detachCmdStr,
	Short: "Detach from the AgenC tmux session",
	Args:  cobra.NoArgs,
	RunE:  runTmuxDetach,
}

func init() {
	tmuxCmd.AddCommand(tmuxDetachCmd)
}

func runTmuxDetach(cmd *cobra.Command, args []string) error {
	detachCmd := exec.Command("tmux", "detach-client")
	detachCmd.Stdin = os.Stdin
	detachCmd.Stdout = os.Stdout
	detachCmd.Stderr = os.Stderr

	if err := detachCmd.Run(); err != nil {
		return stacktrace.Propagate(err, "failed to detach from tmux session")
	}

	return nil
}
