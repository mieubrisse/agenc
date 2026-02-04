package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
)

var daemonStopCmd = &cobra.Command{
	Use:   stopCmdStr,
	Short: "Stop the background daemon",
	RunE:  runDaemonStop,
}

func init() {
	daemonCmd.AddCommand(daemonStopCmd)
}

func runDaemonStop(cmd *cobra.Command, args []string) error {
	pidFilepath := config.GetDaemonPIDFilepath(agencDirpath)

	pid, err := daemon.ReadPID(pidFilepath)
	if err != nil {
		return err
	}

	if pid == 0 || !daemon.IsProcessRunning(pid) {
		fmt.Println("Daemon is not running.")
		return nil
	}

	if err := daemon.StopDaemon(pidFilepath); err != nil {
		return err
	}

	fmt.Println("Daemon stopped.")
	return nil
}
