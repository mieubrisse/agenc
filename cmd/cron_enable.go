package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var cronEnableCmd = &cobra.Command{
	Use:   enableCmdStr + " <name>",
	Short: "Enable a cron job",
	Args:  cobra.ExactArgs(1),
	RunE:  runCronEnable,
}

func init() {
	cronCmd.AddCommand(cronEnableCmd)
}

func runCronEnable(cmd *cobra.Command, args []string) error {
	return setCronEnabled(args[0], true)
}

func setCronEnabled(name string, enabled bool) error {
	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	cronCfg, exists := cfg.Crons[name]
	if !exists {
		return stacktrace.NewError("cron job '%s' not found", name)
	}

	if cronCfg.IsEnabled() == enabled {
		if enabled {
			fmt.Printf("Cron job '%s' is already enabled\n", name)
		} else {
			fmt.Printf("Cron job '%s' is already disabled\n", name)
		}
		return nil
	}

	cronCfg.Enabled = &enabled
	cfg.Crons[name] = cronCfg

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	if enabled {
		fmt.Printf("Enabled cron job '%s'\n", name)
		nextRun, err := config.GetNextCronRun(cronCfg.Schedule)
		if err == nil {
			fmt.Printf("Next run: %s\n", nextRun.Local().Format("2006-01-02 15:04:05"))
		}
	} else {
		fmt.Printf("Disabled cron job '%s'\n", name)
	}

	return nil
}
