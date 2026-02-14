package session

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"sync"
)

// scannerBufPool is a pool of 1MB buffers for reuse across JSONL scans.
var scannerBufPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 1024*1024)
		return &buf
	},
}

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
	file, err := os.Open(jsonlFilepath)
	if err != nil {
		return nil
	}
	defer file.Close()

	// Get a buffer from the pool
	bufPtr := scannerBufPool.Get().(*[]byte)
	defer scannerBufPool.Put(bufPtr)

	var messages []string
	scanner := bufio.NewScanner(file)
	scanner.Buffer(*bufPtr, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, `"type":"user"`) {
			continue
		}

		var entry jsonlUserEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entry.Type != "user" {
			continue
		}

		var msg jsonlUserMessage
		if err := json.Unmarshal(entry.Message, &msg); err != nil {
			continue
		}
		if msg.Content != "" {
			messages = append(messages, msg.Content)
		}
	}

	// Return only the last maxMessages
	if len(messages) > maxMessages {
		messages = messages[len(messages)-maxMessages:]
	}
	return messages
}
