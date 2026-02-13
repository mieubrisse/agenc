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
