package cmd

import (
	"fmt"

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
	repos, err := config.ListRepos(agencDirpath)
	if err != nil {
		return err
	}

	if len(repos) == 0 {
		fmt.Println("No agent templates installed.")
		return nil
	}

	for _, name := range repos {
		fmt.Println(name)
	}

	return nil
}
