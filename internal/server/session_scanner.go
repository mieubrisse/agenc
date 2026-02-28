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
)

const (
	// sessionScannerInterval is how often the scanner checks for JSONL changes.
	sessionScannerInterval = 3 * time.Second
)

// buildJSONLGlobPattern returns the glob pattern for discovering all JSONL session
// files across all missions.
func buildJSONLGlobPattern(agencDirpath string) string {
	return filepath.Join(
		config.GetMissionsDirpath(agencDirpath),
		"*",
		claudeconfig.MissionClaudeConfigDirname,
		"projects",
		"*",
		"*.jsonl",
	)
}

// runSessionScannerLoop polls for JSONL file changes every 3 seconds and
// updates the sessions table with any newly discovered custom titles or
// auto-summaries.
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

// runSessionScannerCycle performs a single scan pass over all JSONL files.
func (s *Server) runSessionScannerCycle() {
	matches, err := filepath.Glob(buildJSONLGlobPattern(s.agencDirpath))
	if err != nil {
		s.logger.Printf("Session scanner: glob failed: %v", err)
		return
	}

	for _, jsonlFilepath := range matches {
		sessionID, missionID, ok := extractSessionAndMissionID(s.agencDirpath, jsonlFilepath)
		if !ok {
			continue
		}

		fileInfo, err := os.Stat(jsonlFilepath)
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
		if fileSize <= sess.LastScannedOffset {
			continue
		}

		// Incremental scan from the last offset
		customTitle, autoSummary, err := scanJSONLFromOffset(jsonlFilepath, sess.LastScannedOffset)
		if err != nil {
			s.logger.Printf("Session scanner: failed to scan '%s': %v", jsonlFilepath, err)
			continue
		}

		// Track whether display-relevant data changed (for tmux reconciliation)
		customTitleChanged := customTitle != "" && customTitle != sess.CustomTitle
		autoSummaryChanged := autoSummary != "" && autoSummary != sess.AutoSummary

		if err := s.db.UpdateSessionScanResults(sessionID, customTitle, autoSummary, fileSize); err != nil {
			s.logger.Printf("Session scanner: failed to update session '%s': %v", sessionID, err)
			continue
		}

		// Reconcile tmux window title when display-relevant data changes
		if customTitleChanged || autoSummaryChanged {
			s.reconcileTmuxWindowTitle(missionID)
		}
	}
}

// extractSessionAndMissionID extracts the session UUID and mission UUID from a
// JSONL filepath. The expected path structure is:
//
//	<agencDirpath>/missions/<missionID>/claude-config/projects/<encoded-path>/<sessionID>.jsonl
//
// Returns (sessionID, missionID, true) on success, or ("", "", false) if the
// path does not match the expected structure.
func extractSessionAndMissionID(agencDirpath string, jsonlFilepath string) (sessionID string, missionID string, ok bool) {
	missionsDirpath := config.GetMissionsDirpath(agencDirpath)

	// Strip the missions directory prefix to get: <missionID>/claude-config/projects/<encoded-path>/<sessionID>.jsonl
	relPath, err := filepath.Rel(missionsDirpath, jsonlFilepath)
	if err != nil {
		return "", "", false
	}

	parts := strings.Split(relPath, string(filepath.Separator))
	// Expected: [missionID, "claude-config", "projects", encodedPath, "sessionID.jsonl"]
	// Minimum 5 parts
	if len(parts) < 5 {
		return "", "", false
	}

	missionID = parts[0]
	filename := parts[len(parts)-1]
	sessionID = strings.TrimSuffix(filename, ".jsonl")

	return sessionID, missionID, true
}

// jsonlMetadataEntry represents a metadata line in a session JSONL file.
// Covers both custom-title and summary entry types.
type jsonlMetadataEntry struct {
	Type        string `json:"type"`
	Summary     string `json:"summary"`
	CustomTitle string `json:"customTitle"`
}

// maxMetadataLineLen is the maximum line length we bother inspecting for
// metadata. Metadata entries (custom-title, summary) are well under 1 KB.
// Conversation message lines can exceed 5 MB — skip those immediately rather
// than searching for substrings in megabytes of JSON.
const maxMetadataLineLen = 10 * 1024 // 10 KB

// scanJSONLFromOffset reads a JSONL file starting at the given byte offset and
// returns any custom-title and summary values found in the new data. Uses
// bufio.Reader (not Scanner) to handle arbitrarily long lines without aborting,
// and quick string matching before JSON parsing to avoid parsing every line.
func scanJSONLFromOffset(jsonlFilepath string, offset int64) (customTitle string, autoSummary string, err error) {
	file, err := os.Open(jsonlFilepath)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	if offset > 0 {
		if _, err := file.Seek(offset, 0); err != nil {
			return "", "", err
		}
	}

	reader := bufio.NewReaderSize(file, 64*1024) // 64 KB read buffer

	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			// Skip oversized lines — metadata entries are always small
			if len(line) <= maxMetadataLineLen {
				// Quick string check: skip lines that cannot contain metadata
				hasCustomTitle := strings.Contains(line, `"custom-title"`)
				hasSummary := strings.Contains(line, `"type":"summary"`)
				if hasCustomTitle || hasSummary {
					var entry jsonlMetadataEntry
					if jsonErr := json.Unmarshal([]byte(line), &entry); jsonErr == nil {
						switch entry.Type {
						case "custom-title":
							if entry.CustomTitle != "" {
								customTitle = entry.CustomTitle
							}
						case "summary":
							if entry.Summary != "" {
								autoSummary = entry.Summary
							}
						}
					}
				}
			}
		}
		if err != nil {
			if err == io.EOF {
				break
			}
			return customTitle, autoSummary, err
		}
	}

	return customTitle, autoSummary, nil
}
