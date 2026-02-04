package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/database"
)

var missionArchiveCmd = &cobra.Command{
	Use:   archiveCmdStr + " [mission-id...]",
	Short: "Stop and archive one or more missions",
	Args:  cobra.ArbitraryArgs,
	RunE:  runMissionArchive,
}

func init() {
	missionCmd.AddCommand(missionArchiveCmd)
}

func runMissionArchive(cmd *cobra.Command, args []string) error {
	return resolveAndRunForEachMission(args, selectMissionsToArchive, archiveMission)
}

func selectMissionsToArchive(db *database.DB) ([]string, error) {
	missions, err := db.ListMissions(false)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list active missions")
	}

	if len(missions) == 0 {
		fmt.Println("No active missions to archive.")
		return nil, nil
	}

	var fzfLines []string
	for _, m := range missions {
		label := truncatePrompt(resolveMissionPrompt(db, agencDirpath, m), 60)
		createdDate := m.CreatedAt.Format("2006-01-02 15:04")
		fzfLines = append(fzfLines, fmt.Sprintf("%s\t%s\t%s", m.ShortID, label, createdDate))
	}

	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH; pass mission IDs as arguments instead")
	}

	fzfCmd := exec.Command(fzfBinary,
		"--multi",
		"--prompt", "Select missions to archive (TAB to multi-select): ",
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

func archiveMission(db *database.DB, missionID string) error {
	// Verify mission exists
	_, err := db.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	// Stop the wrapper if running (idempotent)
	if err := stopMissionWrapper(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to stop wrapper for mission '%s'", missionID)
	}

	if err := db.ArchiveMission(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to archive mission in database")
	}

	fmt.Printf("Archived mission: %s\n", database.ShortID(missionID))
	return nil
}
