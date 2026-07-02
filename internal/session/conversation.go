package session

import (
	"bytes"
	"encoding/json"
)

// jsonlUserEntry represents a user message entry in a session JSONL file.
type jsonlUserEntry struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

// jsonlUserMessage represents the message portion of a user entry.
type jsonlUserMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ExtractRecentUserMessages returns the last maxMessages user message contents
// from the most recent JSONL session file for the given mission. Returns nil if
// no messages are found.
func ExtractRecentUserMessages(claudeConfigDirpath string, missionID string, maxMessages int) []string {
	projectDirpath := findProjectDirpath(claudeConfigDirpath, missionID)
	if projectDirpath == "" {
		return nil
	}

	jsonlFilepath := findMostRecentJSONL(projectDirpath)
	if jsonlFilepath == "" {
		return nil
	}

	return extractUserMessagesFromJSONL(jsonlFilepath, maxMessages)
}

// extractUserMessagesFromJSONL reads a JSONL file and returns the last
// maxMessages user message contents.
func extractUserMessagesFromJSONL(jsonlFilepath string, maxMessages int) []string {
	var messages []string
	_ = ScanJSONLLines(jsonlFilepath, func(line []byte) error {
		if !bytes.Contains(line, []byte(`"type":"user"`)) {
			return nil
		}
		var entry jsonlUserEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			return nil
		}
		if entry.Type != "user" {
			return nil
		}
		var msg jsonlUserMessage
		if err := json.Unmarshal(entry.Message, &msg); err != nil {
			return nil
		}
		if msg.Content != "" {
			messages = append(messages, msg.Content)
		}
		return nil
	})

	if len(messages) > maxMessages {
		messages = messages[len(messages)-maxMessages:]
	}
	return messages
}
