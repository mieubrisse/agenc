package cmd

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/rodaine/table"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/history"
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
var lsGitStatusFlag bool

var missionLsCmd = &cobra.Command{
	Use:   lsCmdStr,
	Short: "List active missions",
	RunE:  runMissionLs,
}

func init() {
	missionLsCmd.Flags().BoolVarP(&lsAllFlag, allFlagName, "a", false, "include archived missions")
	missionLsCmd.Flags().StringVar(&lsCronFlag, cronFlagName, "", "filter to missions from a specific cron job")
	missionLsCmd.Flags().BoolVar(&lsGitStatusFlag, "git-status", false, "show uncommitted changes in mission directories")
	missionCmd.AddCommand(missionLsCmd)
}

func runMissionLs(cmd *cobra.Command, args []string) error {
	if _, err := getAgencContext(); err != nil {
		return err
	}

	ensureServerRunning(agencDirpath)

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

	// Compute shadow repo HEAD once for config staleness display
	var shadowHeadCommitHash string
	if lsAllFlag {
		shadowHeadCommitHash = claudeconfig.GetShadowRepoCommitHash(agencDirpath)
	}

	var tbl table.Table
	if lsAllFlag {
		if lsGitStatusFlag {
			tbl = tableprinter.NewTable("LAST ACTIVE", "ID", "STATUS", "PANE", "CONFIG", "GIT", "SESSION", "REPO")
		} else {
			tbl = tableprinter.NewTable("LAST ACTIVE", "ID", "STATUS", "PANE", "CONFIG", "SESSION", "REPO")
		}
	} else {
		if lsGitStatusFlag {
			tbl = tableprinter.NewTable("LAST ACTIVE", "ID", "STATUS", "GIT", "SESSION", "REPO")
		} else {
			tbl = tableprinter.NewTable("LAST ACTIVE", "ID", "STATUS", "SESSION", "REPO")
		}
	}
	for _, m := range displayMissions {
		status := getMissionStatus(m.ID, m.Status, m.ClaudeState)
		sessionName := resolveSessionName(m)
		repo := displayGitRepo(m.GitRepo)
		if config.IsMissionAdjutant(agencDirpath, m.ID) {
			repo = "🤖  Adjutant"
		}

		var gitStatus string
		if lsGitStatusFlag {
			gitStatus = getMissionGitStatus(m.ID)
		}

		if lsAllFlag {
			pane := "--"
			if m.TmuxPane != nil {
				pane = *m.TmuxPane
			}
			if lsGitStatusFlag {
				tbl.AddRow(
					formatLastActive(m.LastHeartbeat, m.CreatedAt),
					m.ShortID,
					colorizeStatus(status),
					pane,
					formatConfigCommit(m.ConfigCommit, shadowHeadCommitHash),
					gitStatus,
					truncatePrompt(sessionName, defaultPromptMaxLen),
					repo,
				)
			} else {
				tbl.AddRow(
					formatLastActive(m.LastHeartbeat, m.CreatedAt),
					m.ShortID,
					colorizeStatus(status),
					pane,
					formatConfigCommit(m.ConfigCommit, shadowHeadCommitHash),
					truncatePrompt(sessionName, defaultPromptMaxLen),
					repo,
				)
			}
		} else {
			if lsGitStatusFlag {
				tbl.AddRow(
					formatLastActive(m.LastHeartbeat, m.CreatedAt),
					m.ShortID,
					colorizeStatus(status),
					gitStatus,
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
// It uses the server-provided ResolvedSessionTitle which follows the same
// priority chain as tmux window title reconciliation:
//
//	custom_title > agenc_custom_title > auto_summary
//
// Falls back to the mission's first user prompt if no session title is available.
func resolveSessionName(m *database.Mission) string {
	if m.ResolvedSessionTitle != "" {
		return m.ResolvedSessionTitle
	}
	return resolveMissionPrompt(m)
}

// resolveMissionPrompt returns the mission's first user prompt, using the
// server-cached value if available, otherwise reading from Claude's
// history.jsonl. The returned string may be empty if no prompt has been
// recorded yet.
func resolveMissionPrompt(m *database.Mission) string {
	if m.Prompt != "" {
		return m.Prompt
	}

	claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(agencDirpath, m.ID)
	historyFilepath := filepath.Join(claudeConfigDirpath, config.HistoryFilename)
	prompt := history.FindFirstPrompt(historyFilepath, m.ID)
	if prompt == "" {
		return ""
	}

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
	pidFilepath := config.GetMissionPIDFilepath(agencDirpath, missionID)
	pid, err := server.ReadPID(pidFilepath)
	if err == nil && pid != 0 && server.IsProcessRunning(pid) {
		return StatusRunning
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

// getMissionGitStatus checks for uncommitted changes in a mission's agent
// directory. Returns a status string: "clean" (no changes), "modified"
// (unstaged or staged changes), or "--" (not a git repo or error).
func getMissionGitStatus(missionID string) string {
	agentDirpath := config.GetMissionAgentDirpath(agencDirpath, missionID)
	return checkGitStatus(agentDirpath)
}

// checkGitStatus runs git status --porcelain in the given directory and returns
// a formatted status indicator. Returns "clean" for no changes, "modified" for
// uncommitted changes (staged or unstaged), or "--" for non-git directories.
func checkGitStatus(dirpath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitOperationTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = dirpath
	output, err := cmd.Output()
	if err != nil {
		return "--"
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return ansiGreen + "clean" + ansiReset
	}
	return ansiYellow + "modified" + ansiReset
}

// fetchMissions fetches missions from the server.
func fetchMissions() ([]*database.Mission, error) {
	client, err := serverClient()
	if err != nil {
		return nil, err
	}

	missions, err := client.ListMissions(lsAllFlag, lsCronFlag)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list missions")
	}
	return missions, nil
}
