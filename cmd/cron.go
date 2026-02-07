package cmd

import (
	"github.com/spf13/cobra"
)

var cronCmd = &cobra.Command{
	Use:   cronCmdStr,
	Short: "Manage scheduled cron jobs",
	Long: `Manage scheduled cron jobs that run headless Claude missions on a schedule.

Cron jobs are defined in config.yml under the 'crons' key. Each cron job
specifies a schedule (cron expression), prompt, and optional git repository
to clone into the workspace.

Example config.yml:

  crons:
    daily-report:
      schedule: "0 9 * * *"
      prompt: "Generate the daily status report"
      git: github.com/owner/my-repo
      timeout: 30m
      enabled: true

    weekly-cleanup:
      schedule: "0 0 * * SUN"
      prompt: "Clean up old temporary files"
      overlap: skip
`,
}

func init() {
	rootCmd.AddCommand(cronCmd)
}
