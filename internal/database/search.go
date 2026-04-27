package database

import (
	"strings"

	"github.com/mieubrisse/stacktrace"
)

// SearchResult represents a single search hit with mission metadata.
type SearchResult struct {
	MissionID string
	SessionID string
	Snippet   string
	Rank      float64
}

// InsertSearchContentAndUpdateOffset atomically inserts text into the FTS5
// search index and updates the session's last_indexed_offset. Both operations
// happen in a single transaction — either both succeed or neither does.
func (db *DB) InsertSearchContentAndUpdateOffset(missionID, sessionID, content string, newOffset int64) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return stacktrace.Propagate(err, "failed to begin transaction")
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(
		"INSERT INTO mission_search_index (mission_id, session_id, content) VALUES (?, ?, ?)",
		missionID, sessionID, content,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to insert search content")
	}

	_, err = tx.Exec(
		"UPDATE sessions SET last_indexed_offset = ? WHERE id = ?",
		newOffset, sessionID,
	)
	if err != nil {
		return stacktrace.Propagate(err, "failed to update last_indexed_offset")
	}

	if err := tx.Commit(); err != nil {
		return stacktrace.Propagate(err, "failed to commit search index transaction")
	}
	return nil
}

// SearchMissions queries the FTS5 index and returns ranked mission results.
// The query is wrapped in double quotes to treat it as a phrase search by default,
// preventing accidental FTS5 operator syntax. Results are grouped by mission_id,
// returning only the best match per mission.
func (db *DB) SearchMissions(query string, limit int) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, nil
	}

	// Escape double quotes in the query, then wrap in quotes for literal matching.
	escaped := strings.ReplaceAll(query, `"`, `""`)
	ftsQuery := `"` + escaped + `"`

	results, err := db.executeSearch(ftsQuery, limit)
	if err != nil {
		// Phrase search failed — fall back to individual terms joined by AND
		terms := strings.Fields(query)
		if len(terms) > 1 {
			for i, term := range terms {
				terms[i] = `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
			}
			ftsQuery = strings.Join(terms, " AND ")
			results, err = db.executeSearch(ftsQuery, limit)
		}
		if err != nil {
			return nil, stacktrace.Propagate(err, "failed to search missions")
		}
	}

	return results, nil
}

func (db *DB) executeSearch(ftsQuery string, limit int) ([]SearchResult, error) {
	rows, err := db.conn.Query(`
		SELECT mission_id, session_id,
			snippet(mission_search_index, 2, '[', ']', '...', 20) as snippet,
			bm25(mission_search_index) as rank
		FROM mission_search_index
		WHERE content MATCH ?
		ORDER BY rank
		LIMIT ?
	`, ftsQuery, limit*3) // Over-fetch to allow dedup by mission
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	seen := make(map[string]bool)
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.MissionID, &r.SessionID, &r.Snippet, &r.Rank); err != nil {
			return nil, stacktrace.Propagate(err, "failed to scan search result")
		}
		if seen[r.MissionID] {
			continue
		}
		seen[r.MissionID] = true
		results = append(results, r)
		if len(results) >= limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return nil, stacktrace.Propagate(err, "error iterating search results")
	}

	return results, nil
}

// DeleteAllSearchContent removes all entries from the FTS5 index.
func (db *DB) DeleteAllSearchContent() error {
	_, err := db.conn.Exec("DELETE FROM mission_search_index")
	if err != nil {
		return stacktrace.Propagate(err, "failed to clear search index")
	}
	return nil
}

// SessionsNeedingIndexing returns sessions where known_file_size > last_indexed_offset,
// meaning there is new content the FTS indexer hasn't processed yet.
func (db *DB) SessionsNeedingIndexing() ([]*Session, error) {
	rows, err := db.conn.Query(
		"SELECT id, short_id, mission_id, custom_title, agenc_custom_title, auto_summary, last_title_update_offset, known_file_size, last_indexed_offset, created_at, updated_at FROM sessions WHERE known_file_size IS NOT NULL AND known_file_size > last_indexed_offset",
	)
	if err != nil {
		return nil, stacktrace.Propagate(err, "failed to query sessions needing indexing")
	}
	defer rows.Close()
	return scanSessions(rows)
}
