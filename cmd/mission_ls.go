package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
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

	missions, err := db.ListMissions(lsAllFlag)
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

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tSTATUS\tAGENT\tPROMPT\tCREATED")
	for _, m := range missions {
		promptSnippet := m.Prompt
		if len(promptSnippet) > 60 {
			promptSnippet = promptSnippet[:57] + "..."
		}
		status := getMissionStatus(m.ID, m.Status)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			m.ID,
			status,
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

// getMissionStatus returns the unified status for a mission: RUNNING, STOPPED,
// or ARCHIVED. Archived missions are never checked for a running wrapper.
func getMissionStatus(missionID string, dbStatus string) string {
	if dbStatus == "archived" {
		return "ARCHIVED"
	}
	pidFilepath := config.GetMissionPIDFilepath(agencDirpath, missionID)
	pid, err := daemon.ReadPID(pidFilepath)
	if err == nil && pid != 0 && daemon.IsProcessRunning(pid) {
		return "RUNNING"
	}
	return "STOPPED"
}
