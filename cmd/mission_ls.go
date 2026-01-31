package cmd

import (
	"fmt"
	"os"
	"strings"
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

	nicknames := buildNicknameMap(db)

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
			displayAgentTemplate(m.AgentTemplate, nicknames),
			promptSnippet,
			m.CreatedAt.Format("2006-01-02 15:04"),
		)
	}
	w.Flush()

	return nil
}

func displayAgentTemplate(repo string, nicknames map[string]string) string {
	if repo == "" {
		return "(none)"
	}
	if nick, ok := nicknames[repo]; ok {
		return nick
	}
	return repo
}

// buildNicknameMap creates a map from repo -> nickname for all templates
// that have a nickname set.
func buildNicknameMap(db *database.DB) map[string]string {
	templates, err := db.ListAgentTemplates()
	if err != nil {
		return nil
	}
	m := make(map[string]string)
	for _, t := range templates {
		if t.Nickname != "" {
			m[t.Repo] = t.Nickname
		}
	}
	return m
}

func formatTemplateFzfLine(t *database.AgentTemplate) string {
	if t.Nickname != "" {
		return fmt.Sprintf("%s  (%s)", t.Nickname, t.Repo)
	}
	return t.Repo
}

func extractRepoFromFzfLine(line string) string {
	if idx := strings.LastIndex(line, "  ("); idx != -1 {
		return strings.TrimSuffix(line[idx+3:], ")")
	}
	return line
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
