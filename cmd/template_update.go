package cmd

import (
	"fmt"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var templateUpdateNicknameFlag string
var templateUpdateDefaultFlag string

var templateUpdateCmd = &cobra.Command{
	Use:   updateCmdStr + " [template|search-terms...]",
	Short: "Update properties of an installed agent template",
	Long: fmt.Sprintf(`Update properties of an installed agent template.

Without arguments, opens an interactive fzf picker to select a template.
With arguments, accepts a template reference (shorthand or full name) or search
terms to filter the list. If exactly one template matches, it is auto-selected.

Examples:
  %s %s %s owner/repo --%s "My Agent"
  %s %s %s my agent --%s repo`,
		agencCmdStr, templateCmdStr, updateCmdStr, nicknameFlagName,
		agencCmdStr, templateCmdStr, updateCmdStr, defaultFlagName),
	Args: cobra.ArbitraryArgs,
	RunE: runTemplateUpdate,
}

func init() {
	templateUpdateCmd.Flags().StringVar(&templateUpdateNicknameFlag, nicknameFlagName, "", "set or clear the template nickname")
	templateUpdateCmd.Flags().StringVar(&templateUpdateDefaultFlag, defaultFlagName, "",
		fmt.Sprintf("set or clear the mission context this template is the default for; valid values: %s", config.FormatDefaultForValues()))
	templateCmd.AddCommand(templateUpdateCmd)
}

func runTemplateUpdate(cmd *cobra.Command, args []string) error {
	nicknameChanged := cmd.Flags().Changed(nicknameFlagName)
	defaultChanged := cmd.Flags().Changed(defaultFlagName)

	if !nicknameChanged && !defaultChanged {
		return stacktrace.NewError("at least one of --%s or --%s must be provided", nicknameFlagName, defaultFlagName)
	}

	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	if len(cfg.AgentTemplates) == 0 {
		return stacktrace.NewError("no agent templates available to update")
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
		FzfPrompt:   "Select template to update: ",
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

	repo := selected.RepoName
	existing := cfg.AgentTemplates[repo]

	if nicknameChanged {
		if templateUpdateNicknameFlag != "" {
			for otherRepo, props := range cfg.AgentTemplates {
				if props.Nickname == templateUpdateNicknameFlag && otherRepo != repo {
					return stacktrace.NewError("nickname '%s' is already in use by '%s'", templateUpdateNicknameFlag, otherRepo)
				}
			}
		}
		existing.Nickname = templateUpdateNicknameFlag
	}

	if defaultChanged {
		if templateUpdateDefaultFlag != "" {
			if !config.IsValidDefaultForValue(templateUpdateDefaultFlag) {
				return stacktrace.NewError("invalid --%s value '%s'; must be one of: %s", defaultFlagName, templateUpdateDefaultFlag, config.FormatDefaultForValues())
			}
			for otherRepo, props := range cfg.AgentTemplates {
				if props.DefaultFor == templateUpdateDefaultFlag && otherRepo != repo {
					return stacktrace.NewError("defaultFor '%s' is already claimed by '%s'", templateUpdateDefaultFlag, otherRepo)
				}
			}
		}
		existing.DefaultFor = templateUpdateDefaultFlag
	}

	cfg.AgentTemplates[repo] = existing

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	if nicknameChanged {
		if templateUpdateNicknameFlag == "" {
			fmt.Printf("Cleared nickname for '%s'\n", repo)
		} else {
			fmt.Printf("Set nickname for '%s' to '%s'\n", repo, templateUpdateNicknameFlag)
		}
	}
	if defaultChanged {
		if templateUpdateDefaultFlag == "" {
			fmt.Printf("Cleared defaultFor for '%s'\n", repo)
		} else {
			fmt.Printf("Set defaultFor for '%s' to '%s'\n", repo, templateUpdateDefaultFlag)
		}
	}
	return nil
}
