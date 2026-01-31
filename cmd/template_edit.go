package cmd

import (
	"fmt"
	"slices"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var templateEditPromptFlag string

var templateEditCmd = &cobra.Command{
	Use:   "edit [template-name]",
	Short: "Edit an agent template via a new worktree mission",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runTemplateEdit,
}

func init() {
	templateEditCmd.Flags().StringVarP(&templateEditPromptFlag, "prompt", "p", "", "initial prompt to send to claude")
	templateCmd.AddCommand(templateEditCmd)
}

func runTemplateEdit(cmd *cobra.Command, args []string) error {
	ensureDaemonRunning(agencDirpath)

	templates, err := config.ListAgentTemplates(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to list agent templates")
	}
	if len(templates) == 0 {
		fmt.Printf("No agent templates found. Create templates in: %s\n", config.GetAgentTemplatesDirpath(agencDirpath))
		return stacktrace.NewError("no agent templates available to edit")
	}

	var templateName string

	if len(args) == 1 && slices.Contains(templates, args[0]) {
		// Exact match
		templateName = args[0]
	} else if len(args) == 1 && len(matchTemplatesSubstring(templates, args[0])) == 1 {
		// Single substring match
		templateName = matchTemplatesSubstring(templates, args[0])[0]
	} else {
		initialQuery := ""
		if len(args) == 1 {
			initialQuery = args[0]
		}
		selected, err := selectWithFzf(templates, initialQuery, false)
		if err != nil {
			return stacktrace.Propagate(err, "failed to select agent template")
		}
		templateName = selected
	}

	templateAbsDirpath := config.GetAgentTemplateDirpath(agencDirpath, templateName)
	return createAndLaunchMission(agencDirpath, "", templateEditPromptFlag, templateAbsDirpath)
}
