package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var serverLogsPrintRequestsFlag bool
var serverLogsPrintAllFlag bool

var serverLogsPrintCmd = &cobra.Command{
	Use:   printCmdStr,
	Short: "Print server log content",
	Long: `Print the server operational log (default) or HTTP request log.

By default, prints the last 200 lines. Use --all for the full file.

Examples:
  agenc server logs print
  agenc server logs print --requests
  agenc server logs print --all`,
	RunE: runServerLogsPrint,
}

func init() {
	serverLogsPrintCmd.Flags().BoolVar(&serverLogsPrintRequestsFlag, "requests", false, "show HTTP request log instead of operational log")
	serverLogsPrintCmd.Flags().BoolVar(&serverLogsPrintAllFlag, allFlagName, false, "print entire log file instead of last 200 lines")
	serverLogsCmd.AddCommand(serverLogsPrintCmd)
}

func runServerLogsPrint(cmd *cobra.Command, args []string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	source := "server"
	if serverLogsPrintRequestsFlag {
		source = "requests"
	}

	body, err := client.GetServerLogs(source, serverLogsPrintAllFlag)
	if err != nil {
		return fmt.Errorf("failed to fetch server logs: %w", err)
	}

	os.Stdout.Write(body)
	return nil
}
