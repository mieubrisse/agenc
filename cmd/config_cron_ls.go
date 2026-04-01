package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/tableprinter"
)

var configCronLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List cron jobs",
	Long: `List all cron jobs configured in config.yml.

Displays each cron job's name, schedule, and enabled status.`,
	RunE: runConfigCronLs,
}

func init() {
	configCronCmd.AddCommand(configCronLsCmd)
}

func runConfigCronLs(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	crons, err := client.ListCrons()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list crons")
	}

	if len(crons) == 0 {
		fmt.Println("No cron jobs configured.")
		return nil
	}

	tbl := tableprinter.NewTable("NAME", "SCHEDULE", "ENABLED", "PROMPT")
	for _, c := range crons {
		enabled := "true"
		if !c.Enabled {
			enabled = "false"
		}

		prompt := c.Prompt
		if len(prompt) > 60 {
			prompt = prompt[:57] + "..."
		}

		tbl.AddRow(c.Name, c.Schedule, enabled, prompt)
	}

	tbl.Print()
	return nil
}
