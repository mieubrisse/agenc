package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/database"
)

const (
	// searchIndexerInterval is how often the FTS indexer checks for unindexed content.
	searchIndexerInterval = 30 * time.Second
)

// runSearchIndexerLoop processes sessions with unindexed content, extracting
// user messages and assistant text blocks and inserting them into the FTS5
// search index. Runs every 30 seconds.
func (s *Server) runSearchIndexerLoop(ctx context.Context) {
	// Initial delay to let file watcher populate known_file_size first
	select {
	case <-ctx.Done():
		return
	case <-time.After(searchIndexerInterval):
		s.runSearchIndexerCycle()
	}

	ticker := time.NewTicker(searchIndexerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runSearchIndexerCycle()
		}
	}
}

// runSearchIndexerCycle queries for sessions with new content and indexes them.
func (s *Server) runSearchIndexerCycle() {
	sessions, err := s.db.SessionsNeedingIndexing()
	if err != nil {
		s.logger.Printf("Search indexer: failed to query sessions: %v", err)
		return
	}

	for _, sess := range sessions {
		s.indexSession(sess)
	}
}

// indexSession reads unindexed JSONL content for a session, extracts searchable
// text, and inserts it into the FTS5 index atomically with the offset update.
func (s *Server) indexSession(sess *database.Session) {
	jsonlPath := s.resolveSessionJSONLPath(sess)
	if jsonlPath == "" {
		return
	}

	knownSize := int64(0)
	if sess.KnownFileSize != nil {
		knownSize = *sess.KnownFileSize
	}

	content, err := extractIndexableContent(jsonlPath, sess.LastIndexedOffset, knownSize)
	if err != nil {
		s.logger.Printf("Search indexer: failed to extract content from '%s': %v", database.ShortID(sess.ID), err)
		return
	}

	if content == "" {
		// No indexable content found, but still advance the offset
		if err := s.db.InsertSearchContentAndUpdateOffset(sess.MissionID, sess.ID, "", knownSize); err != nil {
			// Empty content insert may fail on FTS5 — just update the offset directly
			if updateErr := s.db.UpdateLastIndexedOffset(sess.ID, knownSize); updateErr != nil {
				s.logger.Printf("Search indexer: failed to update offset for '%s': %v", database.ShortID(sess.ID), updateErr)
			}
		}
		return
	}

	if err := s.db.InsertSearchContentAndUpdateOffset(sess.MissionID, sess.ID, content, knownSize); err != nil {
		s.logger.Printf("Search indexer: failed to index session '%s': %v", database.ShortID(sess.ID), err)
	}
}

// extractIndexableContent reads a JSONL file from startOffset to endOffset and
// extracts searchable text (user messages + assistant text blocks).
func extractIndexableContent(jsonlPath string, startOffset, endOffset int64) (string, error) {
	file, err := os.Open(jsonlPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if startOffset > 0 {
		if _, err := file.Seek(startOffset, 0); err != nil {
			return "", err
		}
	}

	reader := bufio.NewReaderSize(file, 64*1024)
	var texts []string
	bytesRead := startOffset

	for bytesRead < endOffset {
		line, err := reader.ReadString('\n')
		bytesRead += int64(len(line))
		if len(line) > 0 {
			if text := tryExtractIndexableText(line); text != "" {
				texts = append(texts, text)
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
	}

	return strings.Join(texts, "\n"), nil
}

// maxIndexableLineLen is the maximum line length we inspect for indexable content.
const maxIndexableLineLen = 500 * 1024 // 500 KB

// tryExtractIndexableText extracts searchable text from a JSONL line.
// Returns user message text or assistant prose text (skipping tool_use and thinking blocks).
func tryExtractIndexableText(line string) string {
	if len(line) > maxIndexableLineLen {
		return ""
	}

	if strings.Contains(line, `"type":"user"`) {
		return tryExtractUserMessage(line)
	}

	if strings.Contains(line, `"type":"assistant"`) {
		return tryExtractAssistantText(line)
	}

	return ""
}

// jsonlAssistantEntry represents an assistant message entry for text extraction.
type jsonlAssistantEntry struct {
	Type    string          `json:"type"`
	Message json.RawMessage `json:"message"`
}

// jsonlAssistantMessage represents the message portion of an assistant entry.
type jsonlAssistantMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// jsonlContentBlock represents a single content block in a message.
type jsonlContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// tryExtractAssistantText extracts text blocks from an assistant message,
// skipping tool_use and thinking blocks.
func tryExtractAssistantText(line string) string {
	var entry jsonlAssistantEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Type != "assistant" {
		return ""
	}
	var msg jsonlAssistantMessage
	if err := json.Unmarshal(entry.Message, &msg); err != nil {
		return ""
	}
	var blocks []jsonlContentBlock
	if err := json.Unmarshal(msg.Content, &blocks); err != nil {
		return ""
	}
	var texts []string
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			texts = append(texts, b.Text)
		}
	}
	return strings.Join(texts, "\n")
}
