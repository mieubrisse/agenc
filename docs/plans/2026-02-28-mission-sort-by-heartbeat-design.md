Mission Sort by Heartbeat
=========================

Problem
-------

The `mission resume` fzf picker (and all mission listing) sorts by `COALESCE(last_active, last_heartbeat, created_at) DESC`. The `last_active` column is only updated when a user submits a prompt, which is infrequent and doesn't reflect ongoing session activity (assistant responses, tool use, etc.). This makes the sort order feel stale.

Design
------

Simplify: drop `last_active` entirely and rely on the wrapper heartbeat as the activity signal. Increase heartbeat frequency from 60s to 10s so it's a responsive proxy for "this mission is alive and active."

### Changes

1. **Heartbeat interval**: Change from 60s to 10s in `internal/wrapper/wrapper.go`
2. **Drop `last_active` column**: Add a migration to drop the column from `missions`
3. **Remove all `last_active` code**: Delete `UpdateLastActive`, the `/missions/{id}/prompt` handler that calls it, the client method, and all references across 7 files
4. **Update sort query**: Change to `COALESCE(last_heartbeat, created_at) DESC`
5. **Update index**: Change `idx_missions_activity` to cover `(last_heartbeat DESC)` only

### Files affected

- `internal/wrapper/wrapper.go` — heartbeat interval constant
- `internal/database/migrations.go` — new migration to drop column, update index
- `internal/database/missions.go` — remove `UpdateLastActive`, remove `LastActive` from struct/scan
- `internal/database/queries.go` — update ORDER BY and SELECT
- `internal/database/database.go` — remove any `LastActive` references
- `internal/database/database_test.go` — update sort tests
- `internal/server/missions.go` — remove `handleRecordPrompt` handler and route
- `internal/server/client.go` — remove `RecordPrompt` client method

### Sort behavior

| Scenario | Sort key |
|----------|----------|
| Mission running (wrapper alive) | `last_heartbeat` (updated every 10s) |
| Mission stopped (has run before) | `last_heartbeat` (frozen at last heartbeat) |
| Mission never started | `created_at` |

### Performance

Heartbeat writes go from 1/min to 6/min per mission. Each is a single `UPDATE ... SET last_heartbeat = ? WHERE id = ?` on an indexed column. Negligible overhead even with many concurrent missions.
