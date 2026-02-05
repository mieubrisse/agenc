package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var templateRmCmd = &cobra.Command{
	Use:   rmCmdStr + " [template...]",
	Short: "Remove an installed agent template",
	Long: `Remove one or more agent templates from the template library.

Deletes the cloned repo from ~/.agenc/repos/ and removes it from the
agentTemplates list in config.yml.

When called without arguments, opens an interactive fzf picker.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/my-template)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - URL
  nickname                             - template nickname

You can also use search terms to find a template:
  agenc template rm my template        - searches for templates matching "my template"`,
	Args: cobra.ArbitraryArgs,
	RunE: runTemplateRm,
}

func init() {
	templateCmd.AddCommand(templateRmCmd)
}

func runTemplateRm(cmd *cobra.Command, args []string) error {
	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	if len(cfg.AgentTemplates) == 0 {
		fmt.Println("No agent templates installed.")
		return nil
	}

	repoNames, err := resolveTemplateRmArgs(cfg, args)
	if err != nil {
		return err
	}
	if len(repoNames) == 0 {
		return nil
	}

	for _, repoName := range repoNames {
		if err := removeSingleTemplate(cfg, cm, repoName); err != nil {
			return err
		}
	}
	return nil
}

// resolveTemplateRmArgs resolves CLI arguments to canonical repo names for template removal.
// When no arguments are given, opens an fzf picker.
func resolveTemplateRmArgs(cfg *config.AgencConfig, args []string) ([]string, error) {
	// Get list of template repo names
	var templateRepos []string
	for repoName := range cfg.AgentTemplates {
		templateRepos = append(templateRepos, repoName)
	}

	if len(templateRepos) == 0 {
		return nil, nil
	}

	if len(args) == 0 {
		// No args - open fzf picker
		return selectTemplatesWithFzf(cfg.AgentTemplates, "Select templates to remove (TAB to multi-select): ", "")
	}

	// Check if the first arg looks like a repo reference
	if looksLikeRepoReference(args[0]) {
		// Each arg is a repo reference - resolve to canonical names
		var repoNames []string
		for _, arg := range args {
			repoName, _, parseErr := mission.ParseRepoReference(arg, false)
			if parseErr != nil {
				return nil, stacktrace.Propagate(parseErr, "invalid repo reference '%s'", arg)
			}
			repoNames = append(repoNames, repoName)
		}
		return repoNames, nil
	}

	// Treat args as search terms
	matches := matchTemplateEntries(cfg.AgentTemplates, args)

	if len(matches) == 1 {
		fmt.Printf("Auto-selected: %s\n", displayGitRepo(matches[0]))
		return matches, nil
	}

	// Multiple or no matches - use fzf with initial query
	searchTerms := strings.Join(args, " ")
	return selectTemplatesWithFzf(cfg.AgentTemplates, "Select templates to remove (TAB to multi-select): ", searchTerms)
}

// selectTemplatesWithFzf presents an fzf multi-select picker for templates
// and returns the selected canonical repo names. Returns nil (no error) if the
// user cancels with Ctrl-C or Escape.
func selectTemplatesWithFzf(templates map[string]config.AgentTemplateProperties, prompt string, initialQuery string) ([]string, error) {
	repoKeys := sortedRepoKeys(templates)
	if len(repoKeys) == 0 {
		return nil, nil
	}

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

	indices, err := runFzfPicker(FzfPickerConfig{
		Prompt:       prompt,
		Headers:      []string{"NICKNAME", "REPO"},
		Rows:         rows,
		MultiSelect:  true,
		InitialQuery: initialQuery,
	})
	if err != nil {
		return nil, stacktrace.Propagate(err, "'fzf' binary not found in PATH; pass template names as arguments instead")
	}
	if indices == nil {
		return nil, nil
	}

	var selected []string
	for _, idx := range indices {
		selected = append(selected, repoKeys[idx])
	}
	return selected, nil
}

func removeSingleTemplate(cfg *config.AgencConfig, cm yaml.CommentMap, repoName string) error {
	if _, exists := cfg.AgentTemplates[repoName]; !exists {
		fmt.Printf("Template '%s' not found\n", repoName)
		return nil
	}

	delete(cfg.AgentTemplates, repoName)

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	// Remove cloned repo from disk
	repoDirpath := config.GetRepoDirpath(agencDirpath, repoName)
	if err := os.RemoveAll(repoDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to remove repo directory '%s'", repoDirpath)
	}

	fmt.Printf("Removed template '%s'\n", repoName)
	return nil
}
