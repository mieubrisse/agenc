package cmd

import (
	"github.com/spf13/cobra"
)

var configCronCmd = &cobra.Command{
	Use:   cronCmdStr,
	Short: "Manage cron job configuration",
	Long: `Manage cron job configuration in config.yml.

Cron jobs run headless Claude missions on a schedule. Each cron job is
identified by a unique name and has the following configurable fields:

  schedule     - Cron expression (e.g., "0 9 * * *" for 9am daily)
  prompt       - Initial prompt for the Claude mission
  description  - Human-readable description (optional)
  git          - Git repository to clone into workspace (optional)
  timeout      - Maximum runtime (e.g., "1h", "30m") (optional)
  overlap      - Overlap policy: "skip" (default) or "allow" (optional)
  enabled      - Whether the cron job is enabled (defaults to true)

Example config.yml:

  crons:
    daily-report:
      schedule: "0 9 * * *"
      prompt: "Generate the daily status report"
      description: "Automated daily report generation"
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
	configCmd.AddCommand(configCronCmd)
}
