package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configCronRmCmd = &cobra.Command{
	Use:   rmCmdStr + " <name>",
	Short: "Remove a cron job",
	Long: `Remove a cron job from config.yml.

The cron job entry is completely removed from the configuration.

Example:
  agenc config cron rm daily-report
`,
	Args: cobra.ExactArgs(1),
	RunE: runConfigCronRm,
}

func init() {
	configCronCmd.AddCommand(configCronRmCmd)
}

func runConfigCronRm(cmd *cobra.Command, args []string) error {
	name := args[0]

	client, err := serverClient()
	if err != nil {
		return err
	}

	if err := client.DeleteCron(name); err != nil {
		return stacktrace.Propagate(err, "failed to remove cron job")
	}

	fmt.Printf("Removed cron job '%s'\n", name)
	return nil
}
