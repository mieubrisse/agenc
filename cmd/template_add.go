package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var templateAddNicknameFlag string

var templateAddCmd = &cobra.Command{
	Use:   "add <repo>",
	Short: "Add an agent template from a GitHub repository",
	Long: `Add an agent template from a GitHub repository.

Accepts any of these formats:
  owner/repo
  github.com/owner/repo
  https://github.com/owner/repo`,
	Args: cobra.ExactArgs(1),
	RunE: runTemplateAdd,
}

func init() {
	templateAddCmd.Flags().StringVar(&templateAddNicknameFlag, "nickname", "", "optional friendly name for the template")
	templateCmd.AddCommand(templateAddCmd)
}

func runTemplateAdd(cmd *cobra.Command, args []string) error {
	repoName, cloneURL, err := mission.ParseRepoReference(args[0])
	if err != nil {
		return stacktrace.Propagate(err, "invalid repo reference")
	}

	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	if _, exists := cfg.AgentTemplates[repoName]; exists {
		fmt.Printf("Template '%s' already added\n", repoName)
		return nil
	}

	if _, err := mission.EnsureRepoClone(agencDirpath, repoName, cloneURL); err != nil {
		return stacktrace.Propagate(err, "failed to clone repository '%s'", repoName)
	}

	if templateAddNicknameFlag != "" {
		for otherRepo, props := range cfg.AgentTemplates {
			if props.Nickname == templateAddNicknameFlag {
				return stacktrace.NewError("nickname '%s' is already in use by '%s'", templateAddNicknameFlag, otherRepo)
			}
		}
	}

	cfg.AgentTemplates[repoName] = config.AgentTemplateProperties{
		Nickname: templateAddNicknameFlag,
	}

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Added template '%s' from %s\n", repoName, cloneURL)
	return nil
}
