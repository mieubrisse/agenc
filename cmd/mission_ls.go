package cmd

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/rodaine/table"
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

	// Compute shadow repo HEAD once for config staleness display
	var shadowHeadCommitHash string
	if lsAllFlag {
		shadowHeadCommitHash = claudeconfig.GetShadowRepoCommitHash(agencDirpath)
	}

	var tbl table.Table
	if lsAllFlag {
		tbl = tableprinter.NewTable("LAST ACTIVE", "ID", "STATUS", "PANE", "CONFIG", "SESSION", "REPO")
	} else {
		tbl = tableprinter.NewTable("LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO")
	}
	for _, m := range displayMissions {
		status := getMissionStatus(m.ID, m.Status)
		displayName := resolveMissionDisplayName(db, m)
		repo := displayGitRepo(m.GitRepo)
		if config.IsMissionAssistant(agencDirpath, m.ID) {
			repo = "💁‍♂️  AgenC Assistant"
		}
		if lsAllFlag {
			pane := "--"
			if m.TmuxPane != nil {
				pane = *m.TmuxPane
			}
			tbl.AddRow(
				formatLastActive(m.LastActive, m.LastHeartbeat),
				m.ShortID,
				colorizeStatus(status),
				pane,
				formatConfigCommit(m.ConfigCommit, shadowHeadCommitHash),
				truncatePrompt(displayName, defaultPromptMaxLen),
				repo,
			)
		} else {
			tbl.AddRow(
				formatLastActive(m.LastActive, m.LastHeartbeat),
				m.ShortID,
				colorizeStatus(status),
				truncatePrompt(displayName, defaultPromptMaxLen),
				repo,
			)
		}
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

// formatLastActive returns a human-readable timestamp of the mission's last
// activity. Prefers LastActive (user prompt submission) over LastHeartbeat
// (wrapper liveness). Returns "--" if neither timestamp is available.
func formatLastActive(lastActive *time.Time, lastHeartbeat *time.Time) string {
	if lastActive != nil {
		return lastActive.Local().Format("2006-01-02 15:04")
	}
	if lastHeartbeat != nil {
		return lastHeartbeat.Local().Format("2006-01-02 15:04")
	}
	return "--"
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

// resolveMissionDisplayName returns the best human-readable name for a mission.
// Priority: user-set description > session name > first user prompt.
func resolveMissionDisplayName(db *database.DB, m *database.Mission) string {
	if m.Description != "" {
		return m.Description
	}
	return resolveSessionName(db, m)
}

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

// formatConfigCommit returns a display string for a mission's config commit.
// Shows the 12-char short hash, plus "(-N)" in red if behind HEAD.
// Returns "--" if the mission has no config commit.
func formatConfigCommit(configCommit *string, shadowHeadCommitHash string) string {
	if configCommit == nil {
		return "--"
	}

	display := shortHash(*configCommit)

	if shadowHeadCommitHash == "" {
		return display
	}

	behind := claudeconfig.CountCommitsBehind(agencDirpath, *configCommit, shadowHeadCommitHash)
	if behind > 0 {
		display += fmt.Sprintf(" %s(-%d)%s", ansiRed, behind, ansiReset)
	} else if behind < 0 {
		display += " " + ansiRed + "(??)" + ansiReset
	}

	return display
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
