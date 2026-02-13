package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
	"github.com/spf13/cobra"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

var summaryDateFlag string

var summaryCmd = &cobra.Command{
	Use:   summaryCmdStr,
	Short: "Show a daily summary of AgenC activity",
	Long: `Display a summary of your day with AgenC, including:
  - Missions created
  - Claude sessions created
  - Git commits across all repos
  - Hours of active development
  - Claude usage statistics (tokens, messages)

By default shows statistics for the current calendar day (3am-3am).`,
	RunE: runSummary,
}

func init() {
	summaryCmd.Flags().StringVarP(&summaryDateFlag, dateFlagName, "d", "", "date to summarize (YYYY-MM-DD format, defaults to today)")
	rootCmd.AddCommand(summaryCmd)
}

func runSummary(cmd *cobra.Command, args []string) error {
	db, err := openDB()
	if err != nil {
		return err
	}
	defer db.Close()

	// Determine the date range (calendar day from 3am to 3am)
	var targetDate time.Time
	if summaryDateFlag != "" {
		targetDate, err = time.Parse("2006-01-02", summaryDateFlag)
		if err != nil {
			return stacktrace.Propagate(err, "invalid date format; use YYYY-MM-DD")
		}
	} else {
		targetDate = time.Now()
	}

	dayStart, dayEnd := calculateDayBounds(targetDate)

	// Gather statistics
	stats, err := gatherDailyStats(db, dayStart, dayEnd)
	if err != nil {
		return stacktrace.Propagate(err, "failed to gather daily statistics")
	}

	// Display the summary
	printDailySummary(targetDate, stats)

	return nil
}

// calculateDayBounds returns the start and end times for a "calendar day"
// defined as 3am to 3am the next day.
func calculateDayBounds(date time.Time) (time.Time, time.Time) {
	// Set to 3am on the given date
	dayStart := time.Date(date.Year(), date.Month(), date.Day(), 3, 0, 0, 0, date.Location())

	// If we're before 3am, we're actually in the previous day
	if date.Hour() < 3 {
		dayStart = dayStart.Add(-24 * time.Hour)
	}

	dayEnd := dayStart.Add(24 * time.Hour)
	return dayStart, dayEnd
}

// DailyStats holds all the statistics for a day.
type DailyStats struct {
	MissionsCreated     int
	SessionsCreated     int
	TotalCommits        int
	CommitsByRepo       map[string]int
	FirstActivity       *time.Time
	LastActivity        *time.Time
	TotalMessages       int
	TokensConsumed      int
	SessionStats        []SessionStat
	PeakHour            int
	PeakHourSessions    int
}

// SessionStat holds statistics for a single Claude session.
type SessionStat struct {
	MissionID    string
	SessionName  string
	Repo         string
	Messages     int
	Tokens       int
	CreatedAt    time.Time
}

// gatherDailyStats collects all statistics for the given day.
func gatherDailyStats(db *database.DB, dayStart, dayEnd time.Time) (*DailyStats, error) {
	stats := &DailyStats{
		CommitsByRepo: make(map[string]int),
	}

	// Get all missions (including archived) to analyze
	missions, err := db.ListMissions(database.ListMissionsParams{IncludeArchived: true})
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list missions")
	}

	// Count missions created during this day
	for _, m := range missions {
		if m.CreatedAt.After(dayStart) && m.CreatedAt.Before(dayEnd) {
			stats.MissionsCreated++
		}

		// Track first and last activity
		if m.CreatedAt.After(dayStart) && m.CreatedAt.Before(dayEnd) {
			if stats.FirstActivity == nil || m.CreatedAt.Before(*stats.FirstActivity) {
				stats.FirstActivity = &m.CreatedAt
			}
		}
		if m.LastActive != nil && m.LastActive.After(dayStart) && m.LastActive.Before(dayEnd) {
			if stats.LastActivity == nil || m.LastActive.After(*stats.LastActivity) {
				stats.LastActivity = m.LastActive
			}
		}
	}

	// Count sessions and gather session statistics
	sessionStats, err := gatherSessionStats(missions, dayStart, dayEnd)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to gather session statistics")
	}
	stats.SessionsCreated = len(sessionStats)
	stats.SessionStats = sessionStats

	// Calculate totals and peak hour
	hourCounts := make(map[int]int)
	for _, session := range sessionStats {
		stats.TotalMessages += session.Messages
		stats.TokensConsumed += session.Tokens
		hour := session.CreatedAt.Hour()
		hourCounts[hour]++
	}

	// Find peak hour
	for hour, count := range hourCounts {
		if count > stats.PeakHourSessions {
			stats.PeakHour = hour
			stats.PeakHourSessions = count
		}
	}

	// Count git commits across all repos
	commitStats, err := gatherCommitStats(dayStart, dayEnd)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to gather commit statistics")
	}
	stats.TotalCommits = commitStats.TotalCommits
	stats.CommitsByRepo = commitStats.CommitsByRepo

	return stats, nil
}

// gatherSessionStats extracts statistics from Claude session JSONL files.
func gatherSessionStats(missions []*database.Mission, dayStart, dayEnd time.Time) ([]SessionStat, error) {
	var sessionStats []SessionStat

	for _, m := range missions {
		// Only process sessions created during this day
		if !m.CreatedAt.After(dayStart) || !m.CreatedAt.Before(dayEnd) {
			continue
		}

		claudeConfigDirpath := claudeconfig.GetMissionClaudeConfigDirpath(agencDirpath, m.ID)
		sessionStat, err := extractSessionStat(claudeConfigDirpath, m)
		if err != nil {
			// Silently skip sessions we can't read
			continue
		}

		if sessionStat != nil {
			sessionStats = append(sessionStats, *sessionStat)
		}
	}

	return sessionStats, nil
}

// extractSessionStat extracts statistics from a single mission's Claude session.
func extractSessionStat(claudeConfigDirpath string, m *database.Mission) (*SessionStat, error) {
	projectsDir := filepath.Join(claudeConfigDirpath, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, err
	}

	// Find the project directory for this mission
	var projectDir string
	for _, entry := range entries {
		if entry.IsDir() && strings.Contains(entry.Name(), m.ID) {
			projectDir = filepath.Join(projectsDir, entry.Name())
			break
		}
	}
	if projectDir == "" {
		return nil, nil
	}

	// Find and read JSONL files
	jsonlFiles, err := filepath.Glob(filepath.Join(projectDir, "*.jsonl"))
	if err != nil || len(jsonlFiles) == 0 {
		return nil, nil
	}

	// Use the most recent JSONL file
	var newestFile string
	var newestTime time.Time
	for _, f := range jsonlFiles {
		info, err := os.Stat(f)
		if err != nil {
			continue
		}
		if newestFile == "" || info.ModTime().After(newestTime) {
			newestFile = f
			newestTime = info.ModTime()
		}
	}

	if newestFile == "" {
		return nil, nil
	}

	// Count messages and estimate tokens from JSONL
	messages, tokens, err := analyzeJSONL(newestFile)
	if err != nil {
		return nil, err
	}

	stat := &SessionStat{
		MissionID:   m.ShortID,
		SessionName: m.Prompt,
		Repo:        m.GitRepo,
		Messages:    messages,
		Tokens:      tokens,
		CreatedAt:   m.CreatedAt,
	}

	return stat, nil
}

// analyzeJSONL counts messages and estimates token usage from a Claude session JSONL file.
func analyzeJSONL(jsonlPath string) (int, int, error) {
	file, err := os.Open(jsonlPath)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line size

	messageCount := 0
	totalTokens := 0

	for scanner.Scan() {
		line := scanner.Bytes()

		// Parse the JSON line to extract type and token information
		var entry map[string]interface{}
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		// Count messages by type field (user/assistant)
		entryType, ok := entry["type"].(string)
		if !ok {
			continue
		}

		if entryType == "user" || entryType == "assistant" {
			messageCount++
		}

		// Extract token usage from assistant messages
		if entryType == "assistant" {
			if msg, ok := entry["message"].(map[string]interface{}); ok {
				if usage, ok := msg["usage"].(map[string]interface{}); ok {
					// Add input tokens (includes cache reads)
					if inputTokens, ok := usage["input_tokens"].(float64); ok {
						totalTokens += int(inputTokens)
					}
					// Add output tokens
					if outputTokens, ok := usage["output_tokens"].(float64); ok {
						totalTokens += int(outputTokens)
					}
					// Note: We don't separately add cache_creation_input_tokens or cache_read_input_tokens
					// as they're already included in the input_tokens count
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, 0, err
	}

	return messageCount, totalTokens, nil
}

// CommitStats holds git commit statistics.
type CommitStats struct {
	TotalCommits  int
	CommitsByRepo map[string]int
}

// gatherCommitStats collects git commit statistics across all repos.
func gatherCommitStats(dayStart, dayEnd time.Time) (*CommitStats, error) {
	stats := &CommitStats{
		CommitsByRepo: make(map[string]int),
	}

	reposDirpath := config.GetReposDirpath(agencDirpath)
	entries, err := os.ReadDir(reposDirpath)
	if err != nil {
		// If repos directory doesn't exist, return empty stats
		if os.IsNotExist(err) {
			return stats, nil
		}
		return nil, stacktrace.Propagate(err, "failed to read repos directory")
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		repoPath := filepath.Join(reposDirpath, entry.Name())
		if !isGitRepo(repoPath) {
			continue
		}

		commitCount, err := countCommitsInRange(repoPath, dayStart, dayEnd)
		if err != nil {
			// Skip repos we can't analyze
			continue
		}

		if commitCount > 0 {
			stats.CommitsByRepo[entry.Name()] = commitCount
			stats.TotalCommits += commitCount
		}
	}

	return stats, nil
}

// countCommitsInRange counts commits in a git repo within the given time range.
func countCommitsInRange(repoPath string, dayStart, dayEnd time.Time) (int, error) {
	// Use git log with --since and --until to count commits
	sinceStr := dayStart.Format("2006-01-02T15:04:05")
	untilStr := dayEnd.Format("2006-01-02T15:04:05")

	cmd := exec.Command("git", "log", "--all", "--oneline",
		"--since="+sinceStr, "--until="+untilStr)
	cmd.Dir = repoPath

	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	// Count lines in output
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return 0, nil
	}
	return len(lines), nil
}


// printDailySummary displays the formatted daily summary.
func printDailySummary(date time.Time, stats *DailyStats) {
	fmt.Println()
	fmt.Println("  ---")
	fmt.Printf("  Your Day with AgenC: %s\n", date.Format("January 2, 2006"))
	fmt.Println()
	fmt.Println("  📊 By The Numbers")
	fmt.Println()

	// Calculate active hours
	var activeHours string
	if stats.FirstActivity != nil && stats.LastActivity != nil {
		duration := stats.LastActivity.Sub(*stats.FirstActivity)
		hours := int(duration.Hours())
		activeHours = fmt.Sprintf("~%d hours of active development (%s to %s)",
			hours,
			stats.FirstActivity.Format("3:04 PM"),
			stats.LastActivity.Format("3:04 PM"))
	} else {
		activeHours = "No activity recorded"
	}

	// Show basic statistics
	if stats.TotalCommits > 0 {
		// Find primary repo (most commits)
		var primaryRepo string
		maxCommits := 0
		for repo, count := range stats.CommitsByRepo {
			if count > maxCommits {
				primaryRepo = repo
				maxCommits = count
			}
		}
		fmt.Printf("  - %d commits pushed", stats.TotalCommits)
		if primaryRepo != "" && len(stats.CommitsByRepo) == 1 {
			fmt.Printf(" to the %s repository\n", primaryRepo)
		} else if primaryRepo != "" {
			fmt.Printf(" across %d repositories\n", len(stats.CommitsByRepo))
		} else {
			fmt.Println()
		}
	}

	if stats.SessionsCreated > 0 {
		peakHourDisplay := ""
		if stats.PeakHourSessions > 0 {
			hour := stats.PeakHour
			period := "AM"
			if hour >= 12 {
				period = "PM"
				if hour > 12 {
					hour = hour - 12
				}
			}
			if hour == 0 {
				hour = 12
			}
			peakHourDisplay = fmt.Sprintf(" (peak hour: %d %s with %d sessions)", hour, period, stats.PeakHourSessions)
		}
		fmt.Printf("  - %d Claude sessions created%s\n", stats.SessionsCreated, peakHourDisplay)
	}

	if stats.MissionsCreated > 0 {
		fmt.Printf("  - %d missions created\n", stats.MissionsCreated)
	}

	if activeHours != "" {
		fmt.Printf("  - %s\n", activeHours)
	}

	// Claude usage statistics
	if stats.TotalMessages > 0 || stats.TokensConsumed > 0 {
		fmt.Println()
		fmt.Println("  💬 Claude Usage")
		fmt.Println()
		if stats.TotalMessages > 0 {
			fmt.Printf("  - %d messages exchanged\n", stats.TotalMessages)
		}
		if stats.TokensConsumed > 0 {
			fmt.Printf("  - %d tokens consumed\n", stats.TokensConsumed)
		}
	}

	// Repository breakdown
	if len(stats.CommitsByRepo) > 1 {
		fmt.Println()
		fmt.Println("  📁 Activity by Repository")
		fmt.Println()
		for repo, count := range stats.CommitsByRepo {
			fmt.Printf("  - %s: %d commits\n", repo, count)
		}
	}

	fmt.Println()
	fmt.Println("  ---")
	fmt.Println()
}
