package history

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// historyEntry represents a single line in Claude's history.jsonl file.
type historyEntry struct {
	Display string `json:"display"`
	Project string `json:"project"`
}

// FindFirstPrompt scans historyFilepath for the first line whose project path
// contains the given missionID. Returns the display text of that entry, or ""
// if no match is found or the file cannot be read.
func FindFirstPrompt(historyFilepath string, missionID string) string {
	file, err := os.Open(historyFilepath)
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, missionID) {
			continue
		}

		var entry historyEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}

		// Verify the UUID appears in the project path, not just in the display text
		if strings.Contains(entry.Project, missionID) {
			return entry.Display
		}
	}

	return ""
}
