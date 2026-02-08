package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
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
		fmt.Printf("\nTo create a cron job, add an entry to %s under 'crons'.\n", config.GetConfigFilepath(agencDirpath))
		return nil
	}

	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	tbl := tableprinter.NewTable("NAME", "SCHEDULE", "ENABLED", "LAST RUN", "STATUS", "NEXT RUN")

	for name, cronCfg := range cfg.Crons {
		enabled := "yes"
		if !cronCfg.IsEnabled() {
			enabled = ansiYellow + "no" + ansiReset
		}

		lastRun, status := getCronLastRunStatus(db, name)
		nextRun := getNextRunDisplay(cronCfg.Schedule, cronCfg.IsEnabled())

		tbl.AddRow(
			name,
			cronCfg.Schedule,
			enabled,
			lastRun,
			status,
			nextRun,
		)
	}

	tbl.Print()
	return nil
}

func getCronLastRunStatus(db *database.DB, cronName string) (string, string) {
	mission, err := db.GetMostRecentMissionForCron(cronName)
	if err != nil || mission == nil {
		return "--", "--"
	}

	lastRun := mission.CreatedAt.Local().Format("2006-01-02 15:04")

	status := getMissionStatus(mission.ID, mission.Status)
	coloredStatus := colorizeStatus(status)

	return lastRun, coloredStatus
}

func getNextRunDisplay(schedule string, enabled bool) string {
	if !enabled {
		return ansiYellow + "disabled" + ansiReset
	}

	nextTime, err := config.GetNextCronRun(schedule)
	if err != nil {
		return "error"
	}

	// If next run is within 24 hours, show relative time
	until := time.Until(nextTime)
	if until < 24*time.Hour {
		return formatDuration(until)
	}

	return nextTime.Local().Format("2006-01-02 15:04")
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "< 1m"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if minutes == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	return fmt.Sprintf("%dh%dm", hours, minutes)
}
