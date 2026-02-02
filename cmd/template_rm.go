package cmd

import (
	"fmt"
	"os"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
)

var templateRmCmd = &cobra.Command{
	Use:   "rm <template>",
	Short: "Remove an installed agent template",
	Args:  cobra.ExactArgs(1),
	RunE:  runTemplateRm,
}

func init() {
	templateCmd.AddCommand(templateRmCmd)
}

func runTemplateRm(cmd *cobra.Command, args []string) error {
	cfg, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	repo, err := resolveTemplate(cfg.AgentTemplates, args[0])
	if err != nil {
		return err
	}

	delete(cfg.AgentTemplates, repo)

	if err := config.WriteAgencConfig(agencDirpath, cfg); err != nil {
		return stacktrace.Propagate(err, "failed to write config")
	}

	// Remove cloned repo from disk
	repoDirpath := config.GetRepoDirpath(agencDirpath, repo)
	if err := os.RemoveAll(repoDirpath); err != nil {
		return stacktrace.Propagate(err, "failed to remove repo directory '%s'", repoDirpath)
	}

	fmt.Printf("Removed template '%s'\n", repo)
	return nil
}
