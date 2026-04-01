package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/tableprinter"
)

var cronLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List all cron jobs",
	RunE:  runCronLs,
}

func init() {
	cronCmd.AddCommand(cronLsCmd)
}

func runCronLs(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	crons, err := client.ListCrons()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list crons")
	}

	if len(crons) == 0 {
		fmt.Println("No cron jobs defined.")
		fmt.Println("\nTo create a cron job, use 'agenc cron new' or ask the Adjutant.")
		return nil
	}

	tbl := tableprinter.NewTable("NAME", "SCHEDULE", "ENABLED", "LAST RUN", "STATUS")

	for _, c := range crons {
		enabled := "yes"
		if !c.Enabled {
			enabled = ansiYellow + "no" + ansiReset
		}

		lastRun, status := getCronLastRunStatus(client, c)

		tbl.AddRow(
			c.Name,
			c.Schedule,
			enabled,
			lastRun,
			status,
		)
	}

	tbl.Print()
	return nil
}

func getCronLastRunStatus(client *server.Client, cronInfo server.CronInfo) (string, string) {
	if cronInfo.ID == "" {
		return "--", "--"
	}

	missions, err := client.ListMissions(true, "cron", cronInfo.ID)
	if err != nil || len(missions) == 0 {
		return "--", "--"
	}

	mission := missions[0]
	lastRun := mission.CreatedAt.Local().Format("2006-01-02 15:04")

	status := getMissionStatus(mission.ID, mission.Status, mission.ClaudeState)
	coloredStatus := colorizeStatus(status)

	return lastRun, coloredStatus
}
