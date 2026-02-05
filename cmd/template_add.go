package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/mission"
)

var templateAddNicknameFlag string
var templateAddDefaultFlag string

var templateAddCmd = &cobra.Command{
	Use:   addCmdStr + " <repo>",
	Short: "Add an agent template from a GitHub repository",
	Long: `Add an agent template from a GitHub repository.

Accepts any of these formats:
  owner/repo
  github.com/owner/repo
  https://github.com/owner/repo
  git@github.com:owner/repo.git

The clone protocol is auto-detected: explicit URLs preserve their protocol,
while shorthand references (owner/repo) use the protocol inferred from
existing repos in the library.`,
	Args: cobra.ExactArgs(1),
	RunE: runTemplateAdd,
}

func init() {
	templateAddCmd.Flags().StringVar(&templateAddNicknameFlag, "nickname", "", "optional friendly name for the template")
	templateAddCmd.Flags().StringVar(&templateAddDefaultFlag, "default", "",
		fmt.Sprintf("make this template the default for a mission context; valid values: %s", config.FormatDefaultForValues()))
	templateCmd.AddCommand(templateAddCmd)
}

func runTemplateAdd(cmd *cobra.Command, args []string) error {
	preferSSH := mission.DetectPreferredProtocol(agencDirpath)
	repoName, cloneURL, err := mission.ParseRepoReference(args[0], preferSSH)
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

	if templateAddDefaultFlag != "" {
		if !config.IsValidDefaultForValue(templateAddDefaultFlag) {
			return stacktrace.NewError("invalid --default value '%s'; must be one of: %s", templateAddDefaultFlag, config.FormatDefaultForValues())
		}
		for otherRepo, props := range cfg.AgentTemplates {
			if props.DefaultFor == templateAddDefaultFlag {
				return stacktrace.NewError("defaultFor '%s' is already claimed by '%s'", templateAddDefaultFlag, otherRepo)
			}
		}
	}

	cfg.AgentTemplates[repoName] = config.AgentTemplateProperties{
		Nickname:   templateAddNicknameFlag,
		DefaultFor: templateAddDefaultFlag,
	}

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	fmt.Printf("Added template '%s' from %s\n", repoName, cloneURL)
	return nil
}
