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
	// fileWatcherInterval is how often the file watcher checks for JSONL changes.
	fileWatcherInterval = 3 * time.Second

	// titleConsumerInterval is how often the title consumer processes new content.
	titleConsumerInterval = 3 * time.Second
)

// ============================================================================
// Layer 1: File Watcher — discovers JSONL files and tracks their sizes
// ============================================================================

// runFileWatcherLoop polls for JSONL file changes and updates known_file_size
// on the sessions table. Does NOT read file content — just tracks sizes.
// Two scopes per cycle:
//   - Running missions: stats files for active tmux panes
//   - NULL file size sessions: stats files to set initial sizes (backfill trigger)
func (s *Server) runFileWatcherLoop(ctx context.Context) {
	// Initial delay to avoid racing with startup I/O
	select {
	case <-ctx.Done():
		return
	case <-time.After(fileWatcherInterval):
		s.runFileWatcherCycle()
	}

	ticker := time.NewTicker(fileWatcherInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runFileWatcherCycle()
		}
	}
}

// runFileWatcherCycle updates known_file_size for sessions that need it.
func (s *Server) runFileWatcherCycle() {
	// Scope 1: Running missions — discover and stat JSONL files
	s.watchRunningMissionFiles()

	// Scope 2: Sessions with NULL known_file_size — backfill trigger
	s.watchNullFileSizeSessions()
}

// watchRunningMissionFiles stats JSONL files for missions currently running
// in the tmux pool and updates known_file_size.
func (s *Server) watchRunningMissionFiles() {
	paneIDs := listPoolPaneIDs(s.getPoolSessionName())

	for _, paneID := range paneIDs {
		mission, err := s.db.GetMissionByTmuxPane(paneID)
		if err != nil {
			s.logger.Printf("File watcher: failed to resolve pane '%s': %v", paneID, err)
			continue
		}
		if mission == nil {
			continue
		}

		agentDirpath := config.GetMissionAgentDirpath(s.agencDirpath, mission.ID)
		projectDirpath, err := claudeconfig.ComputeProjectDirpath(agentDirpath)
		if err != nil {
			s.logger.Printf("File watcher: failed to compute project dir for mission '%s': %v", database.ShortID(mission.ID), err)
			continue
		}

		s.updateFileSizesForMission(mission.ID, projectDirpath)
	}
}

// updateFileSizesForMission stats all JSONL files in a mission's project
// directory and updates known_file_size for each session.
func (s *Server) updateFileSizesForMission(missionID string, projectDirpath string) {
	entries, err := os.ReadDir(projectDirpath)
	if err != nil {
		return // Directory may not exist yet
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		fileInfo, err := entry.Info()
		if err != nil {
			continue
		}
		fileSize := fileInfo.Size()

		// Look up or create the session row
		sess, err := s.db.GetSession(sessionID)
		if err != nil {
			s.logger.Printf("File watcher: failed to get session '%s': %v", sessionID, err)
			continue
		}
		if sess == nil {
			sess, err = s.db.CreateSession(missionID, sessionID)
			if err != nil {
				s.logger.Printf("File watcher: failed to create session '%s': %v", sessionID, err)
				continue
			}
		}

		// Update known_file_size if it changed
		if sess.KnownFileSize == nil || *sess.KnownFileSize != fileSize {
			if err := s.db.UpdateKnownFileSize(sessionID, fileSize); err != nil {
				s.logger.Printf("File watcher: failed to update file size for session '%s': %v", sessionID, err)
			}
		}
	}
}

// watchNullFileSizeSessions finds sessions with NULL known_file_size, computes
// their JSONL paths, stats the files, and sets the initial size. This is the
// backfill trigger — on first deployment, all existing sessions have NULL, so
// the file watcher progressively discovers their sizes.
func (s *Server) watchNullFileSizeSessions() {
	sessions, err := s.db.SessionsWithNullFileSize()
	if err != nil {
		s.logger.Printf("File watcher: failed to query NULL file size sessions: %v", err)
		return
	}

	for _, sess := range sessions {
		jsonlPath := s.resolveSessionJSONLPath(sess)
		if jsonlPath == "" {
			// Can't find the JSONL file — set size to 0 so we don't retry every cycle
			if err := s.db.UpdateKnownFileSize(sess.ID, 0); err != nil {
				s.logger.Printf("File watcher: failed to set file size to 0 for session '%s': %v", sess.ID, err)
			}
			continue
		}

		info, err := os.Stat(jsonlPath)
		if err != nil {
			if err := s.db.UpdateKnownFileSize(sess.ID, 0); err != nil {
				s.logger.Printf("File watcher: failed to set file size to 0 for session '%s': %v", sess.ID, err)
			}
			continue
		}

		if err := s.db.UpdateKnownFileSize(sess.ID, info.Size()); err != nil {
			s.logger.Printf("File watcher: failed to update file size for session '%s': %v", sess.ID, err)
		}
	}
}

// resolveSessionJSONLPath computes the JSONL file path for a session.
// Returns empty string if the path cannot be determined or the file doesn't exist.
func (s *Server) resolveSessionJSONLPath(sess *database.Session) string {
	agentDirpath := config.GetMissionAgentDirpath(s.agencDirpath, sess.MissionID)
	projectDirpath, err := claudeconfig.ComputeProjectDirpath(agentDirpath)
	if err != nil {
		return ""
	}
	jsonlPath := filepath.Join(projectDirpath, sess.ID+".jsonl")
	if _, err := os.Stat(jsonlPath); err != nil {
		return ""
	}
	return jsonlPath
}

// ============================================================================
// Layer 2: Title Consumer — reads new content and extracts titles/summaries
// ============================================================================

// runTitleConsumerLoop processes sessions with unscanned content, extracting
// custom titles and first user messages for auto-summary.
func (s *Server) runTitleConsumerLoop(ctx context.Context) {
	// Initial delay to let file watcher populate known_file_size first
	select {
	case <-ctx.Done():
		return
	case <-time.After(titleConsumerInterval + 1*time.Second):
		s.runTitleConsumerCycle()
	}

	ticker := time.NewTicker(titleConsumerInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runTitleConsumerCycle()
		}
	}
}

// runTitleConsumerCycle queries for sessions with new content and processes them.
func (s *Server) runTitleConsumerCycle() {
	sessions, err := s.db.SessionsNeedingTitleUpdate()
	if err != nil {
		s.logger.Printf("Title consumer: failed to query sessions: %v", err)
		return
	}

	for _, sess := range sessions {
		jsonlPath := s.resolveSessionJSONLPath(sess)
		if jsonlPath == "" {
			continue
		}

		knownSize := int64(0)
		if sess.KnownFileSize != nil {
			knownSize = *sess.KnownFileSize
		}

		needsUserMessage := sess.AutoSummary == ""

		scanResult, err := scanJSONLFromOffset(jsonlPath, sess.LastTitleUpdateOffset, needsUserMessage)
		if err != nil {
			s.logger.Printf("Title consumer: failed to scan '%s': %v", jsonlPath, err)
			continue
		}

		customTitleChanged := scanResult.customTitle != "" && scanResult.customTitle != sess.CustomTitle

		if err := s.db.UpdateSessionScanResults(sess.ID, scanResult.customTitle, knownSize); err != nil {
			s.logger.Printf("Title consumer: failed to update session '%s': %v", sess.ID, err)
			continue
		}

		if customTitleChanged {
			s.reconcileTmuxWindowTitle(sess.MissionID)
		}

		if needsUserMessage && scanResult.firstUserMessage != "" {
			s.requestSessionSummary(sess.ID, sess.MissionID, scanResult.firstUserMessage)
		}
	}
}

// ============================================================================
// JSONL parsing helpers (shared by title consumer and future consumers)
// ============================================================================

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
