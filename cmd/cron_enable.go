package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/server"
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
	client, err := serverClient()
	if err != nil {
		return err
	}

	if _, err := client.UpdateCron(name, server.UpdateCronRequest{
		Enabled: &enabled,
	}); err != nil {
		return stacktrace.Propagate(err, "failed to update cron job")
	}

	if enabled {
		fmt.Printf("Enabled cron job '%s'\n", name)
	} else {
		fmt.Printf("Disabled cron job '%s'\n", name)
	}

	return nil
}
