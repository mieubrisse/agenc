package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/mission"
	"github.com/odyssey/agenc/internal/wrapper"
)

var agentFlag string
var promptFlag string
var gitFlag string

var missionNewCmd = &cobra.Command{
	Use:   "new",
	Short: "Create a new mission and launch claude",
	Long:  "Create a new mission and launch claude. The agent template is selected automatically from the defaultAgents config, or can be overridden with --agent.",
	Args:  cobra.NoArgs,
	RunE:  runMissionNew,
}

func init() {
	missionNewCmd.Flags().StringVar(&agentFlag, "agent", "", "agent template name (overrides defaultAgents config)")
	missionNewCmd.Flags().StringVarP(&promptFlag, "prompt", "p", "", "initial prompt to send to claude")
	missionNewCmd.Flags().StringVar(&gitFlag, "git", "", "git repo to copy into workspace (local path, owner/repo, or https://github.com/owner/repo/...)")
	missionCmd.AddCommand(missionNewCmd)
}

func runMissionNew(cmd *cobra.Command, args []string) error {
	ensureDaemonRunning(agencDirpath)

	cfg, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	// Resolve --git first so agent template selection can use the context
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

	return createAndLaunchMission(agencDirpath, agentTemplate, promptFlag, gitRepoName, gitCloneDirpath)
}

// resolveAgentTemplate determines which agent template to use for a new
// mission. If agentFlag is set, it resolves via resolveTemplate. Otherwise
// the defaultAgents config is consulted based on the git context.
func resolveAgentTemplate(cfg *config.AgencConfig, agentFlag string, gitRepoName string) (string, error) {
	if agentFlag != "" {
		resolved, err := resolveTemplate(cfg.AgentTemplates, agentFlag)
		if err != nil {
			return "", stacktrace.NewError("agent template '%s' not found", agentFlag)
		}
		return resolved, nil
	}

	// Pick the defaultAgents key based on git context
	var defaultRepo string
	switch {
	case gitRepoName == "":
		defaultRepo = cfg.DefaultAgents.Default
	case isAgentTemplate(cfg, gitRepoName):
		defaultRepo = cfg.DefaultAgents.AgentTemplate
	default:
		defaultRepo = cfg.DefaultAgents.Repo
	}

	if defaultRepo == "" {
		return "", nil
	}

	// Verify the default agent template is actually installed
	if _, ok := cfg.AgentTemplates[defaultRepo]; !ok {
		fmt.Fprintf(os.Stderr, "Warning: defaultAgents references '%s' which is not installed; proceeding without agent template\n", defaultRepo)
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
	prompt string,
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

	missionRecord, err := db.CreateMission(agentTemplate, prompt, gitRepoName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission record")
	}

	fmt.Printf("Created mission: %s\n", missionRecord.ID)

	// Create mission directory structure (repo goes inside workspace/)
	missionDirpath, err := mission.CreateMissionDir(agencDirpath, missionRecord.ID, agentTemplate, gitRepoName, gitCloneDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to create mission directory")
	}

	fmt.Printf("Mission directory: %s\n", missionDirpath)
	fmt.Println("Launching claude...")

	w := wrapper.NewWrapper(agencDirpath, missionRecord.ID, agentTemplate)
	return w.Run(prompt, false)
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
