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
	Long: fmt.Sprintf(`Remove one or more agent templates from the template library.

Deletes the cloned repo from $AGENC_DIRPATH/repos/ and removes it from the
agentTemplates list in config.yml.

When called without arguments, opens an interactive fzf picker.

Accepts any of these formats:
  owner/repo                           - shorthand (e.g., mieubrisse/my-template)
  github.com/owner/repo                - canonical name
  https://github.com/owner/repo        - URL
  nickname                             - template nickname

You can also use search terms to find a template:
  %s %s %s my template        - searches for templates matching "my template"`,
		agencCmdStr, templateCmdStr, rmCmdStr),
	Args: cobra.ArbitraryArgs,
	RunE: runTemplateRm,
}

func init() {
	templateCmd.AddCommand(templateRmCmd)
}

// templateEntry holds a template repo name and its properties for resolution.
type templateEntry struct {
	RepoName string
	Nickname string
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

	// Build sorted list of template entries
	repoKeys := sortedRepoKeys(cfg.AgentTemplates)
	entries := make([]templateEntry, len(repoKeys))
	for i, repo := range repoKeys {
		entries[i] = templateEntry{
			RepoName: repo,
			Nickname: cfg.AgentTemplates[repo].Nickname,
		}
	}

	result, err := Resolve(strings.Join(args, " "), Resolver[templateEntry]{
		TryCanonical: func(input string) (templateEntry, bool, error) {
			if !looksLikeRepoReference(input) {
				return templateEntry{}, false, nil
			}
			name, _, err := mission.ParseRepoReference(input, false)
			if err != nil {
				return templateEntry{}, false, stacktrace.Propagate(err, "invalid repo reference '%s'", input)
			}
			// Return the parsed name; validation happens in removeSingleTemplate
			return templateEntry{RepoName: name}, true, nil
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
		FzfPrompt:   "Select templates to remove (TAB to multi-select): ",
		FzfHeaders:  []string{"NICKNAME", "REPO"},
		MultiSelect: true,
	})
	if err != nil {
		return err
	}

	if result.WasCancelled || len(result.Items) == 0 {
		return nil
	}

	// Print auto-select message if search matched exactly one
	if len(args) > 0 && len(result.Items) == 1 && !looksLikeRepoReference(strings.Join(args, " ")) {
		fmt.Printf("Auto-selected: %s\n", displayGitRepo(result.Items[0].RepoName))
	}

	for _, entry := range result.Items {
		if err := removeSingleTemplate(cfg, cm, entry.RepoName); err != nil {
			return err
		}
	}
	return nil
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
