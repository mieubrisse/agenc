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

	// First, try to resolve positional args as a git reference (URL, path, shorthand)
	if len(args) > 0 {
		joinedArgs := strings.Join(args, " ")
		if looksLikeRepoReference(joinedArgs) || (len(args) == 1 && looksLikeRepoReference(args[0])) {
			input := joinedArgs
			if len(args) == 1 {
				input = args[0]
			}
			result, err := ResolveRepoInput(agencDirpath, input, false, "Select repo: ")
			if err != nil {
				return err
			}
			return launchRepoEditMission(cfg, result.RepoName, result.CloneDirpath)
		}
	}

	// Fall back to search term matching against repos only (no templates)
	repos, err := findReposOnDisk(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to scan repos directory")
	}

	if len(repos) == 0 {
		fmt.Printf("No repos in library. Add repos with: %s %s %s owner/repo\n", agencCmdStr, repoCmdStr, addCmdStr)
		return stacktrace.NewError("no repos available to edit")
	}

	var repoName string

	if len(args) > 0 {
		// Match using glob-style pattern
		terms := args
		matches := matchReposGlob(repos, terms)
		if len(matches) == 1 {
			repoName = matches[0]
			fmt.Printf("Auto-selected: %s\n", displayGitRepo(repoName))
		} else {
			// Multiple or no matches - use fzf with search terms as initial query
			selected, err := selectReposWithFzfAndQuery(repos, "Select repo: ", strings.Join(terms, " "))
			if err != nil {
				return err
			}
			if len(selected) == 0 {
				return stacktrace.NewError("no repo selected")
			}
			repoName = selected[0]
		}
	} else {
		// No args - show fzf picker with all repos
		selected, err := selectReposWithFzf(repos, "Select repo: ")
		if err != nil {
			return err
		}
		if len(selected) == 0 {
			return stacktrace.NewError("no repo selected")
		}
		repoName = selected[0]
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
