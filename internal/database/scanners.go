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
		var lastHeartbeat, sessionNameUpdatedAt, cronID, cronName, configCommit, tmuxPane sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&m.ID, &m.ShortID, &m.Prompt, &m.Status, &m.GitRepo, &lastHeartbeat, &m.SessionName, &sessionNameUpdatedAt, &cronID, &cronName, &configCommit, &tmuxPane, &m.PromptCount, &createdAt, &updatedAt); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan mission row")
		}
		if lastHeartbeat.Valid {
			t, err := time.Parse(time.RFC3339, lastHeartbeat.String)
			if err != nil {
				return nil, stacktrace.Propagate(err, "failed to parse last_heartbeat timestamp")
			}
			m.LastHeartbeat = &t
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

// scanMission scans a single mission row from a query result.
func scanMission(row *sql.Row) (*Mission, error) {
	var m Mission
	var lastHeartbeat, sessionNameUpdatedAt, cronID, cronName, configCommit, tmuxPane sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&m.ID, &m.ShortID, &m.Prompt, &m.Status, &m.GitRepo, &lastHeartbeat, &m.SessionName, &sessionNameUpdatedAt, &cronID, &cronName, &configCommit, &tmuxPane, &m.PromptCount, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if lastHeartbeat.Valid {
		t, err := time.Parse(time.RFC3339, lastHeartbeat.String)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse last_heartbeat timestamp")
		}
		m.LastHeartbeat = &t
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
