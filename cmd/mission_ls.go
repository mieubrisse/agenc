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
	missions, err := fetchMissions()
	if err != nil {
		return err
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

	cfg, _ := readConfig()

	var tbl table.Table
	if lsAllFlag {
		tbl = tableprinter.NewTable("LAST ACTIVE", "ID", "STATUS", "PANE", "SESSION", "REPO")
	} else {
		tbl = tableprinter.NewTable("LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO")
	}
	for _, m := range displayMissions {
		status := getMissionStatus(m.ID, m.Status, m.ClaudeState)
		sessionName := resolveSessionName(m)
		repo := displayGitRepo(m.GitRepo)
		if m.IsAdjutant {
			repo = "🤖  Adjutant"
		} else if cfg != nil {
			if t := cfg.GetRepoTitle(m.GitRepo); t != "" {
				repo = t
			}
		}

		if lsAllFlag {
			pane := "--"
			if m.TmuxPane != nil {
				pane = *m.TmuxPane
			}
			tbl.AddRow(
				formatLastActive(m.LastHeartbeat, m.CreatedAt),
				m.ShortID,
				colorizeStatus(status),
				pane,
				truncatePrompt(sessionName, defaultPromptMaxLen),
				repo,
			)
		} else {
			tbl.AddRow(
				formatLastActive(m.LastHeartbeat, m.CreatedAt),
				m.ShortID,
				colorizeStatus(status),
				truncatePrompt(sessionName, defaultPromptMaxLen),
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

// plainGitRepoName returns a human-friendly repo name without ANSI codes.
// GitHub repos have their "github.com/" prefix stripped; non-GitHub repos
// are shown in full. Returns empty string for empty input.
func plainGitRepoName(gitRepo string) string {
	if gitRepo == "" {
		return ""
	}
	return strings.TrimPrefix(gitRepo, "github.com/")
}

// formatLastActive returns a human-readable timestamp of the mission's last
// activity. Uses the newest of: LastHeartbeat (wrapper liveness) or CreatedAt.
func formatLastActive(lastHeartbeat *time.Time, createdAt time.Time) string {
	if lastHeartbeat != nil {
		return lastHeartbeat.Local().Format("2006-01-02 15:04")
	}
	return createdAt.Local().Format("2006-01-02 15:04")
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

// fetchMissions fetches missions from the server.
func fetchMissions() ([]*database.Mission, error) {
	client, err := serverClient()
	if err != nil {
		return nil, err
	}

	missions, err := client.ListMissions(lsAllFlag, "", lsCronFlag)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list missions")
	}
	return missions, nil
}
