package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
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
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	if len(cfg.Crons) == 0 {
		fmt.Println("No cron jobs defined.")
		fmt.Println("\nTo create a cron job, use 'agenc cron create' or ask the Adjutant.")
		return nil
	}

	client, err := serverClient()
	if err != nil {
		return err
	}

	tbl := tableprinter.NewTable("NAME", "SCHEDULE", "ENABLED", "LAST RUN", "STATUS")

	names := make([]string, 0, len(cfg.Crons))
	for name := range cfg.Crons {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		cronCfg := cfg.Crons[name]
		enabled := "yes"
		if !cronCfg.IsEnabled() {
			enabled = ansiYellow + "no" + ansiReset
		}

		lastRun, status := getCronLastRunStatus(client, cronCfg)

		tbl.AddRow(
			name,
			cronCfg.Schedule,
			enabled,
			lastRun,
			status,
		)
	}

	tbl.Print()
	return nil
}

func getCronLastRunStatus(client *server.Client, cronCfg config.CronConfig) (string, string) {
	if cronCfg.ID == "" {
		return "--", "--"
	}

	missions, err := client.ListMissions(true, "cron", cronCfg.ID)
	if err != nil || len(missions) == 0 {
		return "--", "--"
	}

	mission := missions[0]
	lastRun := mission.CreatedAt.Local().Format("2006-01-02 15:04")

	status := getMissionStatus(mission.ID, mission.Status, mission.ClaudeState)
	coloredStatus := colorizeStatus(status)

	return lastRun, coloredStatus
}
