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
		var lastHeartbeat, lastActive, sessionNameUpdatedAt, cronID, cronName, configCommit, tmuxPane sql.NullString
		var createdAt, updatedAt string
		if err := rows.Scan(&m.ID, &m.ShortID, &m.Prompt, &m.Status, &m.GitRepo, &lastHeartbeat, &lastActive, &m.SessionName, &sessionNameUpdatedAt, &cronID, &cronName, &configCommit, &tmuxPane, &m.PromptCount, &m.LastSummaryPromptCount, &m.AISummary, &m.TmuxWindowTitle, &createdAt, &updatedAt); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan mission row")
		}
		if lastHeartbeat.Valid {
			t, _ := time.Parse(time.RFC3339, lastHeartbeat.String)
			m.LastHeartbeat = &t
		}
		if lastActive.Valid {
			t, _ := time.Parse(time.RFC3339, lastActive.String)
			m.LastActive = &t
		}
		if sessionNameUpdatedAt.Valid {
			t, _ := time.Parse(time.RFC3339, sessionNameUpdatedAt.String)
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
		m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
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
	var lastHeartbeat, lastActive, sessionNameUpdatedAt, cronID, cronName, configCommit, tmuxPane sql.NullString
	var createdAt, updatedAt string
	if err := row.Scan(&m.ID, &m.ShortID, &m.Prompt, &m.Status, &m.GitRepo, &lastHeartbeat, &lastActive, &m.SessionName, &sessionNameUpdatedAt, &cronID, &cronName, &configCommit, &tmuxPane, &m.PromptCount, &m.LastSummaryPromptCount, &m.AISummary, &m.TmuxWindowTitle, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	if lastHeartbeat.Valid {
		t, _ := time.Parse(time.RFC3339, lastHeartbeat.String)
		m.LastHeartbeat = &t
	}
	if lastActive.Valid {
		t, _ := time.Parse(time.RFC3339, lastActive.String)
		m.LastActive = &t
	}
	if sessionNameUpdatedAt.Valid {
		t, _ := time.Parse(time.RFC3339, sessionNameUpdatedAt.String)
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
	m.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	m.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &m, nil
}
