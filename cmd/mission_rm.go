package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var missionRmCmd = &cobra.Command{
	Use:   rmCmdStr + " [mission-id...]",
	Short: "Stop and permanently remove one or more missions",
	Args:  cobra.ArbitraryArgs,
	RunE:  runMissionRm,
}

func init() {
	missionCmd.AddCommand(missionRmCmd)
}

func runMissionRm(cmd *cobra.Command, args []string) error {
	return resolveAndRunForEachMission(args, selectMissionsToRemove, removeMission)
}

func selectMissionsToRemove(db *database.DB) ([]string, error) {
	missions, err := db.ListMissions(true)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No missions to remove.")
		return nil, nil
	}

	var fzfLines []string
	for _, m := range missions {
		label := truncatePrompt(resolveMissionPrompt(db, agencDirpath, m), 60)
		statusTag := ""
		if m.Status == "archived" {
			statusTag = " [archived]"
		}
		createdDate := m.CreatedAt.Format("2006-01-02 15:04")
		fzfLines = append(fzfLines, fmt.Sprintf("%s\t%s%s\t%s", m.ShortID, label, statusTag, createdDate))
	}

	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH; pass mission IDs as arguments instead")
	}

	fzfCmd := exec.Command(fzfBinary,
		"--multi",
		"--prompt", "Select missions to remove (TAB to multi-select): ",
		"--tabstop", "4",
	)
	fzfCmd.Stdin = strings.NewReader(strings.Join(fzfLines, "\n"))
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		// fzf returns exit code 130 on Ctrl-C, and exit code 1 when no match
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
			return nil, nil
		}
		return nil, stacktrace.Propagate(err, "fzf selection failed")
	}

	var selectedIDs []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// The mission ID is the first field (tab-separated)
		id, _, _ := strings.Cut(line, "\t")
		selectedIDs = append(selectedIDs, id)
	}

	return selectedIDs, nil
}

// removeMission tears down a mission in the reverse order of `mission new`:
// mission new creates DB record then directory, so we remove directory then DB record.
func removeMission(db *database.DB, missionID string) error {
	// Fetch mission record to confirm it exists
	if _, err := db.GetMission(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	// Stop the wrapper if running (idempotent)
	if err := stopMissionWrapper(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to stop wrapper for mission '%s'", missionID)
	}

	// Remove the mission directory (workspace is just a directory copy, so RemoveAll handles it)
	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)
	if _, err := os.Stat(missionDirpath); err == nil {
		if err := os.RemoveAll(missionDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to remove mission directory '%s'", missionDirpath)
		}
	}

	// Delete from database
	if err := db.DeleteMission(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to delete mission from database")
	}

	fmt.Printf("Removed mission: %s\n", database.ShortID(missionID))
	return nil
}
