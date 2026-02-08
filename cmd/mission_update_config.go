package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/mission"
)

var updateConfigAllFlag bool

var missionUpdateConfigCmd = &cobra.Command{
	Use:   updateConfigCmdStr + " [mission-id|search-terms...]",
	Short: "Update a mission's Claude config from the config source repo",
	Long: fmt.Sprintf(`Update a mission's Claude config from the config source repo.

Fetches the latest config source, shows a diff of changes, and rebuilds
the per-mission Claude config directory.

Without arguments, opens an interactive fzf picker to select a mission.
With arguments, accepts a mission ID (short or full UUID) or search terms.

Use --%s to update all non-archived missions that have per-mission config.`, allFlagName),
	Args: cobra.ArbitraryArgs,
	RunE: runMissionUpdateConfig,
}

func init() {
	missionUpdateConfigCmd.Flags().BoolVar(&updateConfigAllFlag, allFlagName, false, "update all non-archived missions")
	missionCmd.AddCommand(missionUpdateConfigCmd)
}

func runMissionUpdateConfig(cmd *cobra.Command, args []string) error {
	if _, err := getAgencContext(); err != nil {
		return err
	}

	// Resolve config source
	configSourceDirpath := resolveConfigSourceDirpath()
	if configSourceDirpath == "" {
		return stacktrace.NewError(
			"no Claude config source repo registered; run '%s %s %s' to set one up",
			agencCmdStr, configCmdStr, initCmdStr,
		)
	}

	// Fetch latest config source
	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read agenc config")
	}

	configRepoDirpath := config.GetRepoDirpath(agencDirpath, cfg.ClaudeConfig.Repo)
	fmt.Printf("Updating config source repo '%s'...\n", cfg.ClaudeConfig.Repo)
	if err := mission.ForceUpdateRepo(configRepoDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to update config source repo")
	}

	newCommitHash := claudeconfig.ResolveConfigCommitHash(configSourceDirpath)

	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	if updateConfigAllFlag {
		return updateConfigForAllMissions(db, configSourceDirpath, newCommitHash)
	}

	// Single mission mode
	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No missions.")
		return nil
	}

	entries, err := buildMissionPickerEntries(db, missions)
	if err != nil {
		return err
	}

	result, err := Resolve(strings.Join(args, " "), Resolver[missionPickerEntry]{
		TryCanonical: func(input string) (missionPickerEntry, bool, error) {
			if !looksLikeMissionID(input) {
				return missionPickerEntry{}, false, nil
			}
			missionID, err := db.ResolveMissionID(input)
			if err != nil {
				return missionPickerEntry{}, false, stacktrace.Propagate(err, "failed to resolve mission ID")
			}
			for _, e := range entries {
				if e.MissionID == missionID {
					return e, true, nil
				}
			}
			return missionPickerEntry{}, false, stacktrace.NewError("mission %s not found", input)
		},
		GetItems:    func() ([]missionPickerEntry, error) { return entries, nil },
		ExtractText: formatMissionMatchLine,
		FormatRow: func(e missionPickerEntry) []string {
			return []string{e.LastActive, e.ShortID, e.Status, e.Session, e.Repo}
		},
		FzfPrompt:   "Select mission to update config: ",
		FzfHeaders:  []string{"LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO"},
		MultiSelect: false,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	selected := result.Items[0]

	input := strings.Join(args, " ")
	if input != "" && !looksLikeMissionID(input) {
		fmt.Printf("Auto-selected: %s\n", selected.ShortID)
	}

	return updateMissionConfig(db, selected.MissionID, configSourceDirpath, newCommitHash)
}

// updateConfigForAllMissions updates the Claude config for all non-archived
// missions that have a per-mission config directory.
func updateConfigForAllMissions(db *database.DB, configSourceDirpath string, newCommitHash string) error {
	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: false})
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	var updatedCount int
	for _, m := range missions {
		missionConfigDirpath := filepath.Join(
			config.GetMissionDirpath(agencDirpath, m.ID),
			claudeconfig.MissionClaudeConfigDirname,
		)
		if _, err := os.Stat(missionConfigDirpath); os.IsNotExist(err) {
			continue // Legacy mission without per-mission config
		}

		if err := updateMissionConfig(db, m.ID, configSourceDirpath, newCommitHash); err != nil {
			fmt.Printf("  Failed to update mission %s: %v\n", m.ShortID, err)
			continue
		}
		updatedCount++
	}

	fmt.Printf("\nUpdated %d mission(s).\n", updatedCount)
	return nil
}

// updateMissionConfig rebuilds a single mission's Claude config directory
// from the config source repo.
func updateMissionConfig(db *database.DB, missionID string, configSourceDirpath string, newCommitHash string) error {
	missionRecord, err := db.GetMission(missionID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get mission")
	}

	missionDirpath := config.GetMissionDirpath(agencDirpath, missionID)

	// Read current pinned commit
	currentCommitHash := readConfigCommit(missionDirpath)

	if currentCommitHash == newCommitHash && newCommitHash != "" {
		fmt.Printf("Mission %s: config already up to date (commit %s)\n",
			missionRecord.ShortID, newCommitHash[:12])
		return nil
	}

	// Show diff if we have both commits
	if currentCommitHash != "" && newCommitHash != "" {
		showConfigDiff(configSourceDirpath, currentCommitHash, newCommitHash)
	}

	fmt.Printf("Updating config for mission %s...\n", missionRecord.ShortID)

	// Rebuild per-mission config directory
	if err := claudeconfig.BuildMissionConfigDir(agencDirpath, missionID, configSourceDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to rebuild config for mission '%s'", missionID)
	}

	// Update DB config_commit
	if newCommitHash != "" {
		if err := db.UpdateMissionConfigCommit(missionID, newCommitHash); err != nil {
			return stacktrace.Propagate(err, "failed to update config_commit in database")
		}
	}

	fmt.Printf("Mission %s: config updated", missionRecord.ShortID)
	if newCommitHash != "" {
		fmt.Printf(" (commit %s)", newCommitHash[:12])
	}
	fmt.Println()

	// Warn if the mission is currently running
	if getMissionStatus(missionID, missionRecord.Status) == "RUNNING" {
		fmt.Printf("  Note: mission is running — restart it for changes to take effect\n")
	}

	return nil
}

// readConfigCommit reads the config-commit file from a mission directory.
// Returns empty string if the file doesn't exist.
func readConfigCommit(missionDirpath string) string {
	data, err := os.ReadFile(filepath.Join(missionDirpath, claudeconfig.ConfigCommitFilename))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// showConfigDiff displays a git diff between two commits in the config source
// repo, filtered to trackable config items.
func showConfigDiff(configSourceDirpath string, oldCommit string, newCommit string) {
	repoRootDirpath := claudeconfig.FindGitRoot(configSourceDirpath)
	if repoRootDirpath == "" {
		return
	}

	// Compute the relative path from repo root to config source subdir
	relPath, err := filepath.Rel(repoRootDirpath, configSourceDirpath)
	if err != nil || relPath == "." {
		relPath = ""
	}

	// Build path filters for trackable items
	var pathFilters []string
	for _, itemName := range claudeconfig.TrackableItemNames {
		if relPath != "" {
			pathFilters = append(pathFilters, filepath.Join(relPath, itemName))
		} else {
			pathFilters = append(pathFilters, itemName)
		}
	}

	diffArgs := []string{"diff", "--stat", oldCommit, newCommit, "--"}
	diffArgs = append(diffArgs, pathFilters...)

	diffCmd := exec.Command("git", diffArgs...)
	diffCmd.Dir = repoRootDirpath
	diffCmd.Stdout = os.Stdout
	diffCmd.Stderr = os.Stderr

	fmt.Printf("\nConfig changes (%s..%s):\n", oldCommit[:12], newCommit[:12])
	if err := diffCmd.Run(); err != nil {
		fmt.Println("  (could not generate diff)")
	}
	fmt.Println()
}
