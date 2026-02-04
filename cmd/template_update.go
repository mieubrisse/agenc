package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var templateUpdateNicknameFlag string

var templateUpdateCmd = &cobra.Command{
	Use:   "update <template>",
	Short: "Update properties of an installed agent template",
	Args:  cobra.ExactArgs(1),
	RunE:  runTemplateUpdate,
}

func init() {
	templateUpdateCmd.Flags().StringVar(&templateUpdateNicknameFlag, "nickname", "", "set or clear the template nickname")
	templateUpdateCmd.MarkFlagRequired("nickname")
	templateCmd.AddCommand(templateUpdateCmd)
}

func runTemplateUpdate(cmd *cobra.Command, args []string) error {
	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	repo, err := resolveTemplate(cfg.AgentTemplates, args[0])
	if err != nil {
		return err
	}

	// Check nickname uniqueness
	if templateUpdateNicknameFlag != "" {
		for otherRepo, props := range cfg.AgentTemplates {
			if props.Nickname == templateUpdateNicknameFlag && otherRepo != repo {
				return stacktrace.NewError("nickname '%s' is already in use by '%s'", templateUpdateNicknameFlag, otherRepo)
			}
		}
	}

	existing := cfg.AgentTemplates[repo]
	existing.Nickname = templateUpdateNicknameFlag
	cfg.AgentTemplates[repo] = existing

	if err := config.WriteAgencConfig(agencDirpath, cfg, cm); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	if templateUpdateNicknameFlag == "" {
		fmt.Printf("Cleared nickname for '%s'\n", repo)
	} else {
		fmt.Printf("Set nickname for '%s' to '%s'\n", repo, templateUpdateNicknameFlag)
	}
	return nil
}
