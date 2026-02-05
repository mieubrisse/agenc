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
)

const (
	wrapperStopTimeout = 10 * time.Second
	wrapperStopTick    = 100 * time.Millisecond
)

var missionStopCmd = &cobra.Command{
	Use:   stopCmdStr + " [mission-id|search-terms...]",
	Short: "Stop one or more mission wrapper processes",
	Long: `Stop one or more mission wrapper processes.

Without arguments, opens an interactive fzf picker showing running missions.
With arguments, accepts a mission ID (short or full UUID) or search terms to
filter the list. If exactly one mission matches search terms, it is auto-selected.`,
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

	missions, err := db.ListMissions(false)
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	runningMissions := filterRunningMissions(missions)
	if len(runningMissions) == 0 {
		fmt.Println("No running missions to stop.")
		return nil
	}

	entries, err := buildMissionPickerEntries(db, runningMissions)
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
		GetItems:    func() ([]missionPickerEntry, error) { return entries, nil },
		ExtractText: formatMissionMatchLine,
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Agent, e.Session, e.Repo}
		},
		FzfPrompt:   "Select missions to stop (TAB to multi-select): ",
		FzfHeaders:  []string{"LAST ACTIVE", "ID", "AGENT", "SESSION", "REPO"},
		MultiSelect: true,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	// Print auto-select message only if search terms (not UUID) matched exactly one
	if input != "" && !looksLikeMissionID(input) && len(result.Items) == 1 {
		fmt.Printf("Auto-selected: %s\n", result.Items[0].ShortID)
	}

	for _, entry := range result.Items {
		if err := stopMissionWrapper(entry.MissionID); err != nil {
			return err
		}
	}
	return nil
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
