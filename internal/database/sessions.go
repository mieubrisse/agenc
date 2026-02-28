package database

import (
	"database/sql"
	"time"

	"github.com/mieubrisse/stacktrace"
)

// Session represents a row in the sessions table.
type Session struct {
	ID                string
	MissionID         string
	CustomTitle       string
	AutoSummary       string
	LastScannedOffset int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CreateSession inserts a new session row with the given ID and mission_id.
// Returns the created Session.
func (db *DB) CreateSession(missionID string, sessionID string) (*Session, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"INSERT INTO sessions (id, mission_id, created_at, updated_at) VALUES (?, ?, ?, ?)",
		sessionID, missionID, now, now,
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to insert session '%s'", sessionID)
	}

	return &Session{
		ID:        sessionID,
		MissionID: missionID,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}, nil
}

// GetSession returns a single session by ID, or (nil, nil) if not found.
func (db *DB) GetSession(sessionID string) (*Session, error) {
	row := db.conn.QueryRow(
		"SELECT id, mission_id, custom_title, auto_summary, last_scanned_offset, created_at, updated_at FROM sessions WHERE id = ?",
		sessionID,
	)

	s, err := scanSession(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get session '%s'", sessionID)
	}
	return s, nil
}

// UpdateSessionScanResults updates the custom_title, auto_summary, and
// last_scanned_offset for a session after an incremental JSONL scan.
// Only updates non-empty title/summary values (preserves existing values
// when the new scan found nothing new for that field).
func (db *DB) UpdateSessionScanResults(sessionID string, customTitle string, autoSummary string, lastScannedOffset int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		`UPDATE sessions SET
			custom_title = CASE WHEN ? != '' THEN ? ELSE custom_title END,
			auto_summary = CASE WHEN ? != '' THEN ? ELSE auto_summary END,
			last_scanned_offset = ?,
			updated_at = ?
		WHERE id = ?`,
		customTitle, customTitle, autoSummary, autoSummary, lastScannedOffset, now, sessionID,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update scan results for session '%s'", sessionID)
	}
	return nil
}

// ListSessionsByMission returns all sessions for a given mission,
// ordered by updated_at descending (most recently modified first).
func (db *DB) ListSessionsByMission(missionID string) ([]*Session, error) {
	rows, err := db.conn.Query(
		"SELECT id, mission_id, custom_title, auto_summary, last_scanned_offset, created_at, updated_at FROM sessions WHERE mission_id = ? ORDER BY updated_at DESC",
		missionID,
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list sessions for mission '%s'", missionID)
	}
	defer rows.Close()

	return scanSessions(rows)
}

// GetActiveSession returns the most recently modified session for a mission,
// or (nil, nil) if the mission has no sessions.
func (db *DB) GetActiveSession(missionID string) (*Session, error) {
	row := db.conn.QueryRow(
		"SELECT id, mission_id, custom_title, auto_summary, last_scanned_offset, created_at, updated_at FROM sessions WHERE mission_id = ? ORDER BY updated_at DESC LIMIT 1",
		missionID,
	)

	s, err := scanSession(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get active session for mission '%s'", missionID)
	}
	return s, nil
}

// scanSession scans a single session row.
func scanSession(row *sql.Row) (*Session, error) {
	var s Session
	var createdAt, updatedAt string
	if err := row.Scan(&s.ID, &s.MissionID, &s.CustomTitle, &s.AutoSummary, &s.LastScannedOffset, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &s, nil
}

// scanSessions scans multiple session rows from a query result.
func scanSessions(rows *sql.Rows) ([]*Session, error) {
	var sessions []*Session
	for rows.Next() {
		var s Session
		var createdAt, updatedAt string
		if err := rows.Scan(&s.ID, &s.MissionID, &s.CustomTitle, &s.AutoSummary, &s.LastScannedOffset, &createdAt, &updatedAt); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan session row")
		}
		s.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		s.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
		sessions = append(sessions, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating session rows")
	}
	return sessions, nil
}
