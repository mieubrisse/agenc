package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var templateInstallNicknameFlag string

var templateInstallCmd = &cobra.Command{
	Use:   "install <repo>",
	Short: "Install an agent template from a GitHub repository",
	Long: `Install an agent template from a GitHub repository.

Accepts any of these formats:
  owner/repo
  github.com/owner/repo
  https://github.com/owner/repo`,
	Args: cobra.ExactArgs(1),
	RunE: runTemplateInstall,
}

func init() {
	templateInstallCmd.Flags().StringVar(&templateInstallNicknameFlag, "nickname", "", "optional friendly name for the template")
	templateCmd.AddCommand(templateInstallCmd)
}

func runTemplateInstall(cmd *cobra.Command, args []string) error {
	repoName, cloneURL, err := mission.ParseRepoReference(args[0])
	if err != nil {
		return stacktrace.Propagate(err, "invalid repo reference")
	}

	cfg, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	for _, entry := range cfg.AgentTemplates {
		if entry.Repo == repoName {
			fmt.Printf("Template '%s' already installed\n", repoName)
			return nil
		}
	}

	if _, err := mission.EnsureRepoClone(agencDirpath, repoName, cloneURL); err != nil {
		return stacktrace.Propagate(err, "failed to clone repository '%s'", repoName)
	}

	if templateInstallNicknameFlag != "" {
		for _, entry := range cfg.AgentTemplates {
			if entry.Nickname == templateInstallNicknameFlag {
				return stacktrace.NewError("nickname '%s' is already in use by '%s'", templateInstallNicknameFlag, entry.Repo)
			}
		}
	}

	cfg.AgentTemplates = append(cfg.AgentTemplates, config.AgentTemplateEntry{
		Repo:     repoName,
		Nickname: templateInstallNicknameFlag,
	})

	if err := config.WriteAgencConfig(agencDirpath, cfg); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Installed template '%s' from %s\n", repoName, cloneURL)
	return nil
}
