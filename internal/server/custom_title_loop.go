package server

import (
	"context"
	"time"

	"github.com/odyssey/agenc/internal/database"
)

// customTitleInterval is how often the custom-title loop scans for sessions
// whose JSONL has grown since their last custom-title scan.
const customTitleInterval = 3 * time.Second

// runCustomTitleLoop runs the custom-title cycle every customTitleInterval
// until ctx is cancelled.
func (s *Server) runCustomTitleLoop(ctx context.Context) {
	// Initial delay to let the file watcher populate known_file_size first.
	select {
	case <-ctx.Done():
		return
	case <-time.After(customTitleInterval):
		s.runCustomTitleCycle()
	}

	ticker := time.NewTicker(customTitleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCustomTitleCycle()
		}
	}
}

// runCustomTitleCycle scans every session whose JSONL has grown beyond its
// last custom-title scan offset. For each session it has three branches:
//
//  1. Scan finds a custom-title metadata entry that differs from the
//     session's current CustomTitle → atomically write the new title and
//     advance the offset, then reconcile the tmux window title.
//  2. Scan finds a title equal to the existing CustomTitle → advance only
//     the offset (no spurious DB write to custom_title, no tmux reconcile).
//  3. Scan finds no custom-title metadata in the new bytes → advance only
//     the offset; the session will be re-selected only when the file grows.
//
// Like the auto-summary loop, every output (title) and its offset advance
// move in a single SQL statement, so a scan or DB error naturally leaves the
// offset where it was and the session is retried on the next cycle.
func (s *Server) runCustomTitleCycle() {
	sessions, err := s.db.SessionsNeedingCustomTitleUpdate()
	if err != nil {
		s.logger.Printf("Custom-title: failed to query sessions: %v", err)
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

		title, err := scanJSONLForCustomTitle(jsonlPath, sess.LastCustomTitleScanOffset)
		if err != nil {
			s.logger.Printf("Custom-title: scan failed for session '%s': %v", database.ShortID(sess.ID), err)
			continue
		}

		if title != "" && title != sess.CustomTitle {
			if err := s.db.UpdateCustomTitleAndOffset(sess.ID, title, knownSize); err != nil {
				s.logger.Printf("Custom-title: failed to save title for session '%s': %v", database.ShortID(sess.ID), err)
				continue
			}
			s.reconcileTmuxWindowTitle(sess.MissionID)
			s.logger.Printf("Custom-title: set custom_title for session '%s': %q", database.ShortID(sess.ID), title)
			continue
		}

		// No new title or same title: advance the offset only.
		if err := s.db.UpdateCustomTitleScanOffset(sess.ID, knownSize); err != nil {
			s.logger.Printf("Custom-title: failed to advance offset for session '%s': %v", database.ShortID(sess.ID), err)
		}
	}
}
