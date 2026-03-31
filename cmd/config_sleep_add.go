package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/sleep"
)

var configSleepAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a sleep mode window",
	Long: `Add a time window during which mission and cron creation is blocked.

Examples:
  agenc config sleep add --days mon,tue,wed,thu --start 22:00 --end 06:00
  agenc config sleep add --days fri,sat --start 23:00 --end 07:00`,
	RunE: runConfigSleepAdd,
}

func init() {
	configSleepCmd.AddCommand(configSleepAddCmd)
	configSleepAddCmd.Flags().String("days", "", "comma-separated day names (mon,tue,wed,thu,fri,sat,sun) (required)")
	configSleepAddCmd.Flags().String("start", "", "start time in HH:MM format (required)")
	configSleepAddCmd.Flags().String("end", "", "end time in HH:MM format (required)")
	_ = configSleepAddCmd.MarkFlagRequired("days")
	_ = configSleepAddCmd.MarkFlagRequired("start")
	_ = configSleepAddCmd.MarkFlagRequired("end")
}

func runConfigSleepAdd(cmd *cobra.Command, args []string) error {
	daysStr, _ := cmd.Flags().GetString("days")
	start, _ := cmd.Flags().GetString("start")
	end, _ := cmd.Flags().GetString("end")

	days := strings.Split(daysStr, ",")

	client, err := serverClient()
	if err != nil {
		return err
	}
	windows, err := client.AddSleepWindow(sleep.WindowDef{
		Days:  days,
		Start: start,
		End:   end,
	})
	if err != nil {
		return stacktrace.Propagate(err, "failed to add sleep window")
	}

	fmt.Printf("Added sleep window: %s %s\u2013%s (window %d)\n", daysStr, start, end, len(windows)-1)
	return nil
}
