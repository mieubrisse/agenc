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
var gitFlag string
var cloneFlag string
var promptFlag string

var missionNewCmd = &cobra.Command{
	Use:   newCmdStr + " [search-terms...]",
	Short: "Create a new mission and launch claude",
	Long: fmt.Sprintf(`Create a new mission and launch claude.

Without flags, opens an fzf picker showing all repos and agent templates
in your library ($AGENC_DIRPATH/repos/). Positional arguments act as search terms
to filter the list. If exactly one repo matches, it is auto-selected.

Use --%s and/or --%s flags for explicit control (cannot be combined
with positional search terms).

Use --%s <mission-uuid> to create a new mission with a full copy of an
existing mission's workspace. The source mission's git repo and agent template
carry over by default. Override the agent template with --%s or a single
positional search term. --%s cannot be combined with --%s.

The --%s flag accepts:
  - Full URLs (git@github.com:owner/repo.git or https://github.com/owner/repo)
  - Shorthand references (owner/repo or github.com/owner/repo)
  - Local filesystem paths (/path/to/repo, ./relative/path, ~/home/path)
  - Search terms to match against repos in your library ("my repo")`,
		gitFlagName, agentFlagName,
		cloneFlagName,
		agentFlagName,
		cloneFlagName, gitFlagName,
		gitFlagName),
	Args: cobra.ArbitraryArgs,
	RunE: runMissionNew,
}

func init() {
	missionNewCmd.Flags().StringVar(&agentFlag, agentFlagName, "", "agent template name (overrides defaultFor config)")
	missionNewCmd.Flags().StringVar(&gitFlag, gitFlagName, "", "git repo (URL, shorthand, local path, or search terms)")
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

	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	if cloneFlag != "" {
		if gitFlag != "" {
			return stacktrace.NewError("--%s and --%s are mutually exclusive", cloneFlagName, gitFlagName)
		}
		if agentFlag != "" && len(args) > 0 {
			return stacktrace.NewError("--%s with --%s cannot be combined with positional arguments", cloneFlagName, agentFlagName)
		}
		if len(args) > 1 {
			return stacktrace.NewError("--%s accepts at most one positional argument (agent template search term)", cloneFlagName)
		}
		return runMissionNewWithClone(cfg, args)
	}

	hasFlags := gitFlag != "" || agentFlag != ""
	hasArgs := len(args) > 0

	if hasFlags && hasArgs {
		return stacktrace.NewError("positional search terms cannot be combined with --%s or --%s flags", gitFlagName, agentFlagName)
	}

	if hasFlags {
		return runMissionNewWithFlags(cfg)
	}
	return runMissionNewWithPicker(cfg, args)
}

// runMissionNewWithFlags contains the original flag-based mission creation
// logic (--git and/or --agent).
func runMissionNewWithFlags(cfg *config.AgencConfig) error {
	var gitRepoName string
	var gitCloneDirpath string
	if gitFlag != "" {
		repoName, cloneDirpath, gitErr := resolveGitFlag(agencDirpath, gitFlag)
		if gitErr != nil {
			return gitErr
		}
		gitRepoName = repoName
		gitCloneDirpath = cloneDirpath
	}

	agentTemplate, err := resolveAgentTemplate(cfg, agentFlag, gitRepoName)
	if err != nil {
		return err
	}

	return createAndLaunchMission(agencDirpath, agentTemplate, gitRepoName, gitCloneDirpath, promptFlag)
}

// runMissionNewWithClone creates a new mission by cloning the workspace of an
// existing mission. The source mission's git_repo carries over to the new
// mission. The agent template can be overridden with --agent or a positional arg.
func runMissionNewWithClone(cfg *config.AgencConfig, args []string) error {
	return resolveAndRunForMission(cloneFlag, func(db *database.DB, sourceMissionID string) error {
		sourceMission, err := db.GetMission(sourceMissionID)
		if err != nil {
			return stacktrace.Propagate(err, "failed to get source mission")
		}

		agentTemplate, err := resolveCloneAgentTemplate(cfg, sourceMission, args)
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
// mission. If --agent is set, it resolves via resolveTemplate. If a positional
// arg is provided, it resolves via resolveTemplate. Otherwise the source
// mission's agent template is inherited.
func resolveCloneAgentTemplate(cfg *config.AgencConfig, sourceMission *database.Mission, args []string) (string, error) {
	if agentFlag != "" {
		resolved, err := resolveTemplate(cfg.AgentTemplates, agentFlag)
		if err != nil {
			return "", stacktrace.NewError("agent template '%s' not found", agentFlag)
		}
		return resolved, nil
	}
	if len(args) == 1 {
		resolved, err := resolveTemplate(cfg.AgentTemplates, args[0])
		if err != nil {
			return "", stacktrace.NewError("agent template '%s' not found", args[0])
		}
		return resolved, nil
	}
	return sourceMission.AgentTemplate, nil
}

// runMissionNewWithPicker shows an fzf picker over the repo library. Positional
// args are used as search terms to filter or auto-select.
func runMissionNewWithPicker(cfg *config.AgencConfig, args []string) error {
	entries := listRepoLibrary(agencDirpath, cfg.AgentTemplates)

	var selection *repoLibrarySelection

	if len(args) > 0 {
		matches := matchRepoLibraryEntries(entries, args)
		if len(matches) == 1 {
			entry := matches[0]
			fmt.Printf("Auto-selected: %s\n", displayGitRepo(entry.RepoName))
			selection = &repoLibrarySelection{
				RepoName:   entry.RepoName,
				IsTemplate: entry.IsTemplate,
			}
		} else {
			picked, err := selectFromRepoLibrary(entries, strings.Join(args, " "))
			if err != nil {
				return err
			}
			selection = picked
		}
	} else {
		picked, err := selectFromRepoLibrary(entries, "")
		if err != nil {
			return err
		}
		selection = picked
	}

	return launchFromLibrarySelection(cfg, selection)
}

// launchFromLibrarySelection creates and launches a mission based on the
// library picker selection.
func launchFromLibrarySelection(cfg *config.AgencConfig, selection *repoLibrarySelection) error {
	if selection.RepoName == "" {
		// NONE selected — blank mission with default agent
		agentTemplate, err := resolveAgentTemplate(cfg, "", "")
		if err != nil {
			return err
		}
		return createAndLaunchMission(agencDirpath, agentTemplate, "", "", promptFlag)
	}

	if selection.IsTemplate {
		// Template selected — use the template as agent, no git repo
		return createAndLaunchMission(agencDirpath, selection.RepoName, "", "", promptFlag)
	}

	// Regular repo selected — clone into workspace, use default repo agent
	agentTemplate, err := resolveAgentTemplate(cfg, "", selection.RepoName)
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

const ansiLightBlue = "\033[94m"
const ansiReset = "\033[0m"

// formatLibraryFzfLine formats a repo library entry for display in fzf.
// Agent templates are prefixed with 🤖; repos are prefixed with 📦.
// Uses displayGitRepo for consistent repo formatting across all commands.
// This is used by matchRepoLibraryEntries for pre-filtering before fzf.
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
		Headers:      []string{" ", "ITEM"},
		Rows:         rows,
		MultiSelect:  false,
		InitialQuery: initialQuery,
	}, sentinelRow)
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH; install fzf or use --%s/--%s flags", gitFlagName, agentFlagName)
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

// matchRepoLibraryEntries filters entries by sequential case-insensitive
// substring matching. Each arg must appear in order within the formatted
// fzf line.
func matchRepoLibraryEntries(entries []repoLibraryEntry, args []string) []repoLibraryEntry {
	var matches []repoLibraryEntry
	for _, entry := range entries {
		line := formatLibraryFzfLine(entry)
		if matchesSequentialSubstrings(line, args) {
			matches = append(matches, entry)
		}
	}
	return matches
}

// matchesSequentialSubstrings returns true if all substrings appear in text
// in order, case-insensitively.
func matchesSequentialSubstrings(text string, substrings []string) bool {
	lower := strings.ToLower(text)
	pos := 0
	for _, sub := range substrings {
		idx := strings.Index(lower[pos:], strings.ToLower(sub))
		if idx == -1 {
			return false
		}
		pos += idx + len(sub)
	}
	return true
}

// resolveAgentTemplate determines which agent template to use for a new
// mission. If agentFlag is set, it resolves via resolveTemplate. Otherwise
// the per-template defaultFor config is consulted based on the git context.
func resolveAgentTemplate(cfg *config.AgencConfig, agentFlag string, gitRepoName string) (string, error) {
	if agentFlag != "" {
		resolved, err := resolveTemplate(cfg.AgentTemplates, agentFlag)
		if err != nil {
			return "", stacktrace.NewError("agent template '%s' not found", agentFlag)
		}
		return resolved, nil
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
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
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

// resolveGitFlag resolves a --git flag value into a canonical repo
// name and the filesystem path to the agenc-owned clone. The flag can be a
// local filesystem path (starts with /, ., or ~), a repo reference
// ("owner/repo" or "github.com/owner/repo"), a GitHub URL
// (https://github.com/owner/repo/...), or search terms to match against
// repos in the library.
func resolveGitFlag(agencDirpath string, flag string) (repoName string, cloneDirpath string, err error) {
	result, err := ResolveRepoInput(agencDirpath, flag, false, "Select git repo: ")
	if err != nil {
		return "", "", err
	}
	return result.RepoName, result.CloneDirpath, nil
}


// selectWithFzf presents templates in fzf and returns the selected repo name.
// If allowNone is true, a "NONE" option is prepended. Returns empty string if
// NONE is selected or cancelled.
func selectWithFzf(templates map[string]config.AgentTemplateProperties, initialQuery string, allowNone bool) (string, error) {
	// Build sorted list of repo keys
	repoKeys := sortedRepoKeys(templates)

	// Build rows for the picker
	var rows [][]string
	for _, repo := range repoKeys {
		props := templates[repo]
		nickname := "--"
		if props.Nickname != "" {
			nickname = props.Nickname
		}
		rows = append(rows, []string{nickname, displayGitRepo(repo)})
	}

	var indices []int
	var err error

	if allowNone {
		// Use sentinel for NONE option
		sentinelRow := []string{"NONE", "--"}
		indices, err = runFzfPickerWithSentinel(FzfPickerConfig{
			Prompt:       "Select agent template: ",
			Headers:      []string{"AGENT", "REPO"},
			Rows:         rows,
			MultiSelect:  false,
			InitialQuery: initialQuery,
		}, sentinelRow)
	} else {
		indices, err = runFzfPicker(FzfPickerConfig{
			Prompt:       "Select agent template: ",
			Headers:      []string{"AGENT", "REPO"},
			Rows:         rows,
			MultiSelect:  false,
			InitialQuery: initialQuery,
		})
	}

	if err != nil {
		return "", stacktrace.Propagate(err, "'fzf' binary not found in PATH; install fzf or pass the template name as an argument")
	}
	if indices == nil {
		return "", nil
	}

	// Sentinel row returns index -1
	if len(indices) > 0 && indices[0] == -1 {
		return "", nil
	}

	if len(indices) == 0 {
		return "", nil
	}

	return repoKeys[indices[0]], nil
}

// resolveOrPickTemplate resolves a template from positional search-term
// arguments, falling back to an interactive fzf picker when no unique match
// is found or no arguments are provided. Multiple args are matched
// sequentially (each must appear in order within the formatted template
// line). If exactly one template matches, it is auto-selected.
func resolveOrPickTemplate(templates map[string]config.AgentTemplateProperties, args []string) (string, error) {
	if len(args) > 0 {
		matches := matchTemplateEntries(templates, args)
		if len(matches) == 1 {
			fmt.Printf("Auto-selected: %s\n", displayGitRepo(matches[0]))
			return matches[0], nil
		}
		selected, fzfErr := selectWithFzf(templates, strings.Join(args, " "), false)
		if fzfErr != nil {
			return "", stacktrace.Propagate(fzfErr, "failed to select agent template")
		}
		return selected, nil
	}
	selected, fzfErr := selectWithFzf(templates, "", false)
	if fzfErr != nil {
		return "", stacktrace.Propagate(fzfErr, "failed to select agent template")
	}
	return selected, nil
}

// matchTemplateEntries filters templates by sequential case-insensitive
// substring matching against formatted fzf display lines. Returns the
// canonical repo keys of all matching templates.
func matchTemplateEntries(templates map[string]config.AgentTemplateProperties, args []string) []string {
	var matches []string
	for _, repo := range sortedRepoKeys(templates) {
		line := formatTemplateFzfLine(repo, templates[repo])
		if matchesSequentialSubstrings(line, args) {
			matches = append(matches, repo)
		}
	}
	return matches
}

// resolveTemplate attempts to find exactly one template matching the given
// query. It tries exact match on repo key, then exact match on nickname, then
// single substring match on either field.
func resolveTemplate(templates map[string]config.AgentTemplateProperties, query string) (string, error) {
	// Exact match by repo key
	if _, ok := templates[query]; ok {
		return query, nil
	}
	// Exact match by nickname
	for repo, props := range templates {
		if props.Nickname == query {
			return repo, nil
		}
	}
	// Single substring match
	var matches []string
	for repo, props := range templates {
		if strings.Contains(repo, query) || strings.Contains(props.Nickname, query) {
			matches = append(matches, repo)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return "", stacktrace.NewError("no unique template match for '%s'", query)
}
