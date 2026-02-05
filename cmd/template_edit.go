package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var templateEditCmd = &cobra.Command{
	Use:   editCmdStr + " [template|search-terms...]",
	Short: "Edit an agent template via a new mission with a repo copy",
	Long: fmt.Sprintf(`Edit an agent template via a new mission with a repo copy.

Without arguments, opens an interactive fzf picker to select a template.
With arguments, accepts a template reference (shorthand or full name) or search
terms to filter the list. If exactly one template matches, it is auto-selected.

Examples:
  %s %s %s owner/repo
  %s %s %s my agent`,
		agencCmdStr, templateCmdStr, editCmdStr,
		agencCmdStr, templateCmdStr, editCmdStr),
	Args: cobra.ArbitraryArgs,
	RunE: runTemplateEdit,
}

func init() {
	templateCmd.AddCommand(templateEditCmd)
}

func runTemplateEdit(cmd *cobra.Command, args []string) error {
	cfg, err := readConfig()
	if err != nil {
		return err
	}

	if len(cfg.AgentTemplates) == 0 {
		fmt.Printf("No agent templates found. Add templates with: %s %s %s owner/repo\n", agencCmdStr, templateCmdStr, addCmdStr)
		return stacktrace.NewError("no agent templates available to edit")
	}

	// Build sorted list of template entries
	repoKeys := sortedRepoKeys(cfg.AgentTemplates)
	entries := make([]templateEntry, len(repoKeys))
	for i, repo := range repoKeys {
		entries[i] = templateEntry{
			RepoName: repo,
			Nickname: cfg.AgentTemplates[repo].Nickname,
		}
	}

	input := strings.Join(args, " ")
	result, err := Resolve(input, Resolver[templateEntry]{
		TryCanonical: func(input string) (templateEntry, bool, error) {
			if !looksLikeRepoReference(input) {
				return templateEntry{}, false, nil
			}
			name, _, err := mission.ParseRepoReference(input, false)
			if err != nil {
				return templateEntry{}, false, stacktrace.Propagate(err, "invalid repo reference '%s'", input)
			}
			if _, exists := cfg.AgentTemplates[name]; !exists {
				return templateEntry{}, false, stacktrace.NewError("template '%s' not found", name)
			}
			return templateEntry{RepoName: name, Nickname: cfg.AgentTemplates[name].Nickname}, true, nil
		},
		GetItems: func() ([]templateEntry, error) { return entries, nil },
		ExtractText: func(e templateEntry) string {
			return formatTemplateFzfLine(e.RepoName, config.AgentTemplateProperties{Nickname: e.Nickname})
		},
		FormatRow: func(e templateEntry) []string {
			nickname := "--"
			if e.Nickname != "" {
				nickname = e.Nickname
			}
			return []string{nickname, displayGitRepo(e.RepoName)}
		},
		FzfPrompt:   "Select template to edit: ",
		FzfHeaders:  []string{"NICKNAME", "REPO"},
		MultiSelect: false,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	selected := result.Items[0]

	// Print auto-select message if search matched exactly one
	if input != "" && !looksLikeRepoReference(input) {
		fmt.Printf("Auto-selected: %s\n", displayGitRepo(selected.RepoName))
	}

	return launchTemplateEditMission(agencDirpath, selected.RepoName, "")
}
