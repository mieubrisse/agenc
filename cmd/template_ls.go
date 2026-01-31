package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
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
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return err
	}
	defer db.Close()

	templates, err := db.ListAgentTemplates()
	if err != nil {
		return err
	}

	if len(templates) == 0 {
		fmt.Println("No agent templates installed.")
		return nil
	}

	for _, t := range templates {
		fmt.Println(t.Repo)
	}

	return nil
}
