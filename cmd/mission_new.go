package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/mission"
	"github.com/odyssey/agenc/internal/wrapper"
)

var cloneFlag string
var promptFlag string
var blankFlag bool
var headlessFlag bool
var timeoutFlag string
var cronIDFlag string
var cronNameFlag string

var missionNewCmd = &cobra.Command{
	Use:   newCmdStr + " [search-terms...]",
	Short: "Create a new mission and launch claude",
	Long: fmt.Sprintf(`Create a new mission and launch claude.

Positional arguments select a repo. They can be:
  - A git reference (URL, shorthand like owner/repo, or local path)
  - Search terms to match against your library ("my repo")

Use --%s <mission-uuid> to create a new mission with a full copy of an
existing mission's agent directory.`,
		cloneFlagName),
	Args: cobra.ArbitraryArgs,
	RunE: runMissionNew,
}

func init() {
	missionNewCmd.Flags().StringVar(&cloneFlag, cloneFlagName, "", "mission UUID to clone agent directory from")
	missionNewCmd.Flags().StringVar(&promptFlag, promptFlagName, "", "initial prompt to start Claude with")
	missionNewCmd.Flags().BoolVar(&blankFlag, blankFlagName, false, "create a blank mission with no repo (skip picker)")
	missionNewCmd.Flags().BoolVar(&headlessFlag, headlessFlagName, false, "run in headless mode (no terminal, outputs to log)")
	missionNewCmd.Flags().StringVar(&timeoutFlag, timeoutFlagName, "1h", "max runtime for headless missions (e.g., '1h', '30m')")
	missionNewCmd.Flags().StringVar(&cronIDFlag, cronIDFlagName, "", "cron job ID (internal use)")
	missionNewCmd.Flags().StringVar(&cronNameFlag, cronNameFlagName, "", "cron job name (internal use)")
	// Hide internal cron flags
	missionNewCmd.Flags().MarkHidden(cronIDFlagName)
	missionNewCmd.Flags().MarkHidden(cronNameFlagName)
	missionCmd.AddCommand(missionNewCmd)
}

// repoLibraryEntry represents a single repo discovered in the
// $AGENC_DIRPATH/repos/ directory tree.
type repoLibraryEntry struct {
	RepoName string
}

func runMissionNew(cmd *cobra.Command, args []string) error {
	if _, err := getAgencContext(); err != nil {
		return err
	}
	ensureDaemonRunning(agencDirpath)

	// Ensure shadow repo is initialized (auto-creates from ~/.claude if needed)
	if err := claudeconfig.EnsureShadowRepo(agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to ensure shadow repo")
	}

	if cloneFlag != "" {
		return runMissionNewWithClone()
	}

	if blankFlag {
		return createAndLaunchMission(agencDirpath, "", "", promptFlag)
	}

	return runMissionNewWithPicker(args)
}

// runMissionNewWithClone creates a new mission by cloning the agent directory
// of an existing mission. The source mission's git_repo carries over to the
// new mission.
func runMissionNewWithClone() error {
	return resolveAndRunForMission(cloneFlag, func(db *database.DB, sourceMissionID string) error {
		sourceMission, err := db.GetMission(sourceMissionID)
		if err != nil {
			return stacktrace.Propagate(err, "failed to get source mission")
		}

		createParams := &database.CreateMissionParams{}
		if commitHash := claudeconfig.GetShadowRepoCommitHash(agencDirpath); commitHash != "" {
			createParams.ConfigCommit = &commitHash
		}

		missionRecord, err := db.CreateMission(sourceMission.GitRepo, createParams)
		if err != nil {
			return stacktrace.Propagate(err, "failed to create mission record")
		}

		fmt.Printf("Created mission: %s (cloned from %s)\n", missionRecord.ShortID, sourceMission.ShortID)

		// Create mission directory structure with no git copy
		// (agent directory will be copied separately from the source mission)
		if _, err := mission.CreateMissionDir(agencDirpath, missionRecord.ID, "", ""); err != nil {
			return stacktrace.Propagate(err, "failed to create mission directory")
		}

		// Copy the source mission's agent directory into the new mission
		srcAgentDirpath := config.GetMissionAgentDirpath(agencDirpath, sourceMission.ID)
		dstAgentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionRecord.ID)
		if err := mission.CopyAgentDir(srcAgentDirpath, dstAgentDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to copy agent directory from source mission")
		}

		fmt.Printf("Mission directory: %s\n", config.GetMissionDirpath(agencDirpath, missionRecord.ID))
		fmt.Println("Launching claude...")

		gitRepoName := sourceMission.GitRepo
		w := wrapper.NewWrapper(agencDirpath, missionRecord.ID, gitRepoName, promptFlag, db)
		return w.Run(false)
	})
}

// runMissionNewWithPicker shows an fzf picker over the repo library. Positional
// args are used as search terms to filter or auto-select.
func runMissionNewWithPicker(args []string) error {
	entries := listRepoLibrary(agencDirpath)

	input := strings.Join(args, " ")

	// No args: show fzf picker with sentinel (blank mission option)
	if input == "" {
		picked, err := selectFromRepoLibrary(entries, "")
		if err != nil {
			return err
		}
		return launchFromLibrarySelection(picked)
	}

	// Try to resolve as a git reference (URL, path, shorthand)
	if looksLikeRepoReference(input) {
		result, err := ResolveRepoInput(agencDirpath, input, "Select repo: ")
		if err != nil {
			return err
		}
		return launchFromLibrarySelection(&repoLibraryEntry{RepoName: result.RepoName})
	}

	// Use generic resolver for search/auto-select
	// Sentinel is not included since we're searching, not browsing
	result, err := Resolve(input, Resolver[repoLibraryEntry]{
		TryCanonical: nil, // Already handled above
		GetItems:     func() ([]repoLibraryEntry, error) { return entries, nil },
		ExtractText:  formatLibraryFzfLine,
		FormatRow: func(e repoLibraryEntry) []string {
			return []string{"📦", displayGitRepo(e.RepoName)}
		},
		FzfPrompt:   "Select repo: ",
		FzfHeaders:  []string{"TYPE", "REPO"},
		MultiSelect: false,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled {
		return stacktrace.NewError("fzf selection cancelled")
	}

	if len(result.Items) == 0 {
		return stacktrace.NewError("no matching repos found")
	}

	entry := result.Items[0]

	// Print auto-select message if search matched exactly one
	fmt.Printf("Auto-selected: %s\n", displayGitRepo(entry.RepoName))

	return launchFromLibrarySelection(&entry)
}

// launchFromLibrarySelection creates and launches a mission based on the
// library picker selection.
func launchFromLibrarySelection(selection *repoLibraryEntry) error {
	if selection.RepoName == "" {
		// NONE selected — blank mission
		return createAndLaunchMission(agencDirpath, "", "", promptFlag)
	}

	// Repo selected — clone into agent directory
	gitCloneDirpath := config.GetRepoDirpath(agencDirpath, selection.RepoName)
	return createAndLaunchMission(agencDirpath, selection.RepoName, gitCloneDirpath, promptFlag)
}

// listRepoLibrary scans $AGENC_DIRPATH/repos/ three levels deep
// (github.com/owner/repo) and returns an entry for every repo found on disk.
// Results are sorted alphabetically.
func listRepoLibrary(agencDirpath string) []repoLibraryEntry {
	reposDirpath := config.GetReposDirpath(agencDirpath)

	var entries []repoLibraryEntry

	// Walk three levels: host/owner/repo
	hosts, _ := os.ReadDir(reposDirpath)
	for _, host := range hosts {
		if !host.IsDir() {
			continue
		}
		owners, _ := os.ReadDir(filepath.Join(reposDirpath, host.Name()))
		for _, owner := range owners {
			if !owner.IsDir() {
				continue
			}
			repos, _ := os.ReadDir(filepath.Join(reposDirpath, host.Name(), owner.Name()))
			for _, repo := range repos {
				if !repo.IsDir() {
					continue
				}
				repoName := host.Name() + "/" + owner.Name() + "/" + repo.Name()
				entries = append(entries, repoLibraryEntry{
					RepoName: repoName,
				})
			}
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].RepoName < entries[j].RepoName
	})

	return entries
}

// formatLibraryFzfLine formats a repo library entry for display in fzf.
// Uses displayGitRepo for consistent repo formatting across all commands.
func formatLibraryFzfLine(entry repoLibraryEntry) string {
	return fmt.Sprintf("📦 %s", displayGitRepo(entry.RepoName))
}

// selectFromRepoLibrary presents an fzf picker over the repo library entries.
// A NONE option is prepended for creating a blank mission.
func selectFromRepoLibrary(entries []repoLibraryEntry, initialQuery string) (*repoLibraryEntry, error) {
	var rows [][]string
	for _, entry := range entries {
		rows = append(rows, []string{"📦", displayGitRepo(entry.RepoName)})
	}

	// Use sentinel row for NONE option
	sentinelRow := []string{"😶", "— blank mission"}

	indices, err := runFzfPickerWithSentinel(FzfPickerConfig{
		Prompt:       "Select repo: ",
		Headers:      []string{"TYPE", "REPO"},
		Rows:         rows,
		MultiSelect:  false,
		InitialQuery: initialQuery,
	}, sentinelRow)
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH; install fzf or pass a repo reference as an argument")
	}
	if indices == nil {
		return nil, stacktrace.NewError("fzf selection cancelled")
	}

	// Sentinel row returns index -1
	if len(indices) > 0 && indices[0] == -1 {
		return &repoLibraryEntry{}, nil
	}

	if len(indices) == 0 {
		return nil, stacktrace.NewError("no selection made")
	}

	return &entries[indices[0]], nil
}

// createAndLaunchMission creates the mission record and directory, and
// launches the wrapper process. gitRepoName is the canonical repo name
// stored in the DB (e.g. "github.com/owner/repo"); gitCloneDirpath is
// the filesystem path to the agenc-owned clone used for git operations. Both
// are empty when no git repo is involved. initialPrompt is optional; if
// non-empty, it will be sent to Claude when starting the conversation.
func createAndLaunchMission(
	agencDirpath string,
	gitRepoName string,
	gitCloneDirpath string,
	initialPrompt string,
) error {
	// Open database and create mission record
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Build creation params (cron + config commit)
	createParams := &database.CreateMissionParams{}
	if cronIDFlag != "" {
		createParams.CronID = &cronIDFlag
	}
	if cronNameFlag != "" {
		createParams.CronName = &cronNameFlag
	}
	if commitHash := claudeconfig.GetShadowRepoCommitHash(agencDirpath); commitHash != "" {
		createParams.ConfigCommit = &commitHash
	}

	missionRecord, err := db.CreateMission(gitRepoName, createParams)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission record")
	}

	fmt.Printf("Created mission: %s\n", missionRecord.ShortID)

	// Create mission directory structure (repo is copied directly as agent/)
	missionDirpath, err := mission.CreateMissionDir(agencDirpath, missionRecord.ID, gitRepoName, gitCloneDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission directory")
	}

	fmt.Printf("Mission directory: %s\n", missionDirpath)

	w := wrapper.NewWrapper(agencDirpath, missionRecord.ID, gitRepoName, initialPrompt, db)

	// Run in headless mode if requested
	if headlessFlag {
		if initialPrompt == "" {
			return stacktrace.NewError("headless mode requires a prompt (use --%s)", promptFlagName)
		}

		timeout, err := time.ParseDuration(timeoutFlag)
		if err != nil {
			return stacktrace.Propagate(err, "invalid timeout '%s'", timeoutFlag)
		}

		fmt.Printf("Running in headless mode (timeout: %s)...\n", timeout)
		return w.RunHeadless(false, wrapper.HeadlessConfig{
			Timeout:  timeout,
			CronID:   cronIDFlag,
			CronName: cronNameFlag,
		})
	}

	fmt.Println("Launching claude...")
	return w.Run(false)
}

