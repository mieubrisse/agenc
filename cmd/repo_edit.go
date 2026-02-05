package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var repoEditAgentFlag string

var repoEditCmd = &cobra.Command{
	Use:   editCmdStr + " [search-terms...]",
	Short: "Edit a repo via a new mission with a repo copy",
	Long: fmt.Sprintf(`Edit a repo via a new mission with a repo copy.

Positional arguments select a repo. They can be:
  - A git reference (URL, shorthand like owner/repo, or local path)
  - Search terms to match against your repo library ("my repo")

The --%s flag specifies the agent template using the same format as
positional args (git reference or search terms). Without it, the default
agent template for repos is used.`,
		agentFlagName),
	Args: cobra.ArbitraryArgs,
	RunE: runRepoEdit,
}

func init() {
	repoEditCmd.Flags().StringVar(&repoEditAgentFlag, agentFlagName, "", "agent template (URL, shorthand, local path, or search terms)")
	repoCmd.AddCommand(repoEditCmd)
}

func runRepoEdit(cmd *cobra.Command, args []string) error {
	ensureDaemonRunning(agencDirpath)

	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	repos, err := findReposOnDisk(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to scan repos directory")
	}

	if len(repos) == 0 {
		fmt.Printf("No repos in library. Add repos with: %s %s %s owner/repo\n", agencCmdStr, repoCmdStr, addCmdStr)
		return stacktrace.NewError("no repos available to edit")
	}

	result, err := Resolve(strings.Join(args, " "), Resolver[string]{
		TryCanonical: func(input string) (string, bool, error) {
			if !looksLikeRepoReference(input) {
				return "", false, nil
			}
			// For repo edit, use ResolveRepoInput which handles cloning from external sources
			resolved, err := ResolveRepoInput(agencDirpath, input, false, "Select repo: ")
			if err != nil {
				return "", false, err
			}
			return resolved.RepoName, true, nil
		},
		GetItems:    func() ([]string, error) { return repos, nil },
		ExtractText: func(repo string) string { return repo },
		FormatRow:   func(repo string) []string { return []string{displayGitRepo(repo)} },
		FzfPrompt:   "Select repo: ",
		FzfHeaders:  []string{"REPO"},
		MultiSelect: false,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled {
		return stacktrace.NewError("no repo selected")
	}

	if len(result.Items) == 0 {
		return stacktrace.NewError("no repo selected")
	}

	repoName := result.Items[0]

	// Print auto-select message if search matched exactly one
	if len(args) > 0 && !looksLikeRepoReference(strings.Join(args, " ")) {
		fmt.Printf("Auto-selected: %s\n", displayGitRepo(repoName))
	}

	cloneDirpath := config.GetRepoDirpath(agencDirpath, repoName)
	return launchRepoEditMission(cfg, repoName, cloneDirpath)
}

// launchRepoEditMission launches a mission to edit the given repo.
// The agent template is resolved from the --agent flag or config defaults.
func launchRepoEditMission(cfg *config.AgencConfig, repoName string, cloneDirpath string) error {
	agentTemplate, err := resolveAgentTemplate(cfg, repoEditAgentFlag, repoName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to resolve agent template")
	}

	return createAndLaunchMission(agencDirpath, agentTemplate, repoName, cloneDirpath, "")
}
