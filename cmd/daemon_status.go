package cmd

import (
	"fmt"

	"github.com/kurtosis-tech/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
	"github.com/odyssey/agenc/internal/database"
)

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status and description stats",
	RunE:  runDaemonStatus,
}

func init() {
	daemonCmd.AddCommand(daemonStatusCmd)
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	pidFilepath := config.GetDaemonPIDFilepath(agencDirpath)
	logFilepath := config.GetDaemonLogFilepath(agencDirpath)

	pid, err := daemon.ReadPID(pidFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read daemon PID")
	}

	if pid > 0 && daemon.IsProcessRunning(pid) {
		fmt.Printf("Daemon:       running (PID %d)\n", pid)
	} else {
		fmt.Println("Daemon:       stopped")
	}

	fmt.Printf("Log file:     %s\n", logFilepath)

	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	described, pending, err := db.CountDescriptionStats()
	if err != nil {
		return stacktrace.Propagate(err, "failed to get description stats")
	}

	fmt.Printf("Descriptions: %d generated, %d pending\n", described, pending)

	return nil
}
