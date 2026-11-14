package database

import (
	"database/sql"
	"time"

	"github.com/mieubrisse/stacktrace"
)

// WriteableCopyPause records that a writeable copy has an unresolved failure
// the user has already been told about — a rebase conflict, a refused push, an
// auth failure, a broken git state.
//
// The row does not stop the sync loop, which goes on retrying on every trigger.
// What it does is suppress repeat notifications for a failure already reported,
// and carry the reason so that a *different* failure can be told apart from the
// same one recurring. It is deleted the first time a tick reconciles fully.
type WriteableCopyPause struct {
	RepoName         string
	PausedAt         time.Time
	PausedReason     string
	LocalHeadAtPause string
	NotificationID   string
}

// UpsertPauseAndNotification atomically inserts both a pause row and a
// notification. Returns (true, nil) when both were inserted. Returns
// (false, nil) when a pause for the repo already exists (no-op — neither
// the pause nor the notification is written). The caller may pre-set
// p.PausedAt and n.CreatedAt; both default to time.Now().UTC() when zero.
func (db *DB) UpsertPauseAndNotification(p *WriteableCopyPause, n *Notification) (bool, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to begin transaction for repo '%v'", p.RepoName)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var existing string
	err = tx.QueryRow("SELECT repo_name FROM writeable_copy_pauses WHERE repo_name = ?", p.RepoName).Scan(&existing)
	if err == nil {
		return false, nil
	}
	if err != sql.ErrNoRows {
		return false, stacktrace.Propagate(err, "failed to check for existing pause for repo '%v'", p.RepoName)
	}

	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}
	var sourceRepo sql.NullString
	if n.SourceRepo != "" {
		sourceRepo = sql.NullString{String: n.SourceRepo, Valid: true}
	}
	if _, err := tx.Exec(
		"INSERT INTO notifications (id, kind, source_repo, title, body_markdown, created_at, read_at) VALUES (?, ?, ?, ?, ?, ?, NULL)",
		n.ID, n.Kind, sourceRepo, n.Title, n.BodyMarkdown, n.CreatedAt.UTC().Format(time.RFC3339),
	); err != nil {
		return false, stacktrace.Propagate(err, "failed to insert notification for repo '%v'", p.RepoName)
	}

	if p.PausedAt.IsZero() {
		p.PausedAt = time.Now().UTC()
	}
	if _, err := tx.Exec(
		"INSERT INTO writeable_copy_pauses (repo_name, paused_at, paused_reason, local_head_at_pause, notification_id) VALUES (?, ?, ?, ?, ?)",
		p.RepoName, p.PausedAt.UTC().Format(time.RFC3339), p.PausedReason, p.LocalHeadAtPause, p.NotificationID,
	); err != nil {
		return false, stacktrace.Propagate(err, "failed to insert pause for repo '%v'", p.RepoName)
	}

	if err := tx.Commit(); err != nil {
		return false, stacktrace.Propagate(err, "failed to commit pause transaction for repo '%v'", p.RepoName)
	}
	committed = true
	return true, nil
}

// GetPause returns the pause for a repo, or nil if none exists.
func (db *DB) GetPause(repoName string) (*WriteableCopyPause, error) {
	row := db.conn.QueryRow(
		"SELECT repo_name, paused_at, paused_reason, local_head_at_pause, notification_id FROM writeable_copy_pauses WHERE repo_name = ?",
		repoName,
	)
	var p WriteableCopyPause
	var pausedAt string
	err := row.Scan(&p.RepoName, &pausedAt, &p.PausedReason, &p.LocalHeadAtPause, &p.NotificationID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to get pause for repo '%v'", repoName)
	}
	t, err := time.Parse(time.RFC3339, pausedAt)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to parse paused_at timestamp '%v'", pausedAt)
	}
	p.PausedAt = t
	return &p, nil
}

// ListPauses returns all current pauses, ordered by paused_at descending.
func (db *DB) ListPauses() ([]*WriteableCopyPause, error) {
	rows, err := db.conn.Query("SELECT repo_name, paused_at, paused_reason, local_head_at_pause, notification_id FROM writeable_copy_pauses ORDER BY paused_at DESC")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list pauses")
	}
	defer rows.Close()

	var pauses []*WriteableCopyPause
	for rows.Next() {
		var p WriteableCopyPause
		var pausedAt string
		if err := rows.Scan(&p.RepoName, &pausedAt, &p.PausedReason, &p.LocalHeadAtPause, &p.NotificationID); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan pause row")
		}
		t, err := time.Parse(time.RFC3339, pausedAt)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse paused_at timestamp '%v'", pausedAt)
		}
		p.PausedAt = t
		pauses = append(pauses, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating pause rows")
	}
	return pauses, nil
}

// ReplacePauseAndNotification repoints an existing pause row at a new reason
// and a new notification, in one transaction.
//
// This is what makes a writeable copy's failure notifications escalate on
// change rather than on repetition. A repo whose failure is still the one
// already reported writes nothing (UpsertPauseAndNotification is a no-op while
// a row exists, so the episode keeps its single notification); a repo whose
// failure has turned into a different one gets a fresh notification through
// here, and the row follows it.
//
// Returns (false, nil) when no pause row exists, so the caller can insert one
// with UpsertPauseAndNotification instead.
func (db *DB) ReplacePauseAndNotification(p *WriteableCopyPause, n *Notification) (bool, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to begin transaction for repo '%v'", p.RepoName)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	var existing string
	err = tx.QueryRow("SELECT repo_name FROM writeable_copy_pauses WHERE repo_name = ?", p.RepoName).Scan(&existing)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, stacktrace.Propagate(err, "failed to check for an existing pause for repo '%v'", p.RepoName)
	}

	if n.CreatedAt.IsZero() {
		n.CreatedAt = time.Now().UTC()
	}
	var sourceRepo sql.NullString
	if n.SourceRepo != "" {
		sourceRepo = sql.NullString{String: n.SourceRepo, Valid: true}
	}
	if _, err := tx.Exec(
		"INSERT INTO notifications (id, kind, source_repo, title, body_markdown, created_at, read_at) VALUES (?, ?, ?, ?, ?, ?, NULL)",
		n.ID, n.Kind, sourceRepo, n.Title, n.BodyMarkdown, n.CreatedAt.UTC().Format(time.RFC3339),
	); err != nil {
		return false, stacktrace.Propagate(err, "failed to insert notification for repo '%v'", p.RepoName)
	}

	if p.PausedAt.IsZero() {
		p.PausedAt = time.Now().UTC()
	}
	if _, err := tx.Exec(
		"UPDATE writeable_copy_pauses SET paused_at = ?, paused_reason = ?, local_head_at_pause = ?, notification_id = ? WHERE repo_name = ?",
		p.PausedAt.UTC().Format(time.RFC3339), p.PausedReason, p.LocalHeadAtPause, p.NotificationID, p.RepoName,
	); err != nil {
		return false, stacktrace.Propagate(err, "failed to update pause for repo '%v'", p.RepoName)
	}

	if err := tx.Commit(); err != nil {
		return false, stacktrace.Propagate(err, "failed to commit pause-replacement transaction for repo '%v'", p.RepoName)
	}
	committed = true
	return true, nil
}

// DeletePause removes the pause for a repo. Idempotent: deleting a non-existent
// pause is not an error.
func (db *DB) DeletePause(repoName string) error {
	_, err := db.conn.Exec("DELETE FROM writeable_copy_pauses WHERE repo_name = ?", repoName)
	if err != nil {
		return stacktrace.Propagate(err, "failed to delete pause for repo '%v'", repoName)
	}
	return nil
}
