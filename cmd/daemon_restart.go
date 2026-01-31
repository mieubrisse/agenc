package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
)

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the background daemon",
	RunE:  runDaemonRestart,
}

func init() {
	daemonCmd.AddCommand(daemonRestartCmd)
}

func runDaemonRestart(cmd *cobra.Command, args []string) error {
	pidFilepath := config.GetDaemonPIDFilepath(agencDirpath)
	logFilepath := config.GetDaemonLogFilepath(agencDirpath)

	pid, err := daemon.ReadPID(pidFilepath)
	if err != nil {
		return err
	}

	if pid > 0 && daemon.IsProcessRunning(pid) {
		if err := daemon.StopDaemon(pidFilepath); err != nil {
			return err
		}
		fmt.Println("Daemon stopped.")
	}

	if err := daemon.ForkDaemon(logFilepath, pidFilepath); err != nil {
		return err
	}

	newPID, _ := daemon.ReadPID(pidFilepath)
	fmt.Printf("Daemon started (PID %d).\n", newPID)

	return nil
}
