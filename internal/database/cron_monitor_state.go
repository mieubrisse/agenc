package database

import (
	"database/sql"
	"time"

	"github.com/mieubrisse/stacktrace"
)

// CronMonitorState is the cron monitor's memory of one cron between cycles.
//
// It holds only what cannot be re-derived: when the server first saw the cron
// (the reference point that keeps a cron created this afternoon from reading as
// overdue since this morning), and whether the user has already been told this
// cron is quiet (so a cron broken for a month produces one note, not thirty).
// Everything else the monitor needs is recomputed each cycle from the config
// and the missions table.
type CronMonitorState struct {
	CronID          string
	FirstSeenAt     time.Time
	QuietNotifiedAt *time.Time
}

// RecordCronFirstSeen stores when the server first observed a cron, leaving any
// existing record untouched. Called for every enabled cron on every cycle, so it
// must be idempotent — the first call is the one that counts.
func (db *DB) RecordCronFirstSeen(cronID string, firstSeenAt time.Time) error {
	_, err := db.conn.Exec(
		"INSERT OR IGNORE INTO cron_monitor_state (cron_id, first_seen_at, quiet_notified_at) VALUES (?, ?, NULL)",
		cronID, firstSeenAt.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to record first-seen time for cron '%v'", cronID)
	}
	return nil
}

// ListCronMonitorStates returns the monitor's stored state for every cron it
// has seen.
func (db *DB) ListCronMonitorStates() ([]*CronMonitorState, error) {
	rows, err := db.conn.Query("SELECT cron_id, first_seen_at, quiet_notified_at FROM cron_monitor_state")
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to list cron monitor state")
	}
	defer rows.Close()

	states := []*CronMonitorState{}
	for rows.Next() {
		var state CronMonitorState
		var firstSeenAt string
		var quietNotifiedAt sql.NullString
		if err := rows.Scan(&state.CronID, &firstSeenAt, &quietNotifiedAt); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan cron monitor state row")
		}

		parsedFirstSeenAt, err := time.Parse(time.RFC3339, firstSeenAt)
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to parse first-seen timestamp for cron '%v'", state.CronID)
		}
		state.FirstSeenAt = parsedFirstSeenAt

		if quietNotifiedAt.Valid {
			parsedQuietNotifiedAt, err := time.Parse(time.RFC3339, quietNotifiedAt.String)
			if err != nil {
				return nil, stacktrace.Propagate(err, "failed to parse quiet-notified timestamp for cron '%v'", state.CronID)
			}
			state.QuietNotifiedAt = &parsedQuietNotifiedAt
		}

		states = append(states, &state)
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating cron monitor state rows")
	}

	return states, nil
}

// SetCronQuietNotifiedAt records that the user has been told this cron is quiet,
// or clears that record when notifiedAt is nil because the cron has started
// producing missions again. Clearing is what lets a cron that recovers and later
// goes quiet a second time earn a fresh note.
func (db *DB) SetCronQuietNotifiedAt(cronID string, notifiedAt *time.Time) error {
	var storedValue sql.NullString
	if notifiedAt != nil {
		storedValue = sql.NullString{String: notifiedAt.UTC().Format(time.RFC3339), Valid: true}
	}

	_, err := db.conn.Exec(
		"UPDATE cron_monitor_state SET quiet_notified_at = ? WHERE cron_id = ?",
		storedValue, cronID,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update quiet-notified time for cron '%v'", cronID)
	}
	return nil
}

// DeleteCronMonitorState forgets a cron the monitor no longer watches, because
// it was deleted or disabled. Forgetting is deliberate: a cron disabled for
// months and then re-enabled should start its clock from the moment it comes
// back, not from whenever it last ran.
func (db *DB) DeleteCronMonitorState(cronID string) error {
	_, err := db.conn.Exec("DELETE FROM cron_monitor_state WHERE cron_id = ?", cronID)
	if err != nil {
		return stacktrace.Propagate(err, "failed to delete monitor state for cron '%v'", cronID)
	}
	return nil
}
