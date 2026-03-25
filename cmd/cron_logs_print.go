package cmd

import (
	"fmt"
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var cronLogsPrintAllFlag bool

var cronLogsPrintCmd = &cobra.Command{
	Use:   printCmdStr + " <name-or-id>",
	Short: "Print cron job log content",
	Long: `Print the log output for a cron job.

The argument can be a cron name (as defined in config.yml) or a cron UUID.

By default, prints the last 200 lines. Use --all for the full file.

Examples:
  agenc cron logs print daily-report
  agenc cron logs print daily-report --all
  agenc cron logs print abc-123`,
	Args: cobra.ExactArgs(1),
	RunE: runCronLogsPrint,
}

func init() {
	cronLogsPrintCmd.Flags().BoolVar(&cronLogsPrintAllFlag, allFlagName, false, "print entire log file instead of last 200 lines")
	cronLogsCmd.AddCommand(cronLogsPrintCmd)
}

func runCronLogsPrint(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return stacktrace.Propagate(err, "failed to connect to server")
	}

	cronID, err := resolveCronID(client, args[0])
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve cron job identifier")
	}

	body, err := client.GetCronLogs(cronID, cronLogsPrintAllFlag)
	if err != nil {
		return fmt.Errorf("failed to fetch cron logs: %w", err)
	}

	_, _ = os.Stdout.Write(body) // stdout write failure is unrecoverable
	return nil
}
