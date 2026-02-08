package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/daemon"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/history"
	"github.com/odyssey/agenc/internal/session"
	"github.com/odyssey/agenc/internal/tableprinter"
)

const defaultMissionLsLimit = 20

var lsAllFlag bool
var lsCronFlag string

var missionLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List active missions",
	RunE:  runMissionLs,
}

func init() {
	missionLsCmd.Flags().BoolVarP(&lsAllFlag, allFlagName, "a", false, "include archived missions")
	missionLsCmd.Flags().StringVar(&lsCronFlag, cronFlagName, "", "filter to missions from a specific cron job")
	missionCmd.AddCommand(missionLsCmd)
}

func runMissionLs(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	params := database.ListMissionsParams{IncludeArchived: lsAllFlag}
	if lsCronFlag != "" {
		params.CronID = &lsCronFlag
	}
	missions, err := db.ListMissions(params)
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

	totalCount := len(missions)
	displayMissions := missions
	if !lsAllFlag && totalCount > defaultMissionLsLimit {
		displayMissions = missions[:defaultMissionLsLimit]
	}

	tbl := tableprinter.NewTable("LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO")
	for _, m := range displayMissions {
		status := getMissionStatus(m.ID, m.Status)
		sessionName := resolveSessionName(db, m)
		tbl.AddRow(
			formatLastActive(m.LastHeartbeat),
			m.ShortID,
			colorizeStatus(status),
			truncatePrompt(sessionName, defaultPromptMaxLen),
			displayGitRepo(m.GitRepo),
		)
	}
	tbl.Print()

	if !lsAllFlag && totalCount > defaultMissionLsLimit {
		remaining := totalCount - defaultMissionLsLimit
		fmt.Printf("\n...and %d more missions; run '%s %s %s --%s' to see all missions\n",
			remaining, agencCmdStr, missionCmdStr, lsCmdStr, allFlagName)
	}

	return nil
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

// formatLastActive returns a human-readable representation of the last
// heartbeat timestamp. Returns "--" if the mission has never sent a heartbeat.
func formatLastActive(lastHeartbeat *time.Time) string {
	if lastHeartbeat == nil {
		return "--"
	}
	return lastHeartbeat.Local().Format("2006-01-02 15:04")
}

// colorizeStatus wraps a status string with ANSI color codes.
func colorizeStatus(status string) string {
	switch status {
	case "RUNNING":
		return ansiGreen + status + ansiReset
	case "ARCHIVED":
		return ansiYellow + status + ansiReset
	default:
		return status
	}
}

const defaultPromptMaxLen = 53

// resolveSessionName returns the Claude Code session name for a mission.
// It uses a cached value from the database when the cache is fresh (i.e. the
// mission's heartbeat has not advanced past the last cache update). Otherwise
// it performs the expensive file lookup, caches the result, and returns it.
// Falls back to the mission's first user prompt if no session name is found.
//
// Session data lives in the per-mission claude-config directory (for newer
// missions) or the global claude directory (for older missions).
func resolveSessionName(db *database.DB, m *database.Mission) string {
	if isSessionNameCacheFresh(m) {
		return m.SessionName
	}

	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(agencDirpath, m.ID)
	sessionName := session.FindSessionName(claudeConfigDirpath, m.ID)
	if sessionName != "" {
		_ = db.UpdateMissionSessionName(m.ID, sessionName)
		return sessionName
	}
	return resolveMissionPrompt(db, m)
}

// isSessionNameCacheFresh reports whether the cached session name for a
// mission is still valid. The cache is fresh when it has been populated and
// the mission's heartbeat has not advanced past the cache timestamp.
func isSessionNameCacheFresh(m *database.Mission) bool {
	if m.SessionNameUpdatedAt == nil {
		return false
	}
	if m.LastHeartbeat == nil {
		// No heartbeat — cache can't be stale from activity.
		return m.SessionName != ""
	}
	return !m.LastHeartbeat.After(*m.SessionNameUpdatedAt)
}

// resolveMissionPrompt returns the mission's first user prompt, using the DB
// cache if available, otherwise backfilling from Claude's history.jsonl. The
// returned string may be empty if no prompt has been recorded yet.
//
// History data lives in the per-mission claude-config directory (for newer
// missions) or the global claude directory (for older missions).
func resolveMissionPrompt(db *database.DB, m *database.Mission) string {
	if m.Prompt != "" {
		return m.Prompt
	}

	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(agencDirpath, m.ID)
	historyFilepath := filepath.Join(claudeConfigDirpath, config.HistoryFilename)
	prompt := history.FindFirstPrompt(historyFilepath, m.ID)
	if prompt == "" {
		return ""
	}

	// Cache in DB for future reads; ignore errors since this is best-effort.
	_ = db.UpdateMissionPrompt(m.ID, prompt)
	m.Prompt = prompt
	return prompt
}

// truncatePrompt collapses whitespace and truncates a prompt string for
// display. Returns "--" for an empty prompt.
func truncatePrompt(prompt string, maxLen int) string {
	if prompt == "" {
		return "--"
	}
	collapsed := strings.Join(strings.Fields(prompt), " ")
	if len(collapsed) <= maxLen {
		return collapsed
	}
	return collapsed[:maxLen] + "…"
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
