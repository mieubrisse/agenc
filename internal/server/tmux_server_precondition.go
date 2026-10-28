package server

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/odyssey/agenc/internal/database"
	"github.com/odyssey/agenc/internal/tmux"
)

// tmuxNoServerNotificationKind marks a notification about a mission that could
// not be created because no tmux server was running.
const tmuxNoServerNotificationKind = "tmux.no-server"

// requireTmuxServerForMissionCreate refuses a mission create when no tmux
// server is running, before anything has been written.
//
// Missions live in tmux panes, so a mission create with no tmux server has
// nowhere to run. AgenC will not start the server itself — doing so makes it
// the macOS privacy identity for every pane in it, including the user's own
// shells (internal/tmux/server_ownership.go). The guard inside
// tmux.NewSessionCommand is what actually enforces that; this check exists so
// the refusal happens before a mission row, a mission directory and a trust
// entry have been created, and so the user gets the explanation rather than a
// mission with no pane.
//
// It is deliberately a precondition rather than a retry-until-a-server-appears
// wait. The thing being waited for is a human opening a terminal, so a cron
// that waits after a reboot does not eventually succeed — it stalls silently,
// holding a mission slot, which is the same invisible failure mode this whole
// guard exists to eliminate. A refusal is a signal; a stall is not.
func (s *Server) requireTmuxServerForMissionCreate(req CreateMissionRequest) error {
	if tmux.IsServerRunning() {
		return nil
	}

	s.logger.Printf("Refusing to create a mission: no tmux server is running, and AgenC does not create one")

	// A cron fires from launchd, where stderr lands in a log file nobody
	// reads, so the only way the user learns a scheduled run never started is
	// a notification.
	//
	// This ignores the cron's own notificationsEnabled setting on purpose:
	// that flag governs the routine "cron triggered" FYI, and a run that could
	// not start at all is a different class of event. Being seen is the entire
	// point of this one.
	if req.Source == cronMissionSource {
		s.createNoTmuxServerNotification(req)
	}

	return newHTTPError(http.StatusServiceUnavailable, tmux.NoServerErrorMessage())
}

// createNoTmuxServerNotification records a cron-triggered mission that could
// not start. Best-effort: a failure to write the notification is logged and
// never propagated, because the caller is already returning an error and
// replacing it with a database error would hide the real cause.
func (s *Server) createNoTmuxServerNotification(req CreateMissionRequest) {
	cronName, _ := parseCronSourceMetadata(req.SourceMetadata)
	titleSubject := cronName
	if titleSubject == "" {
		titleSubject = req.SourceID
	}

	notification := &database.Notification{
		ID:    uuid.New().String(),
		Kind:  tmuxNoServerNotificationKind,
		Title: sanitizeNotificationLine("Cron could not start (no tmux server): " + titleSubject),
		BodyMarkdown: "A scheduled mission could not be created, so this cron did not run. " +
			"It will be retried at its next scheduled time.\n\n" +
			tmux.NoServerErrorMessage(),
	}
	if err := s.db.CreateNotification(notification); err != nil {
		s.logger.Printf("failed to create no-tmux-server notification for cron %s: %v", req.SourceID, err)
	}
}
