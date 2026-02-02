package cmd

import (
	"fmt"
	"os"
	"sort"
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

	cfg, err := config.ReadAgencConfig(agencDirpath)
	if err != nil {
		return stacktrace.Propagate(err, "failed to read config")
	}

	nicknames := buildNicknameMap(cfg.AgentTemplates)

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', tabwriter.StripEscape)
	fmt.Fprintln(w, "ID\tSTATUS\tAGENT\tREPO\tCREATED")
	for _, m := range missions {
		status := getMissionStatus(m.ID, m.Status)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			m.ID,
			colorizeStatus(status),
			displayAgentTemplate(m.AgentTemplate, nicknames),
			displayGitRepo(m.GitRepo),
			m.CreatedAt.Format("2006-01-02 15:04"),
		)
	}
	w.Flush()

	return nil
}

func displayAgentTemplate(repo string, nicknames map[string]string) string {
	if repo == "" {
		return "--"
	}
	if nick, ok := nicknames[repo]; ok {
		return nick
	}
	return repo
}

func displayGitRepo(gitRepo string) string {
	if gitRepo == "" {
		return "--"
	}
	return strings.TrimPrefix(gitRepo, "github.com/")
}

// buildNicknameMap creates a map from repo -> nickname for all templates
// that have a nickname set.
func buildNicknameMap(templates map[string]config.AgentTemplateProperties) map[string]string {
	m := make(map[string]string)
	for repo, props := range templates {
		if props.Nickname != "" {
			m[repo] = props.Nickname
		}
	}
	return m
}

// sortedRepoKeys returns the map keys sorted alphabetically.
func sortedRepoKeys(templates map[string]config.AgentTemplateProperties) []string {
	repos := make([]string, 0, len(templates))
	for repo := range templates {
		repos = append(repos, repo)
	}
	sort.Strings(repos)
	return repos
}

func formatTemplateFzfLine(repo string, props config.AgentTemplateProperties) string {
	if props.Nickname != "" {
		return fmt.Sprintf("%s  (%s)", props.Nickname, repo)
	}
	return repo
}

func extractRepoFromFzfLine(line string) string {
	if idx := strings.LastIndex(line, "  ("); idx != -1 {
		return strings.TrimSuffix(line[idx+3:], ")")
	}
	return line
}

// tabEscape wraps s in tabwriter escape markers (\xff) so that its byte
// length is excluded from column width calculation.
func tabEscape(s string) string {
	return "\xff" + s + "\xff"
}

// colorizeStatus wraps a status string with ANSI color codes. The ANSI
// sequences are bracketed with tabwriter escape markers so they don't
// affect column alignment.
func colorizeStatus(status string) string {
	switch status {
	case "RUNNING":
		return tabEscape("\033[32m") + status + tabEscape("\033[0m")
	case "ARCHIVED":
		return tabEscape("\033[33m") + status + tabEscape("\033[0m")
	default:
		return status
	}
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
