package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/version"
)

var daemonStatusJSON bool

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status and description stats",
	RunE:  runDaemonStatus,
}

func init() {
	daemonStatusCmd.Flags().BoolVar(&daemonStatusJSON, "json", false, "output in JSON format")
	daemonCmd.AddCommand(daemonStatusCmd)
}

type daemonStatusOutput struct {
	Version              string `json:"version"`
	DaemonRunning        bool   `json:"daemon_running"`
	DaemonPID            int    `json:"daemon_pid"`
	LogFilepath          string `json:"log_filepath"`
	DescriptionsGenerated int   `json:"descriptions_generated"`
	DescriptionsPending   int   `json:"descriptions_pending"`
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	pidFilepath := config.GetDaemonPIDFilepath(agencDirpath)
	logFilepath := config.GetDaemonLogFilepath(agencDirpath)

	pid, err := daemon.ReadPID(pidFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read daemon PID")
	}

	running := pid > 0 && daemon.IsProcessRunning(pid)

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

	if daemonStatusJSON {
		output := daemonStatusOutput{
			Version:               version.Version,
			DaemonRunning:         running,
			DaemonPID:             pid,
			LogFilepath:           logFilepath,
			DescriptionsGenerated: described,
			DescriptionsPending:   pending,
		}
		encoded, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return stacktrace.Propagate(err, "failed to marshal JSON output")
		}
		fmt.Println(string(encoded))
		return nil
	}

	fmt.Printf("Version:      %s\n", version.Version)
	if running {
		fmt.Printf("Daemon:       running (PID %d)\n", pid)
	} else {
		fmt.Println("Daemon:       stopped")
	}
	fmt.Printf("Log file:     %s\n", logFilepath)
	fmt.Printf("Descriptions: %d generated, %d pending\n", described, pending)

	return nil
}
