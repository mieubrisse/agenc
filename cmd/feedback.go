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
  agenc tmux window new -a -- agenc mission new --adjutant --prompt "I'd like to send feedback about AgenC"`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmdArgs := []string{
			tmuxCmdStr,
			windowCmdStr,
			newCmdStr,
			"-a",
			"--",
			agencCmdStr,
			missionCmdStr,
			newCmdStr,
			"--adjutant",
			"--prompt",
			feedbackPrompt,
		}

		if err := exec.Command(os.Args[0], cmdArgs...).Run(); err != nil {
			return stacktrace.Propagate(err, "failed to launch feedback mission")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(feedbackCmd)
}
