package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var templateEditCmd = &cobra.Command{
	Use:   editCmdStr + " [search-terms...]",
	Short: "Edit an agent template via a new mission with a repo copy",
	Args:  cobra.ArbitraryArgs,
	RunE:  runTemplateEdit,
}

func init() {
	templateCmd.AddCommand(templateEditCmd)
}

func runTemplateEdit(cmd *cobra.Command, args []string) error {
	ensureDaemonRunning(agencDirpath)

	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	if len(cfg.AgentTemplates) == 0 {
		fmt.Printf("No agent templates found. Add templates with: %s %s %s owner/repo\n", agencCmdStr, templateCmdStr, addCmdStr)
		return stacktrace.NewError("no agent templates available to edit")
	}

	templateName, err := resolveOrPickTemplate(cfg.AgentTemplates, args)
	if err != nil {
		return err
	}

	agentTemplate, err := resolveAgentTemplate(cfg, "", templateName)
	if err != nil {
		return err
	}

	templateCloneDirpath := config.GetRepoDirpath(agencDirpath, templateName)
	return createAndLaunchMission(agencDirpath, agentTemplate, templateName, templateCloneDirpath)
}
