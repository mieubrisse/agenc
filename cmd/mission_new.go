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
	Use:   "new [agent-template]",
	Short: "Create a new mission and launch claude",
	Long:  "Create a new mission with an agent template and launch claude. If the template name matches exactly, it is used directly; otherwise an interactive selector is shown.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runMissionNew,
}

func init() {
	missionNewCmd.Flags().StringVar(&agentFlag, "agent", "", "exact agent template name (for programmatic use)")
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

	var agentTemplate string

	if agentFlag != "" {
		// --agent flag: match by repo or nickname
		resolved, resolveErr := resolveTemplate(cfg.AgentTemplates, agentFlag)
		if resolveErr != nil {
			return stacktrace.NewError("agent template '%s' not found", agentFlag)
		}
		agentTemplate = resolved
	} else if len(cfg.AgentTemplates) == 0 {
		fmt.Println("No agent templates found. Proceeding without a template.")
		fmt.Printf("Install templates with: agenc template install owner/repo\n")
	} else if len(args) == 1 {
		resolved, resolveErr := resolveTemplate(cfg.AgentTemplates, args[0])
		if resolveErr != nil {
			// No match found — fall through to fzf with initial query
			selected, fzfErr := selectWithFzf(cfg.AgentTemplates, args[0], true)
			if fzfErr != nil {
				return stacktrace.Propagate(fzfErr, "failed to select agent template")
			}
			if selected != "" {
				agentTemplate = selected
			}
		} else {
			agentTemplate = resolved
		}
	} else {
		selected, fzfErr := selectWithFzf(cfg.AgentTemplates, "", true)
		if fzfErr != nil {
			return stacktrace.Propagate(fzfErr, "failed to select agent template")
		}
		if selected != "" {
			agentTemplate = selected
		}
	}

	var gitRepoName string
	var gitCloneDirpath string
	if gitFlag != "" {
		repoName, cloneDirpath, err := resolveGitFlag(agencDirpath, gitFlag)
		if err != nil {
			return err
		}
		gitRepoName = repoName
		gitCloneDirpath = cloneDirpath
	}

	return createAndLaunchMission(agencDirpath, agentTemplate, promptFlag, gitRepoName, gitCloneDirpath)
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
func selectWithFzf(templates []config.AgentTemplateEntry, initialQuery string, allowNone bool) (string, error) {
	var lines []string
	if allowNone {
		lines = append(lines, "NONE")
	}
	for _, t := range templates {
		lines = append(lines, formatTemplateFzfLine(t))
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

// matchTemplatesSubstring returns templates whose Repo or Nickname contain the
// given substring (case-sensitive).
func matchTemplatesSubstring(templates []config.AgentTemplateEntry, substr string) []config.AgentTemplateEntry {
	var matches []config.AgentTemplateEntry
	for _, t := range templates {
		if strings.Contains(t.Repo, substr) || strings.Contains(t.Nickname, substr) {
			matches = append(matches, t)
		}
	}
	return matches
}

// resolveTemplate attempts to find exactly one template matching the given
// query. It tries exact match on repo, then exact match on nickname, then
// single substring match on either field.
func resolveTemplate(templates []config.AgentTemplateEntry, query string) (string, error) {
	// Exact match by repo
	for _, t := range templates {
		if t.Repo == query {
			return t.Repo, nil
		}
	}
	// Exact match by nickname
	for _, t := range templates {
		if t.Nickname == query {
			return t.Repo, nil
		}
	}
	// Single substring match
	matches := matchTemplatesSubstring(templates, query)
	if len(matches) == 1 {
		return matches[0].Repo, nil
	}
	return "", stacktrace.NewError("no unique template match for '%s'", query)
}
