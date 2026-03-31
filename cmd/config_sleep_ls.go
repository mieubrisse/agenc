package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configSleepLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List sleep mode windows",
	RunE:  runConfigSleepLs,
}

func init() {
	configSleepCmd.AddCommand(configSleepLsCmd)
}

func runConfigSleepLs(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}
	windows, err := client.ListSleepWindows()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list sleep windows")
	}
	if len(windows) == 0 {
		fmt.Println("No sleep windows configured")
		return nil
	}
	for i, w := range windows {
		fmt.Printf("  %d: %s %s\u2013%s\n", i, strings.Join(w.Days, ","), w.Start, w.End)
	}
	return nil
}
