package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var cronHealthCmd = &cobra.Command{
	Use:   healthCmdStr,
	Short: "Show which cron jobs have stopped producing missions",
	Long: `Show which cron jobs have stopped producing missions.

A cron that stops running says nothing on its own — launchd can fail to spawn it
before agenc ever executes, which writes no log, creates no mission and posts no
notification. The server watches for that absence and sends a notification when a
cron has skipped two of its own scheduled cycles. This command shows the same
picture on demand, and never notifies or changes anything.

Examples:
  agenc cron health
`,
	Args: cobra.NoArgs,
	RunE: runCronHealth,
}

func init() {
	cronCmd.AddCommand(cronHealthCmd)
}

func runCronHealth(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	health, err := client.GetCronHealth()
	if err != nil {
		return stacktrace.Propagate(err, "failed to read cron health")
	}

	if health.LastBackgroundCheckAt == nil {
		fmt.Println("The background cron check has not run yet; it starts a few minutes after the server does.")
	} else {
		fmt.Printf("Background cron check last ran %v.\n", health.LastBackgroundCheckAt.Local().Format("2006-01-02 15:04:05"))
	}
	fmt.Println()

	if len(health.QuietCrons) == 0 {
		fmt.Printf("No cron jobs have gone quiet. %d enabled cron job(s) checked.\n", health.MonitoredCronCount)
		return nil
	}

	fmt.Printf("%d of %d enabled cron job(s) have not produced a mission recently:\n", len(health.QuietCrons), health.MonitoredCronCount)
	for _, quietCron := range health.QuietCrons {
		fmt.Println()
		fmt.Printf("  %v\n", quietCron.Name)
		fmt.Printf("    %v\n", quietCron.Detail)
		if quietCron.LaunchdStatus != "" {
			fmt.Printf("    %v\n", quietCron.LaunchdStatus)
		}
		if quietCron.AlreadyReported {
			fmt.Println("    Already mentioned in a notification.")
		} else {
			fmt.Println("    Not yet mentioned in a notification; the next background check will do it.")
		}
	}

	return nil
}
