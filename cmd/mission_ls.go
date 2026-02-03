package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/tableprinter"
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

	tbl := tableprinter.NewTable("ID", "STATUS", "AGENT", "REPO", "CREATED")
	for _, m := range missions {
		status := getMissionStatus(m.ID, m.Status)
		tbl.AddRow(
			m.ID,
			colorizeStatus(status),
			displayAgentTemplate(m.AgentTemplate, nicknames),
			displayGitRepo(m.GitRepo),
			m.CreatedAt.Format("2006-01-02 15:04"),
		)
	}
	tbl.Print()

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

// displayGitRepo formats a canonical repo name for user-facing display.
// GitHub repos have their "github.com/" prefix stripped; non-GitHub repos are
// shown in full. The repo name (final path segment) is colored light blue.
// Returns "--" for an empty repo name.
func displayGitRepo(gitRepo string) string {
	if gitRepo == "" {
		return "--"
	}
	display := strings.TrimPrefix(gitRepo, "github.com/")
	if idx := strings.LastIndex(display, "/"); idx != -1 {
		return display[:idx+1] + ansiLightBlue + display[idx+1:] + ansiReset
	}
	return ansiLightBlue + display + ansiReset
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

// colorizeStatus wraps a status string with ANSI color codes.
func colorizeStatus(status string) string {
	switch status {
	case "RUNNING":
		return "\033[32m" + status + "\033[0m"
	case "ARCHIVED":
		return "\033[33m" + status + "\033[0m"
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
