package cmd

import (
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
)

var serverStatusCmd = &cobra.Command{
	Use:   statusCmdStr,
	Short: "Check AgenC server status",
	RunE:  runServerStatus,
}

func init() {
	serverCmd.AddCommand(serverStatusCmd)
}

func runServerStatus(cmd *cobra.Command, args []string) error {
	if _, err := ensureConfigured(); err != nil {
		return err
	}

	pidFilepath := config.GetServerPIDFilepath(agencDirpath)
	pid, err := server.ReadPID(pidFilepath)
	if err != nil {
		return err
	}

	if pid <= 0 || !server.IsRunning(pidFilepath) {
		fmt.Println("Server is not running.")
		return nil
	}

	fmt.Printf("Server is running (PID %d).\n", pid)

	// Try to get detailed health from the server
	socketFilepath := config.GetServerSocketFilepath(agencDirpath)
	client := server.NewClient(socketFilepath)
	health, err := client.GetHealth()
	if err != nil {
		fmt.Printf("  (could not reach health endpoint: %v)\n", err)
		return nil
	}

	if len(health.Loops) > 0 {
		fmt.Println()
		fmt.Println("Loops:")

		names := make([]string, 0, len(health.Loops))
		for name := range health.Loops {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			status := health.Loops[name]
			marker := ansiGreen + "●" + ansiReset
			if status == "crashed" {
				marker = ansiRed + "●" + ansiReset
			} else if status == "stopped" {
				marker = ansiYellow + "●" + ansiReset
			}
			fmt.Printf("  %s %-25s %s\n", marker, name, status)
		}
	}

	return nil
}
