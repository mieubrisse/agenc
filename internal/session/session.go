package session

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
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

// jsonlSummaryLine represents a summary line in a session JSONL file.
type jsonlSummaryLine struct {
	Type    string `json:"type"`
	Summary string `json:"summary"`
}

// FindSessionName returns the Claude Code session summary for the given mission.
// It searches the Claude config projects directory for a project directory whose
// name contains the missionID, then reads session data from sessions-index.json
// (preferred) or falls back to scanning JSONL files. Returns "" if no session
// name is found.
func FindSessionName(claudeConfigDirpath string, missionID string) string {
	projectsDirpath := filepath.Join(claudeConfigDirpath, "projects")
	entries, err := os.ReadDir(projectsDirpath)
	if err != nil {
		return ""
	}

	var projectDirpath string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.Contains(entry.Name(), missionID) {
			projectDirpath = filepath.Join(projectsDirpath, entry.Name())
			break
		}
	}
	if projectDirpath == "" {
		return ""
	}

	// Primary: try sessions-index.json
	if summary := findSummaryFromIndex(projectDirpath); summary != "" {
		return summary
	}

	// Fallback: scan JSONL files for summary entries
	return findSummaryFromJSONL(projectDirpath)
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

// findSummaryFromJSONL scans .jsonl files in the project directory for
// {"type":"summary","summary":"..."} entries. Returns the last summary found
// from the most recently modified JSONL file. Returns "" if no summaries exist.
func findSummaryFromJSONL(projectDirpath string) string {
	entries, err := os.ReadDir(projectDirpath)
	if err != nil {
		return ""
	}

	type jsonlCandidate struct {
		filepath string
		modTime  int64
	}

	var candidates []jsonlCandidate
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		candidates = append(candidates, jsonlCandidate{
			filepath: filepath.Join(projectDirpath, entry.Name()),
			modTime:  info.ModTime().UnixMilli(),
		})
	}

	if len(candidates) == 0 {
		return ""
	}

	// Find the most recently modified JSONL file
	latestIdx := 0
	for i := 1; i < len(candidates); i++ {
		if candidates[i].modTime > candidates[latestIdx].modTime {
			latestIdx = i
		}
	}

	return findLastSummaryInJSONL(candidates[latestIdx].filepath)
}

// findLastSummaryInJSONL scans a JSONL file for lines with {"type":"summary"}
// and returns the summary from the last such line. Returns "" if none found.
func findLastSummaryInJSONL(jsonlFilepath string) string {
	file, err := os.Open(jsonlFilepath)
	if err != nil {
		return ""
	}
	defer file.Close()

	var lastSummary string
	scanner := bufio.NewScanner(file)
	// Increase buffer size for potentially large JSONL lines
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		// Quick check before attempting JSON parse
		if !strings.Contains(line, `"type":"summary"`) {
			continue
		}

		var entry jsonlSummaryLine
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type == "summary" && entry.Summary != "" {
			lastSummary = entry.Summary
		}
	}

	return lastSummary
}
