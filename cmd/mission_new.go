package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/mission"
	"github.com/odyssey/agenc/internal/wrapper"
)

var agentFlag string
var cloneFlag string
var promptFlag string

var missionNewCmd = &cobra.Command{
	Use:   newCmdStr + " [search-terms...]",
	Short: "Create a new mission and launch claude",
	Long: fmt.Sprintf(`Create a new mission and launch claude.

Positional arguments select a repo or agent template. They can be:
  - A git reference (URL, shorthand like owner/repo, or local path)
  - Search terms to match against your library ("my repo")

Without --%s, both repos and agent templates are shown. Selecting an agent
template creates a blank mission using that template. Selecting a repo clones
it into the workspace and uses the default agent template.

With --%s, only repos are shown. The flag value specifies the agent template
using the same format as positional args (git reference or search terms).

Use --%s <mission-uuid> to create a new mission with a full copy of an
existing mission's workspace. Override the agent template with --%s or a
positional search term.`,
		agentFlagName,
		agentFlagName,
		cloneFlagName,
		agentFlagName),
	Args: cobra.ArbitraryArgs,
	RunE: runMissionNew,
}

func init() {
	missionNewCmd.Flags().StringVar(&agentFlag, agentFlagName, "", "agent template (URL, shorthand, local path, or search terms)")
	missionNewCmd.Flags().StringVar(&cloneFlag, cloneFlagName, "", "mission UUID to clone workspace from")
	missionNewCmd.Flags().StringVar(&promptFlag, promptFlagName, "", "initial prompt to start Claude with")
	missionCmd.AddCommand(missionNewCmd)
}

// repoLibraryEntry represents a single repo or agent template discovered in
// the $AGENC_DIRPATH/repos/ directory tree.
type repoLibraryEntry struct {
	RepoName   string
	IsTemplate bool
	Nickname   string
}

// repoLibrarySelection holds the user's pick from the repo library fzf menu.
type repoLibrarySelection struct {
	RepoName   string // empty if NONE (blank mission)
	IsTemplate bool
}

func runMissionNew(cmd *cobra.Command, args []string) error {
	ensureDaemonRunning(agencDirpath)

	cfg, err := readConfig()
	if err != nil {
		return err
	}

	if cloneFlag != "" {
		if agentFlag != "" && len(args) > 0 {
			return stacktrace.NewError("--%s with --%s cannot be combined with positional arguments", cloneFlagName, agentFlagName)
		}
		if len(args) > 1 {
			return stacktrace.NewError("--%s accepts at most one positional argument (agent template search term)", cloneFlagName)
		}
		return runMissionNewWithClone(args)
	}

	return runMissionNewWithPicker(cfg, args)
}

// runMissionNewWithClone creates a new mission by cloning the workspace of an
// existing mission. The source mission's git_repo carries over to the new
// mission. The agent template can be overridden with --agent or a positional arg.
func runMissionNewWithClone(args []string) error {
	return resolveAndRunForMission(cloneFlag, func(db *database.DB, sourceMissionID string) error {
		sourceMission, err := db.GetMission(sourceMissionID)
		if err != nil {
			return stacktrace.Propagate(err, "failed to get source mission")
		}

		agentTemplate, err := resolveCloneAgentTemplate(sourceMission, args)
		if err != nil {
			return err
		}

		missionRecord, err := db.CreateMission(agentTemplate, sourceMission.GitRepo)
		if err != nil {
			return stacktrace.Propagate(err, "failed to create mission record")
		}

		fmt.Printf("Created mission: %s (cloned from %s)\n", missionRecord.ShortID, sourceMission.ShortID)

		// Create mission directory structure with agent template but no git copy
		// (workspace will be copied separately from the source mission)
		if _, err := mission.CreateMissionDir(agencDirpath, missionRecord.ID, agentTemplate, "", ""); err != nil {
			return stacktrace.Propagate(err, "failed to create mission directory")
		}

		// Copy the source mission's workspace into the new mission
		srcWorkspaceDirpath := config.GetMissionWorkspaceDirpath(agencDirpath, sourceMission.ID)
		dstWorkspaceDirpath := config.GetMissionWorkspaceDirpath(agencDirpath, missionRecord.ID)
		if err := mission.CopyWorkspace(srcWorkspaceDirpath, dstWorkspaceDirpath); err != nil {
			return stacktrace.Propagate(err, "failed to copy workspace from source mission")
		}

		fmt.Printf("Mission directory: %s\n", config.GetMissionDirpath(agencDirpath, missionRecord.ID))
		fmt.Println("Launching claude...")

		gitRepoName := sourceMission.GitRepo
		w := wrapper.NewWrapper(agencDirpath, missionRecord.ID, agentTemplate, gitRepoName, promptFlag, db)
		return w.Run(false)
	})
}

// resolveCloneAgentTemplate determines the agent template when cloning a
// mission. If --agent is set, it resolves via ResolveRepoInput with
// templateOnly=true. If a positional arg is provided, it does the same.
// Otherwise the source mission's agent template is inherited.
func resolveCloneAgentTemplate(sourceMission *database.Mission, args []string) (string, error) {
	if agentFlag != "" {
		result, err := ResolveRepoInput(agencDirpath, agentFlag, true, "Select agent template: ")
		if err != nil {
			return "", err
		}
		return result.RepoName, nil
	}
	if len(args) == 1 {
		result, err := ResolveRepoInput(agencDirpath, args[0], true, "Select agent template: ")
		if err != nil {
			return "", err
		}
		return result.RepoName, nil
	}
	return sourceMission.AgentTemplate, nil
}

// runMissionNewWithPicker shows an fzf picker over the repo library. Positional
// args are used as search terms to filter or auto-select.
//
// When agentFlag is set, only repos are shown (no templates), and the agent
// template is resolved separately from the flag value after repo selection.
func runMissionNewWithPicker(cfg *config.AgencConfig, args []string) error {
	entries := listRepoLibrary(agencDirpath, cfg.AgentTemplates)

	// When --agent is specified, filter to repos only (user will pick their agent)
	if agentFlag != "" {
		var reposOnly []repoLibraryEntry
		for _, e := range entries {
			if !e.IsTemplate {
				reposOnly = append(reposOnly, e)
			}
		}
		entries = reposOnly
	}

	input := strings.Join(args, " ")

	// No args: show fzf picker with sentinel (blank mission option)
	if input == "" {
		picked, err := selectFromRepoLibrary(entries, "")
		if err != nil {
			return err
		}
		return launchFromLibrarySelection(cfg, picked)
	}

	// Try to resolve as a git reference (URL, path, shorthand)
	if looksLikeRepoReference(input) {
		result, err := ResolveRepoInput(agencDirpath, input, false, "Select repo: ")
		if err != nil {
			return err
		}
		selection := &repoLibrarySelection{
			RepoName:   result.RepoName,
			IsTemplate: false,
		}
		return launchFromLibrarySelection(cfg, selection)
	}

	// Use generic resolver for search/auto-select
	// Sentinel is not included since we're searching, not browsing
	result, err := Resolve(input, Resolver[repoLibraryEntry]{
		TryCanonical: nil, // Already handled above
		GetItems:     func() ([]repoLibraryEntry, error) { return entries, nil },
		ExtractText:  formatLibraryFzfLine,
		FormatRow: func(e repoLibraryEntry) []string {
			var typeIcon, item string
			if e.IsTemplate {
				typeIcon = "🤖"
				if e.Nickname != "" {
					item = fmt.Sprintf("%s (%s)", e.Nickname, displayGitRepo(e.RepoName))
				} else {
					item = displayGitRepo(e.RepoName)
				}
			} else {
				typeIcon = "📦"
				item = displayGitRepo(e.RepoName)
			}
			return []string{typeIcon, item}
		},
		FzfPrompt:   "Select repo: ",
		FzfHeaders:  []string{"TYPE", "ITEM"},
		MultiSelect: false,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled {
		return stacktrace.NewError("fzf selection cancelled")
	}

	if len(result.Items) == 0 {
		return stacktrace.NewError("no matching repos or templates found")
	}

	entry := result.Items[0]

	// Print auto-select message if search matched exactly one
	fmt.Printf("Auto-selected: %s\n", displayGitRepo(entry.RepoName))

	selection := &repoLibrarySelection{
		RepoName:   entry.RepoName,
		IsTemplate: entry.IsTemplate,
	}
	return launchFromLibrarySelection(cfg, selection)
}

// launchFromLibrarySelection creates and launches a mission based on the
// library picker selection. If agentFlag is set, resolves the agent template
// from it; otherwise uses defaultFor config or the selected template.
func launchFromLibrarySelection(cfg *config.AgencConfig, selection *repoLibrarySelection) error {
	if selection.RepoName == "" {
		// NONE selected — blank mission
		agentTemplate, err := resolveAgentTemplate(cfg, agentFlag, "")
		if err != nil {
			return err
		}
		return createAndLaunchMission(agencDirpath, agentTemplate, "", "", promptFlag)
	}

	if selection.IsTemplate {
		// Template selected — use the template as agent, no git repo
		// (this path only happens when agentFlag is not set, since templates
		// are filtered out when agentFlag is specified)
		return createAndLaunchMission(agencDirpath, selection.RepoName, "", "", promptFlag)
	}

	// Regular repo selected — clone into workspace
	// If agentFlag is set, resolve it; otherwise use defaultFor config
	agentTemplate, err := resolveAgentTemplate(cfg, agentFlag, selection.RepoName)
	if err != nil {
		return err
	}
	gitCloneDirpath := config.GetRepoDirpath(agencDirpath, selection.RepoName)
	return createAndLaunchMission(agencDirpath, agentTemplate, selection.RepoName, gitCloneDirpath, promptFlag)
}

// listRepoLibrary scans $AGENC_DIRPATH/repos/ three levels deep (github.com/owner/repo)
// and cross-references with agentTemplates config. Returns two types of entries:
// 1. Repo entries (IsTemplate=false) for every repo found on disk
// 2. Template entries (IsTemplate=true) for every template in the config
// A repo that is also registered as a template will appear twice: once as a
// repo row and once as a template row. Results are sorted with templates first,
// then repos, alphabetical within each group.
func listRepoLibrary(agencDirpath string, templates map[string]config.AgentTemplateProperties) []repoLibraryEntry {
	reposDirpath := config.GetReposDirpath(agencDirpath)

	var entries []repoLibraryEntry

	// Walk three levels: host/owner/repo
	// Add ALL repos as non-template entries
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
				// Add as a repo entry (not a template)
				entries = append(entries, repoLibraryEntry{
					RepoName:   repoName,
					IsTemplate: false,
				})
			}
		}
	}

	// Add ALL templates as template entries
	for repoName, props := range templates {
		entries = append(entries, repoLibraryEntry{
			RepoName:   repoName,
			IsTemplate: true,
			Nickname:   props.Nickname,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsTemplate != entries[j].IsTemplate {
			return entries[i].IsTemplate
		}
		return entries[i].RepoName < entries[j].RepoName
	})

	return entries
}

// formatLibraryFzfLine formats a repo library entry for display in fzf.
// Agent templates are prefixed with 🤖; repos are prefixed with 📦.
// Uses displayGitRepo for consistent repo formatting across all commands.
func formatLibraryFzfLine(entry repoLibraryEntry) string {
	coloredRepo := displayGitRepo(entry.RepoName)
	if entry.IsTemplate {
		if entry.Nickname != "" {
			return fmt.Sprintf("🤖 %s (%s)", entry.Nickname, coloredRepo)
		}
		return fmt.Sprintf("🤖 %s", coloredRepo)
	}
	return fmt.Sprintf("📦 %s", coloredRepo)
}

// selectFromRepoLibrary presents an fzf picker over the repo library entries.
// A NONE option is prepended for creating a blank mission.
func selectFromRepoLibrary(entries []repoLibraryEntry, initialQuery string) (*repoLibrarySelection, error) {
	// Build rows for the picker with two columns:
	// Column 1: 🤖 for templates, 📦 for repos
	// Column 2 (ITEM): "Nickname (repo)" for templates, repo path for repos
	var rows [][]string
	for _, entry := range entries {
		var typeIcon, item string
		if entry.IsTemplate {
			typeIcon = "🤖"
			if entry.Nickname != "" {
				item = fmt.Sprintf("%s (%s)", entry.Nickname, displayGitRepo(entry.RepoName))
			} else {
				item = displayGitRepo(entry.RepoName)
			}
		} else {
			typeIcon = "📦"
			item = displayGitRepo(entry.RepoName)
		}
		rows = append(rows, []string{typeIcon, item})
	}

	// Use sentinel row for NONE option
	sentinelRow := []string{"😶", "— blank mission"}

	indices, err := runFzfPickerWithSentinel(FzfPickerConfig{
		Prompt:       "Select repo: ",
		Headers:      []string{"TYPE", "ITEM"},
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
		return &repoLibrarySelection{}, nil
	}

	if len(indices) == 0 {
		return nil, stacktrace.NewError("no selection made")
	}

	entry := entries[indices[0]]
	return &repoLibrarySelection{
		RepoName:   entry.RepoName,
		IsTemplate: entry.IsTemplate,
	}, nil
}

// resolveAgentTemplate determines which agent template to use for a new
// mission. If agentFlag is set, it resolves via ResolveRepoInput with
// templateOnly=true, which tries repo reference resolution first, then
// glob matching against configured templates, then falls back to fzf.
// Otherwise the per-template defaultFor config is consulted based on git context.
func resolveAgentTemplate(cfg *config.AgencConfig, agentFlag string, gitRepoName string) (string, error) {
	if agentFlag != "" {
		result, err := ResolveRepoInput(agencDirpath, agentFlag, true, "Select agent template: ")
		if err != nil {
			return "", err
		}
		return result.RepoName, nil
	}

	// Pick the defaultFor context based on git context
	var defaultForContext string
	switch {
	case gitRepoName == "":
		defaultForContext = "emptyMission"
	case isAgentTemplate(cfg, gitRepoName):
		defaultForContext = "agentTemplate"
	default:
		defaultForContext = "repo"
	}

	defaultRepo := config.FindDefaultTemplate(cfg.AgentTemplates, defaultForContext)
	if defaultRepo == "" {
		return "", nil
	}

	// Verify the default agent template is actually installed
	if _, ok := cfg.AgentTemplates[defaultRepo]; !ok {
		fmt.Fprintf(os.Stderr, "Warning: defaultFor references '%s' which is not installed; proceeding without agent template\n", defaultRepo)
		return "", nil
	}

	return defaultRepo, nil
}

// isAgentTemplate returns true if the given repo name is a key in the
// agentTemplates config map.
func isAgentTemplate(cfg *config.AgencConfig, repoName string) bool {
	_, ok := cfg.AgentTemplates[repoName]
	return ok
}

// createAndLaunchMission creates the mission record and directory, and
// launches the wrapper process. gitRepoName is the canonical repo name
// stored in the DB (e.g. "github.com/owner/repo"); gitCloneDirpath is
// the filesystem path to the agenc-owned clone used for git operations. Both
// are empty when no git repo is involved. initialPrompt is optional; if
// non-empty, it will be sent to Claude when starting the conversation.
func createAndLaunchMission(
	agencDirpath string,
	agentTemplate string,
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

	missionRecord, err := db.CreateMission(agentTemplate, gitRepoName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission record")
	}

	fmt.Printf("Created mission: %s\n", missionRecord.ShortID)

	// Create mission directory structure (repo goes inside workspace/)
	missionDirpath, err := mission.CreateMissionDir(agencDirpath, missionRecord.ID, agentTemplate, gitRepoName, gitCloneDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission directory")
	}

	fmt.Printf("Mission directory: %s\n", missionDirpath)
	fmt.Println("Launching claude...")

	w := wrapper.NewWrapper(agencDirpath, missionRecord.ID, agentTemplate, gitRepoName, initialPrompt, db)
	return w.Run(false)
}



