package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var templateUpdateNicknameFlag string
var templateUpdateDefaultFlag string

var templateUpdateCmd = &cobra.Command{
	Use:   "update <template>",
	Short: "Update properties of an installed agent template",
	Args:  cobra.ExactArgs(1),
	RunE:  runTemplateUpdate,
}

func init() {
	templateUpdateCmd.Flags().StringVar(&templateUpdateNicknameFlag, "nickname", "", "set or clear the template nickname")
	templateUpdateCmd.Flags().StringVar(&templateUpdateDefaultFlag, "default", "",
		fmt.Sprintf("set or clear the mission context this template is the default for; valid values: %s", config.FormatDefaultForValues()))
	templateCmd.AddCommand(templateUpdateCmd)
}

func runTemplateUpdate(cmd *cobra.Command, args []string) error {
	nicknameChanged := cmd.Flags().Changed("nickname")
	defaultChanged := cmd.Flags().Changed("default")

	if !nicknameChanged && !defaultChanged {
		return stacktrace.NewError("at least one of --nickname or --default must be provided")
	}

	cfg, cm, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	repo, err := resolveTemplate(cfg.AgentTemplates, args[0])
	if err != nil {
		return err
	}

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
				return stacktrace.NewError("invalid --default value '%s'; must be one of: %s", templateUpdateDefaultFlag, config.FormatDefaultForValues())
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
