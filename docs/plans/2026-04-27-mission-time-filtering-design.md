Mission Time-Based Filtering
=============================

Context: designed in AgenC mission `19d8e734-664d-4199-b6ee-7f2b7e6379fc`.

Problem
-------

Agents reading historical mission data need to scope queries to time ranges.
`ListMissions` currently filters on `IncludeArchived`, `Source`, and `SourceID`
but has no time-based filtering. Users must fetch all missions and filter
client-side.

Design
------

Add optional `--since` and `--until` flags to `agenc mission ls` that filter
on `created_at`. Changes span three layers: database, server API, and CLI.

### Database layer (`internal/database/`)

Add `Since *time.Time` and `Until *time.Time` to `ListMissionsParams`.
`buildListMissionsQuery` appends `created_at >= ?` and/or `created_at <= ?`
WHERE clauses when the fields are non-nil. Values are formatted as UTC RFC3339
strings for SQLite TEXT comparison (which sorts lexicographically).

No schema migration needed — `created_at` already exists and is covered by
`idx_missions_source`.

### Server API layer (`internal/server/`)

`handleListMissions` parses optional `since` and `until` query params as
RFC3339 strings and populates the corresponding `ListMissionsParams` fields.
Returns HTTP 400 for unparseable values.

`Client.ListMissions` is refactored from positional args
`(bool, string, string)` to a `ListMissionsRequest` struct, aligning with the
DB layer's existing `ListMissionsParams` pattern. All 15 client call sites in
`cmd/` are updated. `Since` and `Until` are serialized as RFC3339 query params
when non-nil.

### CLI layer (`cmd/mission_ls.go`)

New `--since` and `--until` string flags. A `parseTimeFlag` function tries
RFC3339 first, then falls back to `YYYY-MM-DD`:

- `--since YYYY-MM-DD` → start of day (`00:00:00`) in local timezone
- `--until YYYY-MM-DD` → end of day (`23:59:59`) in local timezone
- Full RFC3339 passes through with its explicit timezone

Validation: if `--since` is after `--until`, reject with an error before any
API call.

When either time filter is active, the 20-mission display limit is lifted (the
time bounds themselves scope the result).

Feedback messaging:

- With results: "Showing N missions created since X" / "between X and Y" / "until Y"
- Empty: "No missions found since X" / "between X and Y" / "until Y"

### Data flow

```
CLI flag string
  -> parseTimeFlag() -> *time.Time (local TZ for date-only, UTC for RFC3339)
  -> normalized to UTC on ListMissionsRequest
  -> Client serializes to RFC3339 query param (?since=2026-04-20T00:00:00Z)
  -> Server parses RFC3339 -> sets on ListMissionsParams
  -> buildListMissionsQuery appends "created_at >= ?" with UTC RFC3339 string
  -> SQLite TEXT comparison
```

### Error handling

| Error | Layer | Behavior |
|-------|-------|----------|
| Unparseable flag value | CLI | Error: `invalid --since value "foo": expected YYYY-MM-DD or RFC3339` |
| `--since` after `--until` | CLI | Error before API call |
| Invalid RFC3339 query param | Server | HTTP 400 |
| No results in time range | CLI | Friendly message, exit 0 |

### Testing

- **Unit** (`database_test.go`): Create missions at known times, verify
  `ListMissions` with `Since`, `Until`, and both returns correct subsets.
  Test composition with `IncludeArchived` and `Source`.
- **Unit** (CLI): Test `parseTimeFlag` — date-only produces correct
  local-timezone boundaries, RFC3339 passes through, invalid input errors.
- **E2E** (`scripts/e2e-test.sh`): Create a mission, verify `--since <today>`
  includes it, `--until <yesterday>` excludes it, and `--since` after `--until`
  produces an error.

### UX decisions

- Flag names: `--since`/`--until` (matches `git log`, `journalctl`, `docker logs`)
- Date-only input: local timezone (matches how humans read dates)
- Missing bound: unbounded (no WHERE clause, not "defaults to now/epoch")
- Display limit: lifted when time filters active
- Works independently of `--all` (time filters scope time, `--all` scopes status)

Files Changed
-------------

| File | Change |
|------|--------|
| `internal/database/missions.go` | Add `Since`/`Until` to `ListMissionsParams` |
| `internal/database/queries.go` | Add conditional WHERE clauses |
| `internal/database/database_test.go` | Time-range filtering tests |
| `internal/server/missions.go` | Parse `since`/`until` query params |
| `internal/server/client.go` | Refactor `ListMissions` to `ListMissionsRequest` struct |
| `cmd/mission_ls.go` | `--since`/`--until` flags, parsing, display changes |
| `cmd/cron_history.go` | Update call site |
| `cmd/cron_ls.go` | Update call site |
| `cmd/mission_archive.go` | Update call site |
| `cmd/mission_attach.go` | Update call site |
| `cmd/mission_detach.go` | Update call site |
| `cmd/mission_inspect.go` | Update call site |
| `cmd/mission_nuke.go` | Update call site |
| `cmd/mission_print.go` | Update call site |
| `cmd/mission_reload.go` | Update call site |
| `cmd/mission_rm.go` | Update call site |
| `cmd/mission_stop.go` | Update call site |
| `cmd/mission_update_config.go` | Update call site (x2) |
| `cmd/summary.go` | Update call site |
| `scripts/e2e-test.sh` | E2E tests for time filtering |
