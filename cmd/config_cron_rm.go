package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
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

	cfg, cm, err := readConfigWithComments()
	if err != nil {
		return err
	}

	if _, exists := cfg.Crons[name]; !exists {
		return stacktrace.NewError("cron job '%s' not found in config", name)
	}

	delete(cfg.Crons, name)

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Removed cron job '%s'\n", name)
	return nil
}
