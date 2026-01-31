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
	"github.com/odyssey/agenc/internal/mission"
)

var missionRmCmd = &cobra.Command{
	Use:   "rm [mission-id...]",
	Short: "Stop and permanently remove one or more missions",
	Args:  cobra.ArbitraryArgs,
	RunE:  runMissionRm,
}

func init() {
	missionCmd.AddCommand(missionRmCmd)
}

func runMissionRm(cmd *cobra.Command, args []string) error {
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	missionIDs := args
	if len(missionIDs) == 0 {
		selectedIDs, err := selectMissionsToRemove(db)
		if err != nil {
			return err
		}
		if len(selectedIDs) == 0 {
			return nil
		}
		missionIDs = selectedIDs
	}

	for _, missionID := range missionIDs {
		if err := removeMission(db, missionID); err != nil {
			return err
		}
	}

	return nil
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

	missionIDs := make([]string, len(missions))
	for i, m := range missions {
		missionIDs[i] = m.ID
	}

	descriptions, err := db.GetDescriptionsForMissions(missionIDs)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to fetch mission descriptions")
	}

	var fzfLines []string
	for _, m := range missions {
		label := descriptions[m.ID]
		if label == "" {
			label = m.Prompt
			if len(label) > 60 {
				label = label[:57] + "..."
			}
		}
		statusTag := ""
		if m.Status == "archived" {
			statusTag = " [archived]"
		}
		createdDate := m.CreatedAt.Format("2006-01-02 15:04")
		fzfLines = append(fzfLines, fmt.Sprintf("%s\t%s%s\t%s", m.ID, label, statusTag, createdDate))
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
	// Fetch mission record (needed for worktree cleanup)
	missionRecord, err := db.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	// Stop the wrapper if running (idempotent)
	if err := stopMissionWrapper(missionID); err != nil {
		return stacktrace.Propagate(err, "failed to stop wrapper for mission '%s'", missionID)
	}

	// Clean up worktree and branch before removing the directory
	if missionRecord.WorktreeSource != "" {
		workspaceDirpath := config.GetMissionWorkspaceDirpath(agencDirpath, missionID)
		branchName := mission.GetWorktreeBranchName(missionID)

		// Remove the worktree (best-effort)
		if err := mission.RemoveWorktree(missionRecord.WorktreeSource, workspaceDirpath); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to clean up git worktree: %v\n", err)
		}

		// Delete the branch if fully merged into main; preserve if unmerged
		deleted, err := mission.DeleteWorktreeBranchIfMerged(missionRecord.WorktreeSource, branchName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to check worktree branch: %v\n", err)
		} else if !deleted {
			fmt.Printf("Branch '%s' preserved in %s (has unmerged changes)\n", branchName, missionRecord.WorktreeSource)
		}
	}

	// Remove the mission directory
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

	fmt.Printf("Removed mission: %s\n", missionID)
	return nil
}
