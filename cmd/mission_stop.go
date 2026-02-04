package cmd

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
	"github.com/odyssey/agenc/internal/database"
)

const (
	wrapperStopTimeout = 10 * time.Second
	wrapperStopTick    = 100 * time.Millisecond
)

var missionStopCmd = &cobra.Command{
	Use:   stopCmdStr + " <mission-id>",
	Short: "Stop a mission's wrapper process (no-op if already stopped)",
	Args:  cobra.ExactArgs(1),
	RunE:  runMissionStop,
}

func init() {
	missionCmd.AddCommand(missionStopCmd)
}

func runMissionStop(cmd *cobra.Command, args []string) error {
	return resolveAndRunForMission(args[0], func(_ *database.DB, missionID string) error {
		return stopMissionWrapper(missionID)
	})
}

// stopMissionWrapper gracefully stops a mission's wrapper process if it is
// running. This is idempotent: if the wrapper is already stopped, it returns
// nil without error.
func stopMissionWrapper(missionID string) error {
	pidFilepath := config.GetMissionPIDFilepath(agencDirpath, missionID)
	pid, err := daemon.ReadPID(pidFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read mission PID file")
	}

	if pid == 0 || !daemon.IsProcessRunning(pid) {
		// Already stopped — clean up stale PID file if present
		os.Remove(pidFilepath)
		return nil
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return stacktrace.Propagate(err, "failed to find wrapper process")
	}

	if err := process.Signal(syscall.SIGTERM); err != nil {
		return stacktrace.Propagate(err, "failed to send SIGTERM to wrapper (PID %d)", pid)
	}

	fmt.Printf("Sent SIGTERM to wrapper (PID %d), waiting for exit...\n", pid)

	deadline := time.Now().Add(wrapperStopTimeout)
	for time.Now().Before(deadline) {
		if !daemon.IsProcessRunning(pid) {
			os.Remove(pidFilepath)
			fmt.Printf("Mission '%s' stopped.\n", database.ShortID(missionID))
			return nil
		}
		time.Sleep(wrapperStopTick)
	}

	// Force kill if still running
	_ = process.Signal(syscall.SIGKILL)
	os.Remove(pidFilepath)
	fmt.Printf("Mission '%s' force-killed after timeout.\n", database.ShortID(missionID))

	return nil
}
