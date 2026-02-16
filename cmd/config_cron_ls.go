package cmd

import (
	"fmt"
	"sort"

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
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	if len(cfg.Crons) == 0 {
		fmt.Println("No cron jobs configured.")
		return nil
	}

	// Sort by name for consistent output
	names := make([]string, 0, len(cfg.Crons))
	for name := range cfg.Crons {
		names = append(names, name)
	}
	sort.Strings(names)

	tbl := tableprinter.NewTable("NAME", "SCHEDULE", "ENABLED", "PROMPT")
	for _, name := range names {
		cronCfg := cfg.Crons[name]

		enabled := "true"
		if cronCfg.Enabled != nil && !*cronCfg.Enabled {
			enabled = "false"
		}

		prompt := cronCfg.Prompt
		if len(prompt) > 60 {
			prompt = prompt[:57] + "..."
		}

		tbl.AddRow(name, cronCfg.Schedule, enabled, prompt)
	}

	tbl.Print()
	return nil
}
