package session

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sessionsIndex represents the top-level structure of sessions-index.json.
type sessionsIndex struct {
	Entries []sessionsIndexEntry `json:"entries"`
}

// sessionsIndexEntry represents a single entry in sessions-index.json.
type sessionsIndexEntry struct {
	SessionID string `json:"sessionId"`
	Summary   string `json:"summary"`
	Modified  string `json:"modified"`
}

// jsonlMetadataLine represents a metadata line in a session JSONL file.
// Covers both {"type":"summary"} and {"type":"custom-title"} entries.
type jsonlMetadataLine struct {
	Type        string `json:"type"`
	Summary     string `json:"summary"`
	CustomTitle string `json:"customTitle"`
}

// FindSessionName returns the Claude Code session name for the given mission.
// It searches the Claude config projects directory for a project directory whose
// name contains the missionID, then reads session data using the following
// priority order:
//  1. Custom title from JSONL (set via /rename — always wins)
//  2. Summary from sessions-index.json
//  3. Summary from JSONL files
//
// Returns "" if no session name is found.
func FindSessionName(claudeConfigDirpath string, missionID string) string {
	projectDirpath := findProjectDirpath(claudeConfigDirpath, missionID)
	if projectDirpath == "" {
		return ""
	}

	// Always scan JSONL first — a custom-title (from /rename) takes priority
	// over any auto-generated summary.
	customTitle, jsonlSummary := findNamesFromJSONL(projectDirpath)
	if customTitle != "" {
		return customTitle
	}

	// Try sessions-index.json for an auto-generated summary
	if indexSummary := findSummaryFromIndex(projectDirpath); indexSummary != "" {
		return indexSummary
	}

	// Fall back to the auto-generated summary found in JSONL
	return jsonlSummary
}

// FindCustomTitle returns the custom title set via Claude's /rename command,
// or "" if no custom title exists.
func FindCustomTitle(claudeConfigDirpath string, missionID string) string {
	projectDirpath := findProjectDirpath(claudeConfigDirpath, missionID)
	if projectDirpath == "" {
		return ""
	}
	customTitle, _ := findNamesFromJSONL(projectDirpath)
	return customTitle
}

// findProjectDirpath locates the Claude Code project directory for the given
// mission within the claude config directory. Returns "" if not found.
func findProjectDirpath(claudeConfigDirpath string, missionID string) string {
	projectsDirpath := filepath.Join(claudeConfigDirpath, "projects")
	entries, err := os.ReadDir(projectsDirpath)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.Contains(entry.Name(), missionID) {
			return filepath.Join(projectsDirpath, entry.Name())
		}
	}
	return ""
}

// findSummaryFromIndex reads sessions-index.json and returns the summary from
// the entry with the latest modified timestamp. Returns "" if the file doesn't
// exist, can't be parsed, or has no entries.
func findSummaryFromIndex(projectDirpath string) string {
	indexFilepath := filepath.Join(projectDirpath, "sessions-index.json")
	data, err := os.ReadFile(indexFilepath)
	if err != nil {
		return ""
	}

	var index sessionsIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return ""
	}

	if len(index.Entries) == 0 {
		return ""
	}

	// Find the entry with the latest modified timestamp.
	// The modified field is an ISO 8601 string, so lexicographic comparison works.
	latestIdx := 0
	for i := 1; i < len(index.Entries); i++ {
		if index.Entries[i].Modified > index.Entries[latestIdx].Modified {
			latestIdx = i
		}
	}

	return index.Entries[latestIdx].Summary
}

// findNamesFromJSONL scans the most recently modified .jsonl file in the
// project directory for custom-title and summary entries. Returns
// (customTitle, summary). Either or both may be empty.
func findNamesFromJSONL(projectDirpath string) (customTitle string, summary string) {
	jsonlFilepath := findMostRecentJSONL(projectDirpath)
	if jsonlFilepath == "" {
		return "", ""
	}
	return findNamesInJSONL(jsonlFilepath)
}

// findMostRecentJSONL returns the path of the most recently modified .jsonl
// file in the given directory, or "" if none exist.
func findMostRecentJSONL(projectDirpath string) string {
	entries, err := os.ReadDir(projectDirpath)
	if err != nil {
		return ""
	}

	var latestFilepath string
	var latestModTime int64

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().UnixMilli() > latestModTime {
			latestModTime = info.ModTime().UnixMilli()
			latestFilepath = filepath.Join(projectDirpath, entry.Name())
		}
	}

	return latestFilepath
}

// findNamesInJSONL scans a JSONL file for custom-title and summary entries.
// Returns the last custom-title and the last summary found, either of which
// may be empty.
func findNamesInJSONL(jsonlFilepath string) (customTitle string, summary string) {
	file, err := os.Open(jsonlFilepath)
	if err != nil {
		return "", ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	// Increase buffer size for potentially large JSONL lines
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()

		// Quick check: skip lines that can't contain either entry type
		hasSummary := strings.Contains(line, `"type":"summary"`)
		hasCustomTitle := strings.Contains(line, `"type":"custom-title"`)
		if !hasSummary && !hasCustomTitle {
			continue
		}

		var entry jsonlMetadataLine
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		switch entry.Type {
		case "summary":
			if entry.Summary != "" {
				summary = entry.Summary
			}
		case "custom-title":
			if entry.CustomTitle != "" {
				customTitle = entry.CustomTitle
			}
		}
	}

	return customTitle, summary
}

// FindSessionJSONLPath locates the JSONL transcript file for a given session UUID.
// It searches all project directories under ~/.claude/projects/ for a file named
// <sessionID>.jsonl. Returns the full path or an error if not found.
func FindSessionJSONLPath(sessionID string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	projectsDirpath := filepath.Join(homeDir, ".claude", "projects")
	entries, err := os.ReadDir(projectsDirpath)
	if err != nil {
		return "", fmt.Errorf("failed to read projects directory '%s': %w", projectsDirpath, err)
	}

	targetFilename := sessionID + ".jsonl"
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidateFilepath := filepath.Join(projectsDirpath, entry.Name(), targetFilename)
		if _, err := os.Stat(candidateFilepath); err == nil {
			return candidateFilepath, nil
		}
	}

	return "", fmt.Errorf("session transcript not found for session ID: %s", sessionID)
}

// ListSessionIDs returns all session UUIDs for a given mission by scanning
// the mission's project directory for .jsonl files. Returns session IDs
// (filenames without the .jsonl extension) sorted by modification time
// (most recent first). Returns an empty slice if no sessions are found.
func ListSessionIDs(claudeConfigDirpath string, missionID string) []string {
	projectDirpath := findProjectDirpath(claudeConfigDirpath, missionID)
	if projectDirpath == "" {
		return nil
	}

	entries, err := os.ReadDir(projectDirpath)
	if err != nil {
		return nil
	}

	type sessionEntry struct {
		id      string
		modTime int64
	}
	var sessions []sessionEntry

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")

		// Skip session files that don't contain actual conversation data.
		// A session file may exist with only metadata records (e.g.,
		// file-history-snapshot) if the wrapper was killed before any
		// conversation started. Claude rejects these with "No conversation
		// found" when passed to claude -r.
		if !hasConversationData(filepath.Join(projectDirpath, entry.Name())) {
			continue
		}

		sessions = append(sessions, sessionEntry{
			id:      sessionID,
			modTime: info.ModTime().UnixMilli(),
		})
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].modTime > sessions[j].modTime
	})

	result := make([]string, len(sessions))
	for i, s := range sessions {
		result[i] = s.id
	}
	return result
}

// hasConversationData checks whether a session JSONL file contains at least one
// user or assistant message record. Files that only contain metadata records
// (like file-history-snapshot) are not valid conversations.
func hasConversationData(jsonlFilepath string) bool {
	file, err := os.Open(jsonlFilepath)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 256*1024), 1024*1024)

	for scanner.Scan() {
		var record struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
			continue
		}
		if record.Type == "user" || record.Type == "assistant" {
			return true
		}
	}
	return false
}

// TailJSONLFile reads the last N lines from a JSONL file and writes them to
// the given writer. If n <= 0, writes the entire file. Returns the number
// of lines written.
func TailJSONLFile(jsonlFilepath string, n int, w io.Writer) (int, error) {
	file, err := os.Open(jsonlFilepath)
	if err != nil {
		return 0, fmt.Errorf("failed to open session file '%s': %w", jsonlFilepath, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	if n <= 0 {
		count := 0
		for scanner.Scan() {
			fmt.Fprintln(w, scanner.Text())
			count++
		}
		return count, scanner.Err()
	}

	ring := make([]string, n)
	total := 0
	for scanner.Scan() {
		ring[total%n] = scanner.Text()
		total++
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("error reading session file: %w", err)
	}

	count := total
	if count > n {
		count = n
	}
	startIdx := total - count
	for i := 0; i < count; i++ {
		fmt.Fprintln(w, ring[(startIdx+i)%n])
	}
	return count, nil
}

// FindActiveJSONLPath returns the filesystem path of the most recently modified
// JSONL conversation log for the given mission, or "" if none exists. This is
// used by the idle timeout system to check whether Claude is actively working.
func FindActiveJSONLPath(claudeConfigDirpath string, missionID string) string {
	projectDirpath := findProjectDirpath(claudeConfigDirpath, missionID)
	if projectDirpath == "" {
		return ""
	}
	return findMostRecentJSONL(projectDirpath)
}
