package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var lsAllFlag bool

var missionLsCmd = &cobra.Command{
	Use:   "ls",
	Short: "List active missions",
	RunE:  runMissionLs,
}

func init() {
	missionLsCmd.Flags().BoolVarP(&lsAllFlag, "all", "a", false, "include archived missions")
	missionCmd.AddCommand(missionLsCmd)
}

func runMissionLs(cmd *cobra.Command, args []string) error {
	dbFilepath := config.GetDatabaseFilepath(agencDirpath)
	db, err := database.Open(dbFilepath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to open database")
	}
	defer db.Close()

	var missions []*database.Mission
	if lsAllFlag {
		missions, err = db.ListAllMissions()
	} else {
		missions, err = db.ListActiveMissions()
	}
	if err != nil {
		return stacktrace.Propagate(err, "failed to list missions")
	}

	if len(missions) == 0 {
		if lsAllFlag {
			fmt.Println("No missions.")
		} else {
			fmt.Println("No active missions.")
		}
		return nil
	}

	// Batch-fetch descriptions for all listed missions
	missionIDs := make([]string, len(missions))
	for i, m := range missions {
		missionIDs[i] = m.ID
	}
	descriptions, err := db.GetDescriptionsForMissions(missionIDs)
	if err != nil {
		return stacktrace.Propagate(err, "failed to fetch mission descriptions")
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	if lsAllFlag {
		fmt.Fprintln(w, "ID\tSTATUS\tAGENT\tDESCRIPTION\tPROMPT\tCREATED")
	} else {
		fmt.Fprintln(w, "ID\tAGENT\tDESCRIPTION\tPROMPT\tCREATED")
	}
	for _, m := range missions {
		promptSnippet := m.Prompt
		if len(promptSnippet) > 60 {
			promptSnippet = promptSnippet[:57] + "..."
		}
		descSnippet := "(pending)"
		if desc, ok := descriptions[m.ID]; ok {
			descSnippet = desc
			if len(descSnippet) > 50 {
				descSnippet = descSnippet[:47] + "..."
			}
		}
		if lsAllFlag {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
				m.ID,
				m.Status,
				displayAgentTemplate(m.AgentTemplate),
				descSnippet,
				promptSnippet,
				m.CreatedAt.Format("2006-01-02 15:04"),
			)
		} else {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
				m.ID,
				displayAgentTemplate(m.AgentTemplate),
				descSnippet,
				promptSnippet,
				m.CreatedAt.Format("2006-01-02 15:04"),
			)
		}
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
