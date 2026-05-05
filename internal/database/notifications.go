package database

import (
	"database/sql"
	"time"

	"github.com/mieubrisse/stacktrace"
)

// Notification is an append-only record surfaced to the user via
// `agenc notifications ls`. Created by AgenC subsystems (e.g. the
// writeable-copy reconcile loop) or by agents via the CLI.
type Notification struct {
	ID           string
	Kind         string
	SourceRepo   string
	Title        string
	BodyMarkdown string
	CreatedAt    time.Time
	ReadAt       *time.Time
}

// ListNotificationsParams holds optional parameters for filtering notifications.
type ListNotificationsParams struct {
	UnreadOnly bool
	SourceRepo string
	Kind       string
}

// CreateNotification inserts a new notification row. The caller is
// responsible for setting n.ID (typically a UUID); CreatedAt is set
// automatically if zero.
func (db *DB) CreateNotification(n *Notification) error {
	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}
	var sourceRepo sql.NullString
	if n.SourceRepo != "" {
		sourceRepo = sql.NullString{String: n.SourceRepo, Valid: true}
	}
	_, err := db.conn.Exec(
		"INSERT INTO notifications (id, kind, source_repo, title, body_markdown, created_at, read_at) VALUES (?, ?, ?, ?, ?, ?, NULL)",
		n.ID, n.Kind, sourceRepo, n.Title, n.BodyMarkdown, n.CreatedAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to insert notification with id '%v' kind '%v'", n.ID, n.Kind)
	}
	return nil
}

// GetNotification returns the notification with the given ID, or an error
// if not found.
func (db *DB) GetNotification(id string) (*Notification, error) {
	row := db.conn.QueryRow(
		"SELECT id, kind, source_repo, title, body_markdown, created_at, read_at FROM notifications WHERE id = ?",
		id,
	)
	return scanNotification(row)
}

// ListNotifications returns notifications matching the given filter,
// ordered by creation time descending.
func (db *DB) ListNotifications(params ListNotificationsParams) ([]*Notification, error) {
	query, args := buildListNotificationsQuery(params)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list notifications")
	}
	defer rows.Close()

	return scanNotifications(rows)
}

// MarkNotificationRead sets read_at on the given notification. Idempotent:
// if the notification is already read, this is a no-op (read_at is preserved).
func (db *DB) MarkNotificationRead(id string) error {
	_, err := db.conn.Exec(
		"UPDATE notifications SET read_at = ? WHERE id = ? AND read_at IS NULL",
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to mark notification '%v' as read", id)
	}
	return nil
}

// CountUnreadNotifications returns the number of notifications with read_at IS NULL.
func (db *DB) CountUnreadNotifications() (int, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM notifications WHERE read_at IS NULL").Scan(&count)
	if err != nil {
		return 0, stacktrace.Propagate(err, "failed to count unread notifications")
	}
	return count, nil
}

// ResolveNotificationID resolves a user-supplied identifier (full UUID or any
// unambiguous prefix, typically the 8-char short ID shown in `notifications
// ls`) to a full notification UUID. Returns an error when zero or multiple
// notifications match.
func (db *DB) ResolveNotificationID(userInput string) (string, error) {
	// Try exact match first
	var fullID string
	err := db.conn.QueryRow("SELECT id FROM notifications WHERE id = ?", userInput).Scan(&fullID)
	if err == nil {
		return fullID, nil
	}
	if err != sql.ErrNoRows {
		return "", stacktrace.Propagate(err, "failed to query notification by full ID")
	}

	// Prefix match
	rows, err := db.conn.Query("SELECT id, title FROM notifications WHERE id LIKE ?", userInput+"%")
	if err != nil {
		return "", stacktrace.Propagate(err, "failed to query notification by prefix")
	}
	defer rows.Close()

	type match struct {
		id    string
		title string
	}
	var matches []match
	for rows.Next() {
		var m match
		if err := rows.Scan(&m.id, &m.title); err != nil {
			return "", stacktrace.Propagate(err, "failed to scan notification row")
		}
		matches = append(matches, m)
	}
	if err := rows.Err(); err != nil {
		return "", stacktrace.Propagate(err, "error iterating notification rows")
	}

	switch len(matches) {
	case 0:
		return "", stacktrace.NewError("notification '%s' not found", userInput)
	case 1:
		return matches[0].id, nil
	default:
		lines := make([]string, 0, len(matches))
		for _, m := range matches {
			lines = append(lines, "  "+m.id+"  "+m.title)
		}
		joined := ""
		for i, line := range lines {
			if i > 0 {
				joined += "\n"
			}
			joined += line
		}
		return "", stacktrace.NewError(
			"prefix '%s' is ambiguous; matches %d notifications:\n%s\nPlease provide more of the UUID to disambiguate.",
			userInput, len(matches), joined,
		)
	}
}
