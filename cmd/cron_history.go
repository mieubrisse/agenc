package cmd

import (
	"fmt"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/tableprinter"
)

var cronHistoryLimitFlag int

var cronHistoryCmd = &cobra.Command{
	Use:   historyCmdStr + " <name>",
	Short: "Show run history for a cron job",
	Long: `Show the history of runs for a specific cron job.

Lists missions spawned by the cron scheduler, including their status and duration.

Example:
  agenc cron history daily-report
  agenc cron history daily-report --limit 50
`,
	Args: cobra.ExactArgs(1),
	RunE: runCronHistory,
}

func init() {
	cronHistoryCmd.Flags().IntVar(&cronHistoryLimitFlag, "limit", 20, "maximum number of entries to show")
	cronCmd.AddCommand(cronHistoryCmd)
}

func runCronHistory(cmd *cobra.Command, args []string) error {
	name := args[0]

	// Verify cron exists
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	if _, exists := cfg.Crons[name]; !exists {
		return stacktrace.NewError("cron job '%s' not found", name)
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	missions, err := client.ListMissions(true, name)
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Printf("No runs found for cron job '%s'\n", name)
		return nil
	}

	// Limit the number of entries
	displayMissions := missions
	if len(missions) > cronHistoryLimitFlag {
		displayMissions = missions[:cronHistoryLimitFlag]
	}

	fmt.Printf("Run history for cron job '%s':\n\n", name)

	tbl := tableprinter.NewTable("STARTED", "ID", "STATUS", "DURATION")

	for _, m := range displayMissions {
		status := getMissionStatus(m.ID, m.Status)
		coloredStatus := colorizeStatus(status)

		started := m.CreatedAt.Local().Format("2006-01-02 15:04:05")

		// Calculate duration based on last heartbeat or current time
		var duration string
		if m.LastHeartbeat != nil {
			d := m.LastHeartbeat.Sub(m.CreatedAt)
			duration = formatMissionDuration(d)
		} else {
			duration = "--"
		}

		tbl.AddRow(started, m.ShortID, coloredStatus, duration)
	}

	tbl.Print()

	if len(missions) > cronHistoryLimitFlag {
		fmt.Printf("\n...showing %d of %d runs. Use --limit to see more.\n", cronHistoryLimitFlag, len(missions))
	}

	return nil
}

// formatMissionDuration formats a duration for display in the history table.
func formatMissionDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
