package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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

var missionNewCmd = &cobra.Command{
	Use:   newCmdStr + " [search-terms...]",
	Short: "Create a new mission and launch claude",
	Long: `Create a new mission and launch claude.

Without flags, opens an fzf picker showing all repos and agent templates
in your library (~/.agenc/repos/). Positional arguments act as search terms
to filter the list. If exactly one repo matches, it is auto-selected.

Use --git and/or --agent flags for explicit control (cannot be combined
with positional search terms).

Use --clone <mission-uuid> to create a new mission with a full copy of an
existing mission's workspace. The source mission's git repo and agent template
carry over by default. Override the agent template with --agent or a single
positional search term. --clone cannot be combined with --git.`,
	Args: cobra.ArbitraryArgs,
	RunE: runMissionNew,
}

func init() {
	missionNewCmd.Flags().StringVar(&agentFlag, "agent", "", "agent template name (overrides defaultFor config)")
	missionNewCmd.Flags().StringVar(&gitFlag, "git", "", "git repo to copy into workspace (local path, owner/repo, or https://github.com/owner/repo/...)")
	missionNewCmd.Flags().StringVar(&cloneFlag, "clone", "", "mission UUID to clone workspace from")
	missionCmd.AddCommand(missionNewCmd)
}

// repoLibraryEntry represents a single repo or agent template discovered in
// the ~/.agenc/repos/ directory tree.
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
			return stacktrace.NewError("--clone and --git are mutually exclusive")
		}
		if agentFlag != "" && len(args) > 0 {
			return stacktrace.NewError("--clone with --agent cannot be combined with positional arguments")
		}
		if len(args) > 1 {
			return stacktrace.NewError("--clone accepts at most one positional argument (agent template search term)")
		}
		return runMissionNewWithClone(cfg, args)
	}

	hasFlags := gitFlag != "" || agentFlag != ""
	hasArgs := len(args) > 0

	if hasFlags && hasArgs {
		return stacktrace.NewError("positional search terms cannot be combined with --git or --agent flags")
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

	return createAndLaunchMission(agencDirpath, agentTemplate, gitRepoName, gitCloneDirpath)
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
		w := wrapper.NewWrapper(agencDirpath, missionRecord.ID, agentTemplate, gitRepoName, db)
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
		return createAndLaunchMission(agencDirpath, agentTemplate, "", "")
	}

	if selection.IsTemplate {
		// Template selected — use the template as agent, no git repo
		return createAndLaunchMission(agencDirpath, selection.RepoName, "", "")
	}

	// Regular repo selected — clone into workspace, use default repo agent
	agentTemplate, err := resolveAgentTemplate(cfg, "", selection.RepoName)
	if err != nil {
		return err
	}
	gitCloneDirpath := config.GetRepoDirpath(agencDirpath, selection.RepoName)
	return createAndLaunchMission(agencDirpath, agentTemplate, selection.RepoName, gitCloneDirpath)
}

// listRepoLibrary scans ~/.agenc/repos/ three levels deep (github.com/owner/repo)
// and cross-references with agentTemplates config. Returns entries sorted with
// templates first, then repos, alphabetical within each group.
func listRepoLibrary(agencDirpath string, templates map[string]config.AgentTemplateProperties) []repoLibraryEntry {
	reposDirpath := config.GetReposDirpath(agencDirpath)

	var entries []repoLibraryEntry
	seen := make(map[string]bool)

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
				seen[repoName] = true

				props, isTemplate := templates[repoName]
				entry := repoLibraryEntry{
					RepoName:   repoName,
					IsTemplate: isTemplate,
				}
				if isTemplate {
					entry.Nickname = props.Nickname
				}
				entries = append(entries, entry)
			}
		}
	}

	// Include any templates that aren't physically present in repos/
	for repoName, props := range templates {
		if !seen[repoName] {
			entries = append(entries, repoLibraryEntry{
				RepoName:   repoName,
				IsTemplate: true,
				Nickname:   props.Nickname,
			})
		}
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsTemplate != entries[j].IsTemplate {
			return entries[i].IsTemplate
		}
		return entries[i].RepoName < entries[j].RepoName
	})

	return entries
}

var ansiEscapePattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

const ansiLightBlue = "\033[94m"
const ansiReset = "\033[0m"

// formatLibraryFzfLine formats a repo library entry for display in fzf.
// Agent templates are prefixed with 🤖; regular repos have no prefix.
// Uses displayGitRepo for consistent repo formatting across all commands.
func formatLibraryFzfLine(entry repoLibraryEntry) string {
	coloredRepo := displayGitRepo(entry.RepoName)
	if entry.IsTemplate {
		if entry.Nickname != "" {
			return fmt.Sprintf("🤖 %s  (%s)", entry.Nickname, coloredRepo)
		}
		return fmt.Sprintf("🤖 %s", coloredRepo)
	}
	return fmt.Sprintf("   %s", coloredRepo)
}

// stripAnsi removes ANSI escape sequences from a string.
func stripAnsi(s string) string {
	return ansiEscapePattern.ReplaceAllString(s, "")
}

// reconstructCanonicalRepoName converts a display-formatted repo name back to
// canonical form. GitHub repos are displayed as "owner/repo" (1 slash) and need
// "github.com/" prepended. Non-GitHub repos keep their full URL (2+ slashes).
func reconstructCanonicalRepoName(displayName string) string {
	if strings.Count(displayName, "/") == 1 {
		return "github.com/" + displayName
	}
	return displayName
}

// parseLibraryFzfLine extracts the repo name and template status from a
// formatted fzf line produced by formatLibraryFzfLine. The returned repo name
// is in canonical form (e.g. "github.com/owner/repo").
func parseLibraryFzfLine(line string) (repoName string, isTemplate bool) {
	line = strings.TrimSpace(stripAnsi(line))

	if strings.HasPrefix(line, "🤖") {
		isTemplate = true
		rest := strings.TrimSpace(strings.TrimPrefix(line, "🤖"))
		// Check for nickname format: "nickname  (owner/repo)"
		if idx := strings.LastIndex(rest, "  ("); idx != -1 {
			repoName = strings.TrimSuffix(rest[idx+3:], ")")
			return reconstructCanonicalRepoName(repoName), isTemplate
		}
		return reconstructCanonicalRepoName(rest), isTemplate
	}

	// Non-template repos have no emoji prefix, just whitespace
	return reconstructCanonicalRepoName(strings.TrimSpace(line)), false
}

// selectFromRepoLibrary presents an fzf picker over the repo library entries.
// A NONE option is prepended for creating a blank mission.
func selectFromRepoLibrary(entries []repoLibraryEntry, initialQuery string) (*repoLibrarySelection, error) {
	var lines []string
	lines = append(lines, "NONE — blank mission")
	for _, entry := range entries {
		lines = append(lines, formatLibraryFzfLine(entry))
	}
	input := strings.Join(lines, "\n")

	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH; install fzf or use --git/--agent flags")
	}

	fzfArgs := []string{"--ansi", "--prompt", "Select repo: "}
	if initialQuery != "" {
		fzfArgs = append(fzfArgs, "--query", initialQuery)
	}

	fzfCmd := exec.Command(fzfBinary, fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(input)
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		return nil, stacktrace.Propagate(err, "fzf selection failed")
	}

	selected := strings.TrimSpace(string(output))
	if strings.HasPrefix(selected, "NONE") {
		return &repoLibrarySelection{}, nil
	}

	repoName, isTemplate := parseLibraryFzfLine(selected)
	return &repoLibrarySelection{
		RepoName:   repoName,
		IsTemplate: isTemplate,
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
// are empty when no git repo is involved.
func createAndLaunchMission(
	agencDirpath string,
	agentTemplate string,
	gitRepoName string,
	gitCloneDirpath string,
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

	w := wrapper.NewWrapper(agencDirpath, missionRecord.ID, agentTemplate, gitRepoName, db)
	return w.Run(false)
}

// resolveGitFlag resolves a --git flag value into a canonical repo
// name and the filesystem path to the agenc-owned clone. The flag can be a
// local filesystem path (starts with /, ., or ~), a repo reference
// ("owner/repo" or "github.com/owner/repo"), or a GitHub URL
// (https://github.com/owner/repo/...).
func resolveGitFlag(agencDirpath string, flag string) (repoName string, cloneDirpath string, err error) {
	if isLocalPath(flag) {
		return resolveGitFlagFromLocalPath(agencDirpath, flag)
	}
	return resolveGitFlagFromRepoRef(agencDirpath, flag)
}

// resolveGitFlagFromLocalPath handles --git when it points to a local git
// repository. It validates the repo, extracts the GitHub remote URL, and
// clones into ~/.agenc/repos/.
func resolveGitFlagFromLocalPath(agencDirpath string, localPath string) (string, string, error) {
	absDirpath, err := filepath.Abs(localPath)
	if err != nil {
		return "", "", stacktrace.Propagate(err, "failed to resolve git repo path")
	}

	if err := mission.ValidateGitRepo(absDirpath); err != nil {
		return "", "", stacktrace.Propagate(err, "invalid git repository")
	}

	repoName, err := mission.ExtractGitHubRepoName(absDirpath)
	if err != nil {
		return "", "", stacktrace.Propagate(err, "failed to extract GitHub repo name")
	}

	// Read the clone URL from the user's repo (preserves SSH vs HTTPS)
	cloneURLCmd := exec.Command("git", "remote", "get-url", "origin")
	cloneURLCmd.Dir = absDirpath
	cloneURLOutput, err := cloneURLCmd.Output()
	if err != nil {
		return "", "", stacktrace.Propagate(err, "failed to read origin remote URL")
	}
	cloneURL := strings.TrimSpace(string(cloneURLOutput))

	cloneDirpath, err := mission.EnsureRepoClone(agencDirpath, repoName, cloneURL)
	if err != nil {
		return "", "", stacktrace.Propagate(err, "failed to ensure repo clone")
	}

	return repoName, cloneDirpath, nil
}

// resolveGitFlagFromRepoRef handles --git when it's a repo reference like
// "owner/repo" or "github.com/owner/repo". Clones via HTTPS into
// ~/.agenc/repos/ and validates the result.
func resolveGitFlagFromRepoRef(agencDirpath string, ref string) (string, string, error) {
	repoName, cloneURL, err := mission.ParseRepoReference(ref)
	if err != nil {
		return "", "", stacktrace.Propagate(err, "invalid --git value '%s'", ref)
	}

	cloneDirpath, err := mission.EnsureRepoClone(agencDirpath, repoName, cloneURL)
	if err != nil {
		return "", "", stacktrace.Propagate(err, "failed to clone '%s'", repoName)
	}

	if err := mission.ValidateGitRepo(cloneDirpath); err != nil {
		return "", "", stacktrace.Propagate(err, "cloned repository is not valid")
	}

	return repoName, cloneDirpath, nil
}

// isLocalPath returns true if the string looks like a filesystem path rather
// than a repo reference.
func isLocalPath(s string) bool {
	return strings.HasPrefix(s, "/") || strings.HasPrefix(s, ".") || strings.HasPrefix(s, "~")
}

// selectWithFzf presents templates in fzf and returns the selected repo name.
// If allowNone is true, a "NONE" option is prepended. Returns empty string if
// NONE is selected.
func selectWithFzf(templates map[string]config.AgentTemplateProperties, initialQuery string, allowNone bool) (string, error) {
	var lines []string
	if allowNone {
		lines = append(lines, "NONE")
	}
	for _, repo := range sortedRepoKeys(templates) {
		lines = append(lines, formatTemplateFzfLine(repo, templates[repo]))
	}
	input := strings.Join(lines, "\n")

	fzfBinary, err := exec.LookPath("fzf")
	if err != nil {
		return "", stacktrace.Propagate(err, "'fzf' binary not found in PATH; install fzf or pass the template name as an argument")
	}

	fzfArgs := []string{"--prompt", "Select agent template: "}
	if initialQuery != "" {
		fzfArgs = append(fzfArgs, "--query", initialQuery)
	}

	fzfCmd := exec.Command(fzfBinary, fzfArgs...)
	fzfCmd.Stdin = strings.NewReader(input)
	fzfCmd.Stderr = os.Stderr

	output, err := fzfCmd.Output()
	if err != nil {
		return "", stacktrace.Propagate(err, "fzf selection failed")
	}

	selected := strings.TrimSpace(string(output))
	if selected == "NONE" {
		return "", nil
	}
	return extractRepoFromFzfLine(selected), nil
}

// resolveOrPickTemplate resolves a template from an optional CLI argument,
// falling back to an interactive fzf picker when no unique match is found or
// no argument is provided.
func resolveOrPickTemplate(templates map[string]config.AgentTemplateProperties, args []string) (string, error) {
	if len(args) == 1 {
		resolved, resolveErr := resolveTemplate(templates, args[0])
		if resolveErr != nil {
			selected, fzfErr := selectWithFzf(templates, args[0], false)
			if fzfErr != nil {
				return "", stacktrace.Propagate(fzfErr, "failed to select agent template")
			}
			return selected, nil
		}
		return resolved, nil
	}
	selected, fzfErr := selectWithFzf(templates, "", false)
	if fzfErr != nil {
		return "", stacktrace.Propagate(fzfErr, "failed to select agent template")
	}
	return selected, nil
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
