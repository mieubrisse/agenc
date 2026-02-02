package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var templateLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List installed agent templates",
	RunE:  runTemplateLs,
}

func init() {
	templateCmd.AddCommand(templateLsCmd)
}

func runTemplateLs(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	if len(cfg.AgentTemplates) == 0 {
		fmt.Println("No agent templates installed.")
		return nil
	}

	for _, entry := range cfg.AgentTemplates {
		if entry.Nickname != "" {
			fmt.Println(entry.Nickname)
		} else {
			fmt.Println(entry.Repo)
		}
	}

	return nil
}
