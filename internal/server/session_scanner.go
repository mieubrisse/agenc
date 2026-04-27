package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/odyssey/agenc/internal/claudeconfig"
	"github.com/odyssey/agenc/internal/config"
	"github.com/odyssey/agenc/internal/database"
)

const (
	// sessionScannerInterval is how often the scanner checks for JSONL changes.
	sessionScannerInterval = 3 * time.Second
)

// runSessionScannerLoop polls for JSONL file changes every 3 seconds and
// updates the sessions table with any newly discovered custom titles.
// When a session has no auto_summary yet and a user message is found,
// it sends a summary request to the session summarizer worker.
func (s *Server) runSessionScannerLoop(ctx context.Context) {
	// Initial delay to avoid racing with startup I/O
	select {
	case <-ctx.Done():
		return
	case <-time.After(sessionScannerInterval):
		s.runSessionScannerCycle()
	}

	ticker := time.NewTicker(sessionScannerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runSessionScannerCycle()
		}
	}
}

// runSessionScannerCycle scans JSONL files for missions currently running in
// the tmux pool. For each running mission, it computes the project directory
// path directly (no glob), lists JSONL files in that directory, and
// incrementally scans for metadata changes.
func (s *Server) runSessionScannerCycle() {
	paneIDs := listPoolPaneIDs(s.getPoolSessionName())

	for _, paneID := range paneIDs {
		mission, err := s.db.GetMissionByTmuxPane(paneID)
		if err != nil {
			s.logger.Printf("Session scanner: failed to resolve pane '%s': %v", paneID, err)
			continue
		}
		if mission == nil {
			continue
		}

		agentDirpath := config.GetMissionAgentDirpath(s.agencDirpath, mission.ID)
		projectDirpath, err := claudeconfig.ComputeProjectDirpath(agentDirpath)
		if err != nil {
			s.logger.Printf("Session scanner: failed to compute project dir for mission '%s': %v", database.ShortID(mission.ID), err)
			continue
		}

		s.scanMissionJSONLFiles(mission.ID, projectDirpath)
	}
}

// scanMissionJSONLFiles scans all JSONL files in a mission's project directory
// for metadata changes (custom titles) and first user messages (for auto-summary).
func (s *Server) scanMissionJSONLFiles(missionID string, projectDirpath string) {
	entries, err := os.ReadDir(projectDirpath)
	if err != nil {
		// Directory may not exist yet (mission hasn't started a session)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		jsonlFilepath := filepath.Join(projectDirpath, entry.Name())

		fileInfo, err := entry.Info()
		if err != nil {
			continue
		}
		fileSize := fileInfo.Size()

		// Look up or create the session row
		sess, err := s.db.GetSession(sessionID)
		if err != nil {
			s.logger.Printf("Session scanner: failed to get session '%s': %v", sessionID, err)
			continue
		}
		if sess == nil {
			sess, err = s.db.CreateSession(missionID, sessionID)
			if err != nil {
				s.logger.Printf("Session scanner: failed to create session '%s': %v", sessionID, err)
				continue
			}
		}

		// Skip if no new data since last scan
		if fileSize <= sess.LastTitleUpdateOffset {
			continue
		}

		// Determine whether we need to look for user messages (for auto-summary)
		needsUserMessage := sess.AutoSummary == ""

		// Incremental scan from the last offset
		scanResult, err := scanJSONLFromOffset(jsonlFilepath, sess.LastTitleUpdateOffset, needsUserMessage)
		if err != nil {
			s.logger.Printf("Session scanner: failed to scan '%s': %v", jsonlFilepath, err)
			continue
		}

		// Track whether display-relevant data changed (for tmux reconciliation)
		customTitleChanged := scanResult.customTitle != "" && scanResult.customTitle != sess.CustomTitle

		if err := s.db.UpdateSessionScanResults(sessionID, scanResult.customTitle, fileSize); err != nil {
			s.logger.Printf("Session scanner: failed to update session '%s': %v", sessionID, err)
			continue
		}

		// Reconcile tmux window title when display-relevant data changes
		if customTitleChanged {
			s.reconcileTmuxWindowTitle(missionID)
		}

		// If we found a user message and the session needs a summary, send it
		// to the summarizer worker for async Haiku processing
		if needsUserMessage && scanResult.firstUserMessage != "" {
			s.requestSessionSummary(sessionID, missionID, scanResult.firstUserMessage)
		}
	}
}

// jsonlScanResult holds the results of scanning a JSONL file.
type jsonlScanResult struct {
	customTitle      string
	firstUserMessage string
}

// jsonlMetadataEntry represents a metadata line in a session JSONL file.
type jsonlMetadataEntry struct {
	Type        string `json:"type"`
	CustomTitle string `json:"customTitle"`
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

// maxMetadataLineLen is the maximum line length we bother inspecting for
// metadata. Metadata entries (custom-title) are well under 1 KB.
// Conversation message lines can exceed 5 MB — skip those immediately rather
// than searching for substrings in megabytes of JSON.
const maxMetadataLineLen = 10 * 1024 // 10 KB

// maxUserMessageLineLen is the maximum line length we inspect for user messages.
// User prompts are typically under 50 KB. We set a generous limit to catch
// longer prompts while still skipping multi-MB assistant response lines.
const maxUserMessageLineLen = 100 * 1024 // 100 KB

// tryExtractCustomTitle checks whether line contains a custom-title metadata entry
// and returns the title if found.
func tryExtractCustomTitle(line string) string {
	if len(line) > maxMetadataLineLen || !strings.Contains(line, `"custom-title"`) {
		return ""
	}
	var entry jsonlMetadataEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return ""
	}
	if entry.Type == "custom-title" && entry.CustomTitle != "" {
		return entry.CustomTitle
	}
	return ""
}

// tryExtractUserMessage checks whether line contains a user message entry
// and returns the message content if found.
func tryExtractUserMessage(line string) string {
	if len(line) > maxUserMessageLineLen || !strings.Contains(line, `"type":"user"`) {
		return ""
	}
	var entry jsonlUserEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil || entry.Type != "user" {
		return ""
	}
	var msg jsonlUserMessage
	if err := json.Unmarshal(entry.Message, &msg); err != nil || msg.Content == "" {
		return ""
	}
	return msg.Content
}

// scanJSONLFromOffset reads a JSONL file starting at the given byte offset and
// returns any custom-title values found in the new data, plus optionally the
// first user message if extractUserMessage is true.
// Uses bufio.Reader (not Scanner) to handle arbitrarily long lines without
// aborting, and quick string matching before JSON parsing to avoid parsing
// every line.
func scanJSONLFromOffset(jsonlFilepath string, offset int64, extractUserMessage bool) (jsonlScanResult, error) {
	var result jsonlScanResult

	file, err := os.Open(jsonlFilepath)
	if err != nil {
		return result, err
	}
	defer file.Close()

	if offset > 0 {
		if _, err := file.Seek(offset, 0); err != nil {
			return result, err
		}
	}

	reader := bufio.NewReaderSize(file, 64*1024) // 64 KB read buffer
	foundUserMessage := false

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			if title := tryExtractCustomTitle(line); title != "" {
				result.customTitle = title
			}

			if extractUserMessage && !foundUserMessage {
				if msg := tryExtractUserMessage(line); msg != "" {
					result.firstUserMessage = msg
					foundUserMessage = true
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return result, err
		}
	}

	return result, nil
}
