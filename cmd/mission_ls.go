package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/kurtosis-tech/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var missionLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List active missions",
	RunE:  runMissionLs,
}

func init() {
	missionCmd.AddCommand(missionLsCmd)
}

func runMissionLs(cmd *cobra.Command, args []string) error {
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	missions, err := db.ListActiveMissions()
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		fmt.Println("No active missions.")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tAGENT\tPROMPT\tCREATED")
	for _, m := range missions {
		promptSnippet := m.Prompt
		if len(promptSnippet) > 60 {
			promptSnippet = promptSnippet[:57] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			m.ID,
			displayAgentTemplate(m.AgentTemplate),
			promptSnippet,
			m.CreatedAt.Format("2006-01-02 15:04"),
		)
	}
	w.Flush()

	return nil
}

func displayAgentTemplate(template string) string {
	if template == "" {
		return "(none)"
	}
	return template
}
