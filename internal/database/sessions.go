package database

import (
	"database/sql"
	"strings"
	"time"

	"github.com/mieubrisse/stacktrace"
)

// Session represents a row in the sessions table.
type Session struct {
	ID                string
	ShortID           string
	MissionID         string
	CustomTitle       string
	AgencCustomTitle  string
	AutoSummary       string
	LastScannedOffset int64
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// CreateSession inserts a new session row with the given ID and mission_id.
// Returns the created Session.
func (db *DB) CreateSession(missionID string, sessionID string) (*Session, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	shortID := ShortID(sessionID)
	_, err := db.conn.Exec(
		"INSERT INTO sessions (id, short_id, mission_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?)",
		sessionID, shortID, missionID, now, now,
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to insert session '%s'", sessionID)
	}

	return &Session{
		ID:        sessionID,
		ShortID:   shortID,
		MissionID: missionID,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}, nil
}

// GetSession returns a single session by ID, or (nil, nil) if not found.
func (db *DB) GetSession(sessionID string) (*Session, error) {
	row := db.conn.QueryRow(
		"SELECT id, short_id, mission_id, custom_title, agenc_custom_title, auto_summary, last_scanned_offset, created_at, updated_at FROM sessions WHERE id = ?",
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

// UpdateSessionScanResults updates the custom_title and last_scanned_offset
// for a session after an incremental JSONL scan.
// Only updates non-empty title values (preserves existing values when the
// new scan found nothing new).
//
// updated_at is only bumped when custom_title actually changes. Offset-only
// updates are silent — they must not affect GetActiveSession ordering, which
// uses updated_at to determine the "active" session for a mission.
func (db *DB) UpdateSessionScanResults(sessionID string, customTitle string, lastScannedOffset int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		`UPDATE sessions SET
			custom_title = CASE WHEN ? != '' THEN ? ELSE custom_title END,
			last_scanned_offset = ?,
			updated_at = CASE WHEN ? != '' THEN ? ELSE updated_at END
		WHERE id = ?`,
		customTitle, customTitle, lastScannedOffset, customTitle, now, sessionID,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update scan results for session '%s'", sessionID)
	}
	return nil
}

// UpdateSessionAutoSummary sets the auto_summary for a session.
//
// Does not bump updated_at — auto_summary is a background operation that must
// not affect GetActiveSession ordering. Only user-initiated actions (session
// creation, rename) should influence which session is considered "active."
func (db *DB) UpdateSessionAutoSummary(sessionID string, autoSummary string) error {
	_, err := db.conn.Exec(
		"UPDATE sessions SET auto_summary = ? WHERE id = ?",
		autoSummary, sessionID,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update auto_summary for session '%s'", sessionID)
	}
	return nil
}

// UpdateSessionAgencCustomTitle sets the agenc_custom_title for a session.
// An empty title clears the custom title.
func (db *DB) UpdateSessionAgencCustomTitle(sessionID string, title string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"UPDATE sessions SET agenc_custom_title = ?, updated_at = ? WHERE id = ?",
		title, now, sessionID,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update agenc_custom_title for session '%s'", sessionID)
	}
	return nil
}

// ListSessions returns all sessions across all missions,
// ordered by updated_at descending (most recently modified first).
func (db *DB) ListSessions() ([]*Session, error) {
	rows, err := db.conn.Query(
		"SELECT id, short_id, mission_id, custom_title, agenc_custom_title, auto_summary, last_scanned_offset, created_at, updated_at FROM sessions ORDER BY updated_at DESC",
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list sessions")
	}
	defer rows.Close()

	return scanSessions(rows)
}

// ListSessionsByMission returns all sessions for a given mission,
// ordered by updated_at descending (most recently modified first).
func (db *DB) ListSessionsByMission(missionID string) ([]*Session, error) {
	rows, err := db.conn.Query(
		"SELECT id, short_id, mission_id, custom_title, agenc_custom_title, auto_summary, last_scanned_offset, created_at, updated_at FROM sessions WHERE mission_id = ? ORDER BY updated_at DESC",
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
		"SELECT id, short_id, mission_id, custom_title, agenc_custom_title, auto_summary, last_scanned_offset, created_at, updated_at FROM sessions WHERE mission_id = ? ORDER BY updated_at DESC LIMIT 1",
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
	if err := row.Scan(&s.ID, &s.ShortID, &s.MissionID, &s.CustomTitle, &s.AgencCustomTitle, &s.AutoSummary, &s.LastScannedOffset, &createdAt, &updatedAt); err != nil {
		return nil, err
	}
	var err error
	s.CreatedAt, err = time.Parse(time.RFC3339, createdAt)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse session created_at timestamp")
	}
	s.UpdatedAt, err = time.Parse(time.RFC3339, updatedAt)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse session updated_at timestamp")
	}
	return &s, nil
}

// scanSessions scans multiple session rows from a query result.
func scanSessions(rows *sql.Rows) ([]*Session, error) {
	var sessions []*Session
	for rows.Next() {
		var s Session
		var createdAt, updatedAt string
		if err := rows.Scan(&s.ID, &s.ShortID, &s.MissionID, &s.CustomTitle, &s.AgencCustomTitle, &s.AutoSummary, &s.LastScannedOffset, &createdAt, &updatedAt); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan session row")
		}
		var parseErr error
		s.CreatedAt, parseErr = time.Parse(time.RFC3339, createdAt)
		if parseErr != nil {
			return nil, stacktrace.Propagate(parseErr, "failed to parse session created_at timestamp")
		}
		s.UpdatedAt, parseErr = time.Parse(time.RFC3339, updatedAt)
		if parseErr != nil {
			return nil, stacktrace.Propagate(parseErr, "failed to parse session updated_at timestamp")
		}
		sessions = append(sessions, &s)
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating session rows")
	}
	return sessions, nil
}

// ResolveSessionID resolves a user-provided session identifier (either a full
// UUID or an 8-character short ID) to the full session UUID. Returns an error
// if the identifier matches zero or multiple sessions.
func (db *DB) ResolveSessionID(userInput string) (string, error) {
	// Try exact match on full ID first (O(1) via primary key)
	var fullID string
	err := db.conn.QueryRow("SELECT id FROM sessions WHERE id = ?", userInput).Scan(&fullID)
	if err == nil {
		return fullID, nil
	}
	if err != sql.ErrNoRows {
		return "", stacktrace.Propagate(err, "failed to query session by full ID")
	}

	// Try match on short_id (O(1) via index)
	rows, err := db.conn.Query("SELECT id, mission_id FROM sessions WHERE short_id = ?", userInput)
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to query session by short ID")
	}
	defer rows.Close()

	type match struct {
		id        string
		missionID string
	}
	var matches []match
	for rows.Next() {
		var m match
		if err := rows.Scan(&m.id, &m.missionID); err != nil {
			return "", stacktrace.Propagate(err, "failed to scan session row")
		}
		matches = append(matches, m)
	}
	if err := rows.Err(); err != nil {
		return "", stacktrace.Propagate(err, "error iterating session rows")
	}

	switch len(matches) {
	case 0:
		return "", stacktrace.NewError("session '%s' not found", userInput)
	case 1:
		return matches[0].id, nil
	default:
		var lines []string
		for _, m := range matches {
			lines = append(lines, "  "+m.id+" (mission "+ShortID(m.missionID)+")")
		}
		return "", stacktrace.NewError(
			"ambiguous session short ID '%s' matches %d sessions:\n%s",
			userInput, len(matches), strings.Join(lines, "\n"),
		)
	}
}
