package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var cronRmCmd = &cobra.Command{
	Use:   rmCmdStr + " <name>",
	Short: "Remove a cron job from config",
	Args:  cobra.ExactArgs(1),
	RunE:  runCronRm,
}

func init() {
	cronCmd.AddCommand(cronRmCmd)
}

func runCronRm(cmd *cobra.Command, args []string) error {
	name := args[0]

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	if _, exists := cfg.Crons[name]; !exists {
		return stacktrace.NewError("cron job '%s' not found", name)
	}

	delete(cfg.Crons, name)

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Removed cron job '%s'\n", name)
	return nil
}
