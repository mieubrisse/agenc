package cmd

import (
	"os/exec"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

const githubRepoURL = "https://github.com/mieubrisse/agenc"

var starCmd = &cobra.Command{
	Use:   starCmdStr,
	Short: "Open the AgenC GitHub repository in your browser",
	Long: `Opens the AgenC GitHub repository in your default browser.
This makes it easy to star the project, browse the code, or file issues.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := exec.Command("open", githubRepoURL).Run(); err != nil {
			return stacktrace.Propagate(err, "failed to open browser to GitHub repository")
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(starCmd)
}
