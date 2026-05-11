package server

import (
	"context"
	"time"

	"github.com/odyssey/agenc/internal/database"
)

// autoSummaryInterval is how often the auto-summary loop scans for sessions
// needing a generated title from Claude Haiku.
const autoSummaryInterval = 3 * time.Second

// summarizeFunc is the signature of the auto-summary generator. Production code
// uses generateSessionSummary; tests inject mocks via runAutoSummaryCycleWith.
type summarizeFunc func(ctx context.Context, agencDirpath, firstUserMessage string, maxWords int) (string, error)

// runAutoSummaryLoop runs the auto-summary cycle every autoSummaryInterval until
// ctx is cancelled.
func (s *Server) runAutoSummaryLoop(ctx context.Context) {
	// Initial delay to let the file watcher populate known_file_size first.
	select {
	case <-ctx.Done():
		return
	case <-time.After(autoSummaryInterval):
		s.runAutoSummaryCycle(ctx)
	}

	ticker := time.NewTicker(autoSummaryInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runAutoSummaryCycle(ctx)
		}
	}
}

// runAutoSummaryCycle is the production entrypoint: it scans sessions that need
// an auto_summary and invokes the real Haiku summarizer.
func (s *Server) runAutoSummaryCycle(ctx context.Context) {
	s.runAutoSummaryCycleWith(ctx, generateSessionSummary)
}

// runAutoSummaryCycleWith is the test-friendly inner cycle. It takes the
// summarizer as a parameter so tests can inject success/failure mocks. For
// each session that needs an auto_summary, it scans the JSONL for the first
// user message, calls the summarizer, and atomically writes the summary and
// the offset together. Critically, if the summarizer fails, the offset is NOT
// advanced — the session is re-picked on the next cycle and retried.
func (s *Server) runAutoSummaryCycleWith(ctx context.Context, summarize summarizeFunc) {
	sessions, err := s.db.SessionsNeedingAutoSummary()
	if err != nil {
		s.logger.Printf("Auto-summary: failed to query sessions: %v", err)
		return
	}

	maxWords := s.getConfig().GetSessionTitleMaxWords()

	for _, sess := range sessions {
		jsonlPath := s.resolveSessionJSONLPath(sess)
		if jsonlPath == "" {
			continue
		}

		knownSize := int64(0)
		if sess.KnownFileSize != nil {
			knownSize = *sess.KnownFileSize
		}

		msg, err := scanJSONLForFirstUserMessage(jsonlPath, sess.LastAutoSummaryScanOffset)
		if err != nil {
			s.logger.Printf("Auto-summary: scan failed for session '%s': %v", database.ShortID(sess.ID), err)
			continue
		}
		if msg == "" {
			// No first user message in the scanned range — advance the offset
			// so we don't re-scan the same bytes next cycle. The session will
			// be re-selected only when the file grows past this offset.
			if err := s.db.UpdateAutoSummaryScanOffset(sess.ID, knownSize); err != nil {
				s.logger.Printf("Auto-summary: failed to advance offset for session '%s': %v", database.ShortID(sess.ID), err)
			}
			continue
		}

		summary, err := summarize(ctx, s.agencDirpath, msg, maxWords)
		if err != nil {
			// Summarizer failure (Haiku CLI killed, timeout, missing OAuth,
			// response too long, etc.) — leave the offset untouched so the
			// next cycle re-picks this session and retries. This is the bug
			// fix: the old pipeline advanced the offset before/regardless of
			// the summarizer succeeding, so a single Haiku failure caused
			// permanent loss of the auto_summary.
			s.logger.Printf("Auto-summary: failed to generate summary for session '%s': %v", database.ShortID(sess.ID), err)
			continue
		}

		if err := s.db.UpdateAutoSummaryAndOffset(sess.ID, summary, knownSize); err != nil {
			s.logger.Printf("Auto-summary: failed to save summary for session '%s': %v", database.ShortID(sess.ID), err)
			continue
		}

		s.reconcileTmuxWindowTitle(sess.MissionID)
		s.logger.Printf("Auto-summary: set auto_summary for session '%s': %q", database.ShortID(sess.ID), summary)
	}
}
