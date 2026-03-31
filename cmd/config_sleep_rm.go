package cmd

import (
	"fmt"
	"strconv"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"
)

var configSleepRmCmd = &cobra.Command{
	Use:   "rm <index>",
	Short: "Remove a sleep mode window",
	Long:  "Remove a sleep window by its index (shown in 'agenc config sleep ls').",
	Args:  cobra.ExactArgs(1),
	RunE:  runConfigSleepRm,
}

func init() {
	configSleepCmd.AddCommand(configSleepRmCmd)
}

func runConfigSleepRm(cmd *cobra.Command, args []string) error {
	index, err := strconv.Atoi(args[0])
	if err != nil {
		return stacktrace.NewError("invalid index %q: must be a number", args[0])
	}

	client, err := serverClient()
	if err != nil {
		return err
	}
	if err := client.RemoveSleepWindow(index); err != nil {
		return stacktrace.Propagate(err, "failed to remove sleep window")
	}

	fmt.Printf("Removed sleep window %d\n", index)
	return nil
}
