package cmd

import (
	"fmt"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
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
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	templateRecords, err := db.ListAgentTemplates()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list agent templates")
	}

	repo, err := resolveTemplate(templateRecords, args[0])
	if err != nil {
		return err
	}

	if err := db.UpdateAgentTemplateNickname(repo, templateUpdateNicknameFlag); err != nil {
		return stacktrace.Propagate(err, "failed to update nickname")
	}

	if templateUpdateNicknameFlag == "" {
		fmt.Printf("Cleared nickname for '%s'\n", repo)
	} else {
		fmt.Printf("Set nickname for '%s' to '%s'\n", repo, templateUpdateNicknameFlag)
	}
	return nil
}
