package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/server"
)

var cloneFlag string
var promptFlag string
var blankFlag bool
var adjutantFlag bool
var noFocusFlag bool
var headlessFlag bool
var timeoutFlag string
var cronIDFlag string
var cronNameFlag string
var cronTriggerFlag string

var missionNewCmd = &cobra.Command{
	Use:   newCmdStr + " [repo]",
	Short: "Create a new mission and launch claude",
	Long: fmt.Sprintf(`Create a new mission and launch claude.

Without arguments, opens an interactive fzf picker showing your repo library.
With arguments, accepts a git reference (URL, shorthand like owner/repo, or
local path).

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
	missionNewCmd.Flags().BoolVar(&adjutantFlag, adjutantFlagName, false, "create an Adjutant mission")
	missionNewCmd.Flags().BoolVar(&noFocusFlag, noFocusFlagName, false, "don't focus the new mission's tmux window after creation")
	missionNewCmd.Flags().BoolVar(&headlessFlag, headlessFlagName, false, "run in headless mode (no terminal, outputs to log)")
	missionNewCmd.Flags().StringVar(&timeoutFlag, timeoutFlagName, "1h", "max runtime for headless missions (e.g., '1h', '30m')")
	missionNewCmd.Flags().StringVar(&cronIDFlag, cronIDFlagName, "", "cron job ID (internal use)")
	missionNewCmd.Flags().StringVar(&cronNameFlag, cronNameFlagName, "", "cron job name (internal use)")
	missionNewCmd.Flags().StringVar(&cronTriggerFlag, cronTriggerFlagName, "", "cron job name for double-fire prevention (internal use)")
	// Hide internal cron flags
	missionNewCmd.Flags().MarkHidden(cronIDFlagName)
	missionNewCmd.Flags().MarkHidden(cronNameFlagName)
	missionNewCmd.Flags().MarkHidden(cronTriggerFlagName)
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
	ensureServerRunning(agencDirpath)

	// Double-fire prevention: if this is a cron trigger, check for running missions
	if cronTriggerFlag != "" {
		if shouldSkipCronTrigger(cronTriggerFlag) {
			fmt.Printf("Skipping cron trigger '%s': previous mission still running\n", cronTriggerFlag)
			return nil
		}
		// Set cronNameFlag so the mission gets tagged with the cron name
		if cronNameFlag == "" {
			cronNameFlag = cronTriggerFlag
		}
	}

	// Ensure shadow repo is initialized (auto-creates from ~/.claude if needed)
	if err := claudeconfig.EnsureShadowRepo(agencDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to ensure shadow repo")
	}

	if cloneFlag != "" {
		return runMissionNewWithClone()
	}

	if adjutantFlag {
		return createAndLaunchAdjutantMission(agencDirpath, promptFlag)
	}

	if blankFlag {
		return createAndLaunchMission(agencDirpath, "", promptFlag)
	}

	return runMissionNewWithPicker(args)
}

// shouldSkipCronTrigger checks if a cron trigger should be skipped due to a
// running mission. Returns true if there is a recent mission for this cron
// that is still active (status != "completed").
func shouldSkipCronTrigger(cronName string) bool {
	client, err := serverClient()
	if err != nil {
		fmt.Printf("Warning: failed to connect to server: %v\n", err)
		return false
	}

	missions, err := client.ListMissions(true, cronName)
	if err != nil {
		fmt.Printf("Warning: failed to query for recent mission: %v\n", err)
		return false
	}

	if len(missions) == 0 {
		return false
	}

	mission := missions[0]
	return mission.Status != "completed" && mission.Status != "archived"
}

// runMissionNewWithClone creates a new mission by cloning the agent directory
// of an existing mission. The source mission's git_repo carries over to the
// new mission.
func runMissionNewWithClone() error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	sourceMission, err := client.GetMission(cloneFlag)
	if err != nil {
		return stacktrace.Propagate(err, "failed to get source mission")
	}

	tmuxSession := ""
	if !headlessFlag {
		tmuxSession = getCurrentTmuxSessionName()
	}

	missionRecord, err := client.CreateMission(server.CreateMissionRequest{
		Repo:        sourceMission.GitRepo,
		Prompt:      promptFlag,
		CloneFrom:   sourceMission.ID,
		TmuxSession: tmuxSession,
	})
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission")
	}

	fmt.Printf("Created mission: %s (cloned from %s)\n", missionRecord.ShortID, sourceMission.ShortID)
	fmt.Printf("Mission directory: %s\n", config.GetMissionDirpath(agencDirpath, missionRecord.ID))

	if tmuxSession != "" {
		fmt.Println("Launched in tmux pool")
		if !noFocusFlag {
			focusMissionWindow(missionRecord.ShortID, tmuxSession)
		}
	} else {
		fmt.Println("Running in background (pool window)")
	}

	return nil
}

// runMissionNewWithPicker shows an fzf picker over the repo library, or resolves
// a positional arg as a repo reference.
func runMissionNewWithPicker(args []string) error {
	entries := listRepoLibrary(agencDirpath)

	input := strings.Join(args, " ")

	// No args: show fzf picker with sentinel (Blank Mission option)
	if input == "" {
		picked, err := selectFromRepoLibrary(entries, "")
		if err != nil {
			return err
		}
		return launchFromLibrarySelection(picked)
	}

	// Try to resolve as a git reference (URL, path, shorthand)
	// looksLikeRepoReference will lazily fetch defaultGitHubUser only if needed
	if looksLikeRepoReference(agencDirpath, input) {
		result, err := ResolveRepoInput(agencDirpath, input, "Select repo: ")
		if err != nil {
			return err
		}
		return launchFromLibrarySelection(&repoLibraryEntry{RepoName: result.RepoName})
	}

	// Non-empty input that doesn't look like a repo reference is an error
	return stacktrace.NewError("not a valid repo reference: %s", input)
}

// adjutantSentinelRepoName is the sentinel value used in fzf picker entries
// to represent the "Adjutant" option.
const adjutantSentinelRepoName = ".adjutant-sentinel"

// cloneNewRepoSentinelRepoName is the sentinel value used in fzf picker entries
// to represent the "Github Repo" option.
const cloneNewRepoSentinelRepoName = "__clone_new__"

// launchFromLibrarySelection creates and launches a mission based on the
// library picker selection.
func launchFromLibrarySelection(selection *repoLibraryEntry) error {
	if selection.RepoName == adjutantSentinelRepoName {
		return createAndLaunchAdjutantMission(agencDirpath, promptFlag)
	}

	if selection.RepoName == cloneNewRepoSentinelRepoName {
		result, err := promptForRepoLocator(agencDirpath)
		if err != nil {
			return err
		}
		return createAndLaunchMission(agencDirpath, result.RepoName, promptFlag)
	}

	if selection.RepoName == "" {
		// NONE selected — Blank Mission
		return createAndLaunchMission(agencDirpath, "", promptFlag)
	}

	// Repo selected — clone into agent directory
	return createAndLaunchMission(agencDirpath, selection.RepoName, promptFlag)
}

// createAndLaunchAdjutantMission creates an Adjutant mission via the server,
// which spawns a wrapper in a tmux pool window.
func createAndLaunchAdjutantMission(agencDirpath string, initialPrompt string) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	tmuxSession := ""
	if !headlessFlag {
		tmuxSession = getCurrentTmuxSessionName()
	}

	missionRecord, err := client.CreateMission(server.CreateMissionRequest{
		Adjutant:    true,
		Prompt:      initialPrompt,
		TmuxSession: tmuxSession,
	})
	if err != nil {
		return stacktrace.Propagate(err, "failed to create adjutant mission")
	}

	fmt.Printf("Created Adjutant mission: %s\n", missionRecord.ShortID)

	if tmuxSession != "" {
		fmt.Println("Launched in tmux pool")
		if !noFocusFlag {
			focusMissionWindow(missionRecord.ShortID, tmuxSession)
		}
	} else {
		fmt.Println("Running in background (pool window)")
	}

	return nil
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

// selectFromRepoLibrary presents an fzf picker over the repo library entries.
// An "Adjutant" option is prepended as the first data row, followed by
// repo entries. A NONE sentinel (Blank Mission) is appended at the bottom.
func selectFromRepoLibrary(entries []repoLibraryEntry, initialQuery string) (*repoLibraryEntry, error) {
	// First two data rows are special options; repos follow at index offset 2
	var rows [][]string
	rows = append(rows, []string{"🤖", "Adjutant"})
	rows = append(rows, []string{"🐙", "Github Repo"})
	for _, entry := range entries {
		rows = append(rows, []string{"📦", displayGitRepo(entry.RepoName)})
	}

	// Use sentinel row for NONE option (Blank Mission)
	sentinelRow := []string{"😶", "Blank Mission"}

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

	idx := indices[0]
	if idx == 0 {
		// Adjutant row selected
		return &repoLibraryEntry{RepoName: adjutantSentinelRepoName}, nil
	}
	if idx == 1 {
		// Github Repo row selected
		return &repoLibraryEntry{RepoName: cloneNewRepoSentinelRepoName}, nil
	}

	// Adjust index for the two special rows (adjutant + clone new)
	return &entries[idx-2], nil
}

// createAndLaunchMission creates the mission record and directory via the
// server, which spawns a wrapper in a tmux pool window.
// gitRepoName is the canonical repo name stored in the DB (e.g.
// "github.com/owner/repo"); empty when no git repo is involved.
// initialPrompt is optional; if non-empty, it will be sent to Claude.
func createAndLaunchMission(
	agencDirpath string,
	gitRepoName string,
	initialPrompt string,
) error {
	client, err := serverClient()
	if err != nil {
		return err
	}

	// Detect tmux session — omit if headless flag is set
	tmuxSession := ""
	if !headlessFlag {
		tmuxSession = getCurrentTmuxSessionName()
	}

	missionRecord, err := client.CreateMission(server.CreateMissionRequest{
		Repo:        gitRepoName,
		Prompt:      initialPrompt,
		TmuxSession: tmuxSession,
		CronID:      cronIDFlag,
		CronName:    cronNameFlag,
		Timeout:     timeoutFlag,
	})
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission")
	}

	fmt.Printf("Created mission: %s\n", missionRecord.ShortID)

	if tmuxSession != "" {
		fmt.Println("Launched in tmux pool")
		if !noFocusFlag {
			focusMissionWindow(missionRecord.ShortID, tmuxSession)
		}
	} else {
		fmt.Println("Running in background (pool window)")
	}

	return nil
}

// promptForRepoLocator interactively prompts the user for a repo locator,
// printing the accepted formats and looping on invalid input. Returns the
// resolved repo result ready for mission creation.
func promptForRepoLocator(agencDirpath string) (*RepoResolutionResult, error) {
	fmt.Println()
	printRepoFormatHelp()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("\nRepo: ")
		input, err := reader.ReadString('\n')
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to read input")
		}
		input = strings.TrimSpace(input)

		if input == "" {
			fmt.Println("No repo provided. Please enter a repo reference or press Ctrl-C to cancel.")
			continue
		}

		if !looksLikeRepoReference(agencDirpath, input) {
			fmt.Println("Not a valid repo reference. Please try again.")
			continue
		}

		// Get default GitHub user now that we know we need to resolve a repo reference
		defaultGitHubUser := getDefaultGitHubUser()
		result, err := resolveAsRepoReference(agencDirpath, input, defaultGitHubUser)
		if err != nil {
			fmt.Printf("Invalid repo: %v\n", err)
			fmt.Println("Please try again.")
			continue
		}

		return result, nil
	}
}
