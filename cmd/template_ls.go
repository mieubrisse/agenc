package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/tableprinter"
)

var templateLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List installed agent templates",
	RunE:  runTemplateLs,
}

func init() {
	templateCmd.AddCommand(templateLsCmd)
}

func runTemplateLs(cmd *cobra.Command, args []string) error {
	cfg, _, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	if len(cfg.AgentTemplates) == 0 {
		fmt.Println("No agent templates installed.")
		return nil
	}

	tbl := tableprinter.NewTable("NICKNAME", "REPO", "DEFAULT FOR")
	for _, repo := range sortedRepoKeys(cfg.AgentTemplates) {
		props := cfg.AgentTemplates[repo]
		tbl.AddRow(
			formatNickname(props.Nickname),
			displayGitRepo(repo),
			formatDefaultFor(props.DefaultFor),
		)
	}
	tbl.Print()

	return nil
}

func formatNickname(nickname string) string {
	if nickname == "" {
		return "--"
	}
	return nickname
}

func formatDefaultFor(defaultFor string) string {
	if defaultFor == "" {
		return "--"
	}
	return defaultFor
}
