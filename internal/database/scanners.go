package database

import (
	"database/sql"
	"time"

	"github.com/mieubrisse/stacktrace"
)

// scanMissions scans multiple mission rows from a query result.
func scanMissions(rows *sql.Rows) ([]*Mission, error) {
	var missions []*Mission
	for rows.Next() {
		var m Mission
		var lastHeartbeat, lastUserPromptAt, sessionNameUpdatedAt, cronID, cronName, configCommit, tmuxPane, source, sourceID, sourceMetadata sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&m.ID, &m.ShortID, &m.Prompt, &m.Status, &m.GitRepo, &lastHeartbeat, &lastUserPromptAt, &m.SessionName, &sessionNameUpdatedAt, &cronID, &cronName, &configCommit, &tmuxPane, &m.PromptCount, &createdAt, &updatedAt, &source, &sourceID, &sourceMetadata); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan mission row")
		}
		if lastHeartbeat.Valid {
			t, err := time.Parse(time.RFC3339, lastHeartbeat.String)
			if err != nil {
				return nil, stacktrace.Propagate(err, "failed to parse last_heartbeat timestamp")
			}
			m.LastHeartbeat = &t
		}
		if lastUserPromptAt.Valid {
			t, err := time.Parse(time.RFC3339, lastUserPromptAt.String)
			if err != nil {
				return nil, stacktrace.Propagate(err, "failed to parse last_user_prompt_at timestamp")
			}
			m.LastUserPromptAt = &t
		}
		if sessionNameUpdatedAt.Valid {
			t, err := time.Parse(time.RFC3339, sessionNameUpdatedAt.String)
			if err != nil {
				return nil, stacktrace.Propagate(err, "failed to parse session_name_updated_at timestamp")
			}
			m.SessionNameUpdatedAt = &t
		}
		if cronID.Valid {
			m.CronID = &cronID.String
		}
		if cronName.Valid {
			m.CronName = &cronName.String
		}
		if configCommit.Valid {
			m.ConfigCommit = &configCommit.String
		}
		if tmuxPane.Valid {
			m.TmuxPane = &tmuxPane.String
		}
		if source.Valid {
			m.Source = &source.String
		}
		if sourceID.Valid {
			m.SourceID = &sourceID.String
		}
		if sourceMetadata.Valid {
			m.SourceMetadata = &sourceMetadata.String
		}
		var err error
		m.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse created_at timestamp")
		}
		m.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse updated_at timestamp")
		}
		missions = append(missions, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating mission rows")
	}
	return missions, nil
}

// scanNotifications scans multiple notification rows from a query result.
func scanNotifications(rows *sql.Rows) ([]*Notification, error) {
	var notifications []*Notification
	for rows.Next() {
		n, err := scanNotificationFromRows(rows)
		if err != nil {
			return nil, err
		}
		notifications = append(notifications, n)
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating notification rows")
	}
	return notifications, nil
}

// scanNotification scans a single notification row from a QueryRow result.
func scanNotification(row *sql.Row) (*Notification, error) {
	var n Notification
	var sourceRepo sql.NullString
	var createdAt string
	var readAt sql.NullString
	if err := row.Scan(&n.ID, &n.Kind, &sourceRepo, &n.Title, &n.BodyMarkdown, &createdAt, &readAt); err != nil {
		return nil, stacktrace.Propagate(err, "failed to scan notification row")
	}
	if err := populateNotificationTimes(&n, sourceRepo, createdAt, readAt); err != nil {
		return nil, err
	}
	return &n, nil
}

// scanNotificationFromRows scans a single notification from rows iteration.
// Separated from scanNotification because *sql.Rows and *sql.Row don't share
// a common Scan interface in the standard library.
func scanNotificationFromRows(rows *sql.Rows) (*Notification, error) {
	var n Notification
	var sourceRepo sql.NullString
	var createdAt string
	var readAt sql.NullString
	if err := rows.Scan(&n.ID, &n.Kind, &sourceRepo, &n.Title, &n.BodyMarkdown, &createdAt, &readAt); err != nil {
		return nil, stacktrace.Propagate(err, "failed to scan notification row")
	}
	if err := populateNotificationTimes(&n, sourceRepo, createdAt, readAt); err != nil {
		return nil, err
	}
	return &n, nil
}

func populateNotificationTimes(n *Notification, sourceRepo sql.NullString, createdAt string, readAt sql.NullString) error {
	if sourceRepo.Valid {
		n.SourceRepo = sourceRepo.String
	}
	t, err := time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return stacktrace.Propagate(err, "failed to parse notification created_at timestamp '%v'", createdAt)
	}
	n.CreatedAt = t
	if readAt.Valid {
		readTime, err := time.Parse(time.RFC3339, readAt.String)
		if err != nil {
			return stacktrace.Propagate(err, "failed to parse notification read_at timestamp '%v'", readAt.String)
		}
		n.ReadAt = &readTime
	}
	return nil
}

// scanMission scans a single mission row from a query result.
func scanMission(row *sql.Row) (*Mission, error) {
	var m Mission
	var lastHeartbeat, lastUserPromptAt, sessionNameUpdatedAt, cronID, cronName, configCommit, tmuxPane, source, sourceID, sourceMetadata sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&m.ID, &m.ShortID, &m.Prompt, &m.Status, &m.GitRepo, &lastHeartbeat, &lastUserPromptAt, &m.SessionName, &sessionNameUpdatedAt, &cronID, &cronName, &configCommit, &tmuxPane, &m.PromptCount, &createdAt, &updatedAt, &source, &sourceID, &sourceMetadata); err != nil {
		return nil, err
	}
	if lastHeartbeat.Valid {
		t, err := time.Parse(time.RFC3339, lastHeartbeat.String)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse last_heartbeat timestamp")
		}
		m.LastHeartbeat = &t
	}
	if lastUserPromptAt.Valid {
		t, err := time.Parse(time.RFC3339, lastUserPromptAt.String)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse last_user_prompt_at timestamp")
		}
		m.LastUserPromptAt = &t
	}
	if sessionNameUpdatedAt.Valid {
		t, err := time.Parse(time.RFC3339, sessionNameUpdatedAt.String)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse session_name_updated_at timestamp")
		}
		m.SessionNameUpdatedAt = &t
	}
	if cronID.Valid {
		m.CronID = &cronID.String
	}
	if cronName.Valid {
		m.CronName = &cronName.String
	}
	if configCommit.Valid {
		m.ConfigCommit = &configCommit.String
	}
	if tmuxPane.Valid {
		m.TmuxPane = &tmuxPane.String
	}
	if source.Valid {
		m.Source = &source.String
	}
	if sourceID.Valid {
		m.SourceID = &sourceID.String
	}
	if sourceMetadata.Valid {
		m.SourceMetadata = &sourceMetadata.String
	}
	var err error
	m.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse created_at timestamp")
	}
	m.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse updated_at timestamp")
	}
	return &m, nil
}
