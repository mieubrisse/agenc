package cmd

import (
	"os"
	"os/exec"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

const feedbackPrompt = "I'd like to send feedback about AgenC"

var feedbackCmd = &cobra.Command{
	Use:   feedbackCmdStr,
	Short: "Launch a feedback mission with Adjutant",
	Long: `Launches a new tmux window with an Adjutant mission for sending feedback about AgenC.
This is a shorthand for:
  tmux new-window -a agenc mission new --adjutant --prompt "I'd like to send feedback about AgenC"`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		tmuxCmd := exec.Command("tmux", "new-window", "-a",
			"agenc", "mission", "new",
			"--adjutant",
			"--prompt", feedbackPrompt,
		)
		tmuxCmd.Stdout = os.Stdout
		tmuxCmd.Stderr = os.Stderr

		if err := tmuxCmd.Run(); err != nil {
			return stacktrace.Propagate(err, "failed to launch feedback mission")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(feedbackCmd)
}
