package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// errStopScanningForConversation is a sentinel returned from hasConversationData's
// ScanJSONLLines callback to stop iteration as soon as the first user or
// assistant record is seen. Callers of ScanJSONLLines identify it via errors.Is.
var errStopScanningForConversation = errors.New("found conversation record")

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
func ListSessionIDs(projectDirpath string) []string {
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
	found := false
	err := ScanJSONLLines(jsonlFilepath, func(line []byte) error {
		var record struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(line, &record); err != nil {
			return nil
		}
		if record.Type == "user" || record.Type == "assistant" {
			found = true
			return errStopScanningForConversation
		}
		return nil
	})
	if err != nil && !errors.Is(err, errStopScanningForConversation) {
		return false
	}
	return found
}

// TailJSONLFile reads the last N lines from a JSONL file and writes them to
// the given writer. If n <= 0, writes the entire file. Returns the number
// of lines written.
func TailJSONLFile(jsonlFilepath string, n int, w io.Writer) (int, error) {
	if n <= 0 {
		count := 0
		err := ScanJSONLLines(jsonlFilepath, func(line []byte) error {
			fmt.Fprintln(w, string(line))
			count++
			return nil
		})
		return count, err
	}

	ring := make([]string, n)
	total := 0
	err := ScanJSONLLines(jsonlFilepath, func(line []byte) error {
		ring[total%n] = string(line)
		total++
		return nil
	})
	if err != nil {
		return 0, err
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
// JSONL conversation log in the given project directory, or "" if none exists.
// This is used by the idle timeout system to check whether Claude is actively working.
func FindActiveJSONLPath(projectDirpath string) string {
	return findMostRecentJSONL(projectDirpath)
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
