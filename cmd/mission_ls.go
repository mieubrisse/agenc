package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/rodaine/table"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/server"
	"github.com/odyssey/agenc/internal/tableprinter"
)

const defaultMissionLsLimit = 20

// MissionDisplayStatus represents the display status of a mission.
type MissionDisplayStatus string

const (
	StatusIdle     MissionDisplayStatus = "IDLE"
	StatusBusy     MissionDisplayStatus = "BUSY"
	StatusWaiting  MissionDisplayStatus = "WAITING"
	StatusRunning  MissionDisplayStatus = "RUNNING"
	StatusStopped  MissionDisplayStatus = "STOPPED"
	StatusArchived MissionDisplayStatus = "ARCHIVED"
)

var lsAllFlag bool
var lsCronFlag string
var lsSinceFlag string
var lsUntilFlag string

var missionLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List active missions",
	RunE:  runMissionLs,
}

func init() {
	missionLsCmd.Flags().BoolVarP(&lsAllFlag, allFlagName, "a", false, "include archived missions")
	missionLsCmd.Flags().StringVar(&lsCronFlag, cronFlagName, "", "filter to missions from a specific cron job")
	missionLsCmd.Flags().StringVar(&lsSinceFlag, "since", "", "show missions created on or after this date (YYYY-MM-DD or RFC3339)")
	missionLsCmd.Flags().StringVar(&lsUntilFlag, "until", "", "show missions created on or before this date (YYYY-MM-DD or RFC3339)")
	missionCmd.AddCommand(missionLsCmd)
}

func runMissionLs(cmd *cobra.Command, args []string) error {
	missions, err := fetchMissions()
	if err != nil {
		return err
	}

	if len(missions) == 0 {
		if hasTimeFilter() {
			if lsSinceFlag != "" && lsUntilFlag != "" {
				fmt.Printf("No missions found between %s and %s.\n", lsSinceFlag, lsUntilFlag)
			} else if lsSinceFlag != "" {
				fmt.Printf("No missions found since %s.\n", lsSinceFlag)
			} else {
				fmt.Printf("No missions found until %s.\n", lsUntilFlag)
			}
		} else if lsAllFlag {
			fmt.Println("No missions.")
		} else {
			fmt.Println("No active missions.")
		}
		return nil
	}

	totalCount := len(missions)
	displayMissions := missions
	if !lsAllFlag && !hasTimeFilter() && totalCount > defaultMissionLsLimit {
		displayMissions = missions[:defaultMissionLsLimit]
	}

	cfg, _ := readConfig()

	var tbl table.Table
	if lsAllFlag {
		tbl = tableprinter.NewTable("LAST PROMPT", "ID", "STATUS", "PANE", "SESSION", "REPO")
	} else {
		tbl = tableprinter.NewTable("LAST PROMPT", "ID", "STATUS", "SESSION", "REPO")
	}
	for _, m := range displayMissions {
		status := getMissionStatus(m.ID, m.Status, m.ClaudeState)
		sessionName := resolveSessionName(m)
		repo := formatRepoDisplay(m.GitRepo, m.IsAdjutant, cfg)

		if lsAllFlag {
			pane := "--"
			if m.TmuxPane != nil {
				pane = *m.TmuxPane
			}
			tbl.AddRow(
				formatLastPrompt(m.LastUserPromptAt, m.CreatedAt),
				m.ShortID,
				colorizeStatus(status),
				pane,
				truncatePrompt(sessionName, defaultPromptMaxLen),
				repo,
			)
		} else {
			tbl.AddRow(
				formatLastPrompt(m.LastUserPromptAt, m.CreatedAt),
				m.ShortID,
				colorizeStatus(status),
				truncatePrompt(sessionName, defaultPromptMaxLen),
				repo,
			)
		}
	}
	if hasTimeFilter() {
		fmt.Println(formatTimeFilterMessage(len(displayMissions), lsSinceFlag, lsUntilFlag))
	}
	tbl.Print()

	if !lsAllFlag && !hasTimeFilter() && totalCount > defaultMissionLsLimit {
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

// plainGitRepoName returns a human-friendly repo name without ANSI codes.
// GitHub repos have their "github.com/" prefix stripped; non-GitHub repos
// are shown in full. Returns empty string for empty input.
func plainGitRepoName(gitRepo string) string {
	if gitRepo == "" {
		return ""
	}
	return strings.TrimPrefix(gitRepo, "github.com/")
}

// formatRepoDisplay returns a user-facing display string for a repo, combining
// emoji prefix (if configured), title (if configured), or colored canonical name.
// For adjutant missions, returns "🤖  Adjutant" regardless of other parameters.
// Safe to call with nil cfg (falls back to displayGitRepo with no emoji).
func formatRepoDisplay(repoName string, isAdjutant bool, cfg *config.AgencConfig) string {
	if isAdjutant {
		return "🤖  Adjutant"
	}

	displayName := displayGitRepo(repoName)
	emoji := ""
	if cfg != nil {
		if t := cfg.GetRepoTitle(repoName); t != "" {
			displayName = t
		}
		emoji = cfg.GetRepoEmoji(repoName)
	}

	if emoji != "" {
		return emoji + "  " + displayName
	}

	return displayName
}

// formatLastPrompt returns a human-readable timestamp of the user's last
// prompt for this mission. Returns "--" when no prompt has been recorded.
// The createdAt parameter is unused for display; it exists for symmetry with
// the sort-side COALESCE(last_user_prompt_at, created_at) key.
func formatLastPrompt(lastUserPromptAt *time.Time, _ time.Time) string {
	if lastUserPromptAt == nil {
		return "--"
	}
	return lastUserPromptAt.Local().Format("2006-01-02 15:04")
}

// colorizeStatus wraps a status string with ANSI color codes.
func colorizeStatus(status MissionDisplayStatus) string {
	s := string(status)
	switch status {
	case StatusIdle:
		return ansiLightBlue + s + ansiReset
	case StatusBusy:
		return ansiGreen + s + ansiReset
	case StatusWaiting:
		return ansiYellow + s + ansiReset
	case StatusRunning:
		return ansiGreen + s + ansiReset
	case StatusArchived:
		return ansiYellow + s + ansiReset
	default:
		return s
	}
}

const defaultPromptMaxLen = 106

// resolveSessionName returns the display name for a mission's active session.
// Uses server-provided ResolvedSessionTitle (custom_title > agenc_custom_title >
// auto_summary), falling back to the cached first user prompt.
func resolveSessionName(m *database.Mission) string {
	if m.ResolvedSessionTitle != "" {
		return m.ResolvedSessionTitle
	}
	return m.Prompt
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

// getMissionStatus returns the display status for a mission.
func getMissionStatus(missionID string, dbStatus string, claudeState *string) MissionDisplayStatus {
	if dbStatus == "archived" {
		return StatusArchived
	}
	if claudeState != nil {
		switch *claudeState {
		case "idle":
			return StatusIdle
		case "busy":
			return StatusBusy
		case "needs_attention":
			return StatusWaiting
		default:
			return StatusRunning
		}
	}
	// Fallback: check PID when claudeState is not available
	dirpath, dirErr := config.GetAgencDirpath()
	if dirErr == nil {
		pidFilepath := config.GetMissionPIDFilepath(dirpath, missionID)
		pid, err := server.ReadPID(pidFilepath)
		if err == nil && pid != 0 && server.IsProcessRunning(pid) {
			return StatusRunning
		}
	}
	return StatusStopped
}

// isMissionRunning returns true if the mission status indicates the wrapper is alive.
func isMissionRunning(status MissionDisplayStatus) bool {
	switch status {
	case StatusRunning, StatusIdle, StatusBusy, StatusWaiting:
		return true
	}
	return false
}

// hasTimeFilter returns true if either --since or --until was provided.
func hasTimeFilter() bool {
	return lsSinceFlag != "" || lsUntilFlag != ""
}

// parseTimeFlag parses a time flag value. Accepts RFC3339 or YYYY-MM-DD.
// For date-only input, isSince controls whether the time is set to start of
// day (true) or end of day (false), using local timezone.
func parseTimeFlag(value string, isSince bool) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}

	t, err := time.ParseInLocation("2006-01-02", value, time.Now().Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("expected YYYY-MM-DD or RFC3339 format")
	}

	if !isSince {
		t = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
	}
	return t, nil
}

// formatTimeFilterMessage returns a summary line for time-filtered results.
func formatTimeFilterMessage(count int, since, until string) string {
	noun := "missions"
	if count == 1 {
		noun = "mission"
	}
	if since != "" && until != "" {
		return fmt.Sprintf("Showing %d %s created between %s and %s", count, noun, since, until)
	}
	if since != "" {
		return fmt.Sprintf("Showing %d %s created since %s", count, noun, since)
	}
	return fmt.Sprintf("Showing %d %s created until %s", count, noun, until)
}

// fetchMissions fetches missions from the server.
func fetchMissions() ([]*database.Mission, error) {
	client, err := serverClient()
	if err != nil {
		return nil, err
	}

	req := server.ListMissionsRequest{
		IncludeArchived: lsAllFlag,
		SourceID:        lsCronFlag,
	}

	if lsSinceFlag != "" {
		t, err := parseTimeFlag(lsSinceFlag, true)
		if err != nil {
			return nil, fmt.Errorf("invalid --since value %q: %s", lsSinceFlag, err)
		}
		req.Since = &t
	}
	if lsUntilFlag != "" {
		t, err := parseTimeFlag(lsUntilFlag, false)
		if err != nil {
			return nil, fmt.Errorf("invalid --until value %q: %s", lsUntilFlag, err)
		}
		req.Until = &t
	}

	if req.Since != nil && req.Until != nil && req.Since.After(*req.Until) {
		return nil, fmt.Errorf("--since (%s) is after --until (%s)", lsSinceFlag, lsUntilFlag)
	}

	missions, err := client.ListMissions(req)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list missions")
	}
	return missions, nil
}
