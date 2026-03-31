package cmd

import "github.com/spf13/cobra"

var configSleepCmd = &cobra.Command{
	Use:   "sleep",
	Short: "Manage sleep mode windows",
	Long: `Manage sleep mode time windows that block mission and cron creation.

When sleep mode is active, the server rejects new mission creation (except
cron-triggered missions). Configure time windows with day-of-week and
HH:MM start/end times. Overnight windows are supported.`,
}

func init() {
	configCmd.AddCommand(configSleepCmd)
}
