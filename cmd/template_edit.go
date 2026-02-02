package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var templateEditPromptFlag string

var templateEditCmd = &cobra.Command{
	Use:   "edit [template-name]",
	Short: "Edit an agent template via a new mission with a repo copy",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTemplateEdit,
}

func init() {
	templateEditCmd.Flags().StringVarP(&templateEditPromptFlag, "prompt", "p", "", "initial prompt to send to claude")
	templateCmd.AddCommand(templateEditCmd)
}

func runTemplateEdit(cmd *cobra.Command, args []string) error {
	ensureDaemonRunning(agencDirpath)

	cfg, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	if len(cfg.AgentTemplates) == 0 {
		fmt.Printf("No agent templates found. Add templates with: agenc template add owner/repo\n")
		return stacktrace.NewError("no agent templates available to edit")
	}

	var templateName string

	if len(args) == 1 {
		resolved, resolveErr := resolveTemplate(cfg.AgentTemplates, args[0])
		if resolveErr != nil {
			// No match — fall through to fzf with initial query
			selected, fzfErr := selectWithFzf(cfg.AgentTemplates, args[0], false)
			if fzfErr != nil {
				return stacktrace.Propagate(fzfErr, "failed to select agent template")
			}
			templateName = selected
		} else {
			templateName = resolved
		}
	} else {
		selected, fzfErr := selectWithFzf(cfg.AgentTemplates, "", false)
		if fzfErr != nil {
			return stacktrace.Propagate(fzfErr, "failed to select agent template")
		}
		templateName = selected
	}

	templateCloneDirpath := config.GetRepoDirpath(agencDirpath, templateName)
	return createAndLaunchMission(agencDirpath, "", templateEditPromptFlag, templateName, templateCloneDirpath)
}
