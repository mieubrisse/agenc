package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
)

var daemonStatusJSON bool

var daemonStatusCmd = &cobra.Command{
	Use:   statusCmdStr,
	Short: "Show daemon status",
	RunE:  runDaemonStatus,
}

func init() {
	daemonStatusCmd.Flags().BoolVar(&daemonStatusJSON, jsonFlagName, false, "output in JSON format")
	daemonCmd.AddCommand(daemonStatusCmd)
}

type daemonStatusOutput struct {
	Version       string `json:"version"`
	DaemonRunning bool   `json:"daemon_running"`
	DaemonPID     int    `json:"daemon_pid"`
	LogFilepath   string `json:"log_filepath"`
}

func runDaemonStatus(cmd *cobra.Command, args []string) error {
	if _, err := getAgencContext(); err != nil {
		return err
	}
	pidFilepath := config.GetDaemonPIDFilepath(agencDirpath)
	logFilepath := config.GetDaemonLogFilepath(agencDirpath)

	pid, err := daemon.ReadPID(pidFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read daemon PID")
	}

	running := pid > 0 && daemon.IsProcessRunning(pid)

	daemonVersion := ""
	if running {
		versionFilepath := config.GetDaemonVersionFilepath(agencDirpath)
		raw, err := os.ReadFile(versionFilepath)
		if err == nil {
			daemonVersion = strings.TrimSpace(string(raw))
		}
	}

	if daemonStatusJSON {
		output := daemonStatusOutput{
			Version:       daemonVersion,
			DaemonRunning: running,
			DaemonPID:     pid,
			LogFilepath:   logFilepath,
		}
		encoded, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return stacktrace.Propagate(err, "failed to marshal JSON output")
		}
		fmt.Println(string(encoded))
		return nil
	}

	if running {
		fmt.Printf("Daemon:       running (PID %d)\n", pid)
		fmt.Printf("Version:      %s\n", daemonVersion)
	} else {
		fmt.Println("Daemon:       stopped")
	}
	fmt.Printf("Log file:     %s\n", logFilepath)

	return nil
}
