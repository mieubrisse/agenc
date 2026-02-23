package cmd

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
)

const (
	wrapperStopTimeout = 10 * time.Second
	wrapperStopTick    = 100 * time.Millisecond
)

var missionStopCmd = &cobra.Command{
	Use:   stopCmdStr + " [mission-id...]",
	Short: "Stop one or more mission wrapper processes",
	Long: `Stop one or more mission wrapper processes.

Without arguments, opens an interactive fzf picker showing running missions.
With arguments, accepts a mission ID (short 8-char hex or full UUID).`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionStop,
}

func init() {
	missionCmd.AddCommand(missionStopCmd)
}

func runMissionStop(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	runningMissions := filterRunningMissions(missions)
	if len(runningMissions) == 0 {
		fmt.Println("No running missions to stop.")
		return nil
	}

	entries, err := buildMissionPickerEntries(db, runningMissions, defaultPromptMaxLen)
	if err != nil {
		return err
	}

	input := strings.Join(args, " ")
	result, err := Resolve(input, Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := db.ResolveMissionID(input)
			if err != nil {
				return missionPickerEntry{}, false, stacktrace.Propagate(err, "failed to resolve mission ID")
			}
			// Find the entry in our running missions list
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s is not running", input)
		},
		GetItems: func() ([]missionPickerEntry, error) { return entries, nil },
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Session, e.Repo}
		},
		FzfPrompt:         "Select missions to stop (TAB to multi-select): ",
		FzfHeaders:        []string{"LAST ACTIVE", "ID", "SESSION", "REPO"},
		MultiSelect:       true,
		NotCanonicalError: "not a valid mission ID",
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	for _, entry := range result.Items {
		if err := stopMissionWrapper(entry.MissionID); err != nil {
			return err
		}
	}
	return nil
}

// stopMissionWrapper gracefully stops a mission's wrapper process. Tries the
// server endpoint first, falling back to direct process management if the
// server is unreachable. This is idempotent: if the wrapper is already stopped,
// it returns nil without error.
func stopMissionWrapper(missionID string) error {
	// Try the server first
	socketFilepath := config.GetServerSocketFilepath(agencDirpath)
	client := server.NewClient(socketFilepath)
	if err := client.Post("/missions/"+missionID+"/stop", nil, nil); err == nil {
		fmt.Printf("Mission '%s' stopped.\n", database.ShortID(missionID))
		return nil
	}

	// Fall back to direct process management
	return stopMissionWrapperDirect(missionID)
}

// stopMissionWrapperDirect stops a mission's wrapper via direct process
// management (SIGTERM + poll + SIGKILL fallback).
func stopMissionWrapperDirect(missionID string) error {
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
