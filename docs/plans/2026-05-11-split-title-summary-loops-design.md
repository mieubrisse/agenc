Split Title/Summary Pipeline Into Two Independent Loops
========================================================

Context
-------

Mission `c0e36548` was running but never received an `auto_summary`. Investigation showed the underlying Claude Haiku CLI invocation got killed once (`signal: killed`, likely the 30s timeout), and the existing pipeline has no retry mechanism after that failure.

The root cause is a structural one in `internal/server/session_scanner.go`'s title-consumer loop:

1. It runs *one* scan per session that extracts two outputs (`custom_title`, `first_user_message`) and dispatches summarization asynchronously over a channel to `runSessionSummarizerWorker`.
2. It advances `last_title_update_offset` to `known_file_size` *immediately* after the scan, regardless of whether the asynchronous summarization succeeded.
3. The summarizer worker writes `auto_summary` independently when (and if) Haiku returns.

Consequence: if the Haiku call fails — channel-full drop, CLI killed, OAuth missing, response-too-long, any error — the offset has already advanced past the first user message and no future cycle can re-extract it. The failure is permanent.

By contrast, `InsertSearchContentAndUpdateOffset` in `internal/database/search.go:17-47` already gets this right for the search indexer: the FTS insert and the `last_indexed_offset` bump happen in a single transaction, so a failed write naturally rolls back the offset and the session is re-picked on the next cycle.

This design extends the search-indexer pattern to title and summary updates.

Goal
----

Make title extraction and auto-summarization independently retriable. Each operation either fully succeeds (its output and its offset advance together) or fully fails (offset stays put, retry next cycle). No async fire-and-forget. No shared offset.

Non-goals
---------

- Backfilling existing rows manually. Existing rows with non-empty `auto_summary` stay as-is; existing rows with empty `auto_summary` are naturally re-summarized as a side effect of the new column defaulting to 0.
- Implementing periodic re-summarization. The offset is sized to support that future work, but the trigger condition stays `auto_summary = ''` for now.
- Handling multimodal-only sessions (user typed only images). Out of scope.
- Changing the file watcher, the search indexer, the tmux reconciliation, or any consumer of `auto_summary` / `custom_title`.

Design
------

### Database schema

Migration in `internal/database/migrations.go` (extends the existing `migrateSearchIndex` or adds a new migration function — implementation detail):

- Rename column `sessions.last_title_update_offset` → `sessions.last_custom_title_scan_offset`. Existing values carry forward unchanged.
- Add column `sessions.last_auto_summary_scan_offset INTEGER NOT NULL DEFAULT 0`. Rows already summarized are filtered out by the `auto_summary = ''` clause in the new query, so the 0 default is harmless. Rows with empty `auto_summary` (e.g., `c90f07ce`) get offset 0 → automatically re-scanned and re-summarized.

Both DDL operations are idempotent (check column presence via `PRAGMA table_info` before applying), matching the existing migration style.

Update `internal/database/sessions.go`:

- `Session` struct: rename `LastTitleUpdateOffset` → `LastCustomTitleScanOffset`. Add `LastAutoSummaryScanOffset int64`.
- All SELECT statements that scan session rows: update column list and `Scan` calls.

### Database functions

In `internal/database/sessions.go`, replace:

- `UpdateSessionScanResults` → remove
- `SessionsNeedingTitleUpdate` → remove
- `UpdateSessionAutoSummary` → remove

Add:

- `SessionsNeedingCustomTitleUpdate() ([]*Session, error)` — `WHERE known_file_size IS NOT NULL AND known_file_size > last_custom_title_scan_offset`
- `SessionsNeedingAutoSummary() ([]*Session, error)` — `WHERE auto_summary = '' AND known_file_size IS NOT NULL AND known_file_size > last_auto_summary_scan_offset`
- `UpdateCustomTitleAndOffset(sessionID, customTitle string, newOffset int64) error` — single UPDATE: `SET custom_title = ?, last_custom_title_scan_offset = ?, updated_at = ?`
- `UpdateCustomTitleScanOffset(sessionID string, newOffset int64) error` — single UPDATE: `SET last_custom_title_scan_offset = ?, updated_at = ?`
- `UpdateAutoSummaryAndOffset(sessionID, summary string, newOffset int64) error` — single UPDATE: `SET auto_summary = ?, last_auto_summary_scan_offset = ?, updated_at = ?`
- `UpdateAutoSummaryScanOffset(sessionID string, newOffset int64) error` — single UPDATE: `SET last_auto_summary_scan_offset = ?, updated_at = ?`

Each update is a single statement — no explicit transaction needed (SQLite auto-commits the single statement).

### Server components

Remove from `internal/server/`:

- `session_summarizer.go`: the channel, the worker, the dedup sync.Map, the request type — `sessionSummaryCh`, `summarizedSessions`, `summaryRequest`, `requestSessionSummary`, `runSessionSummarizerWorker`, `initSessionSummarizer`, `summarizerChannelSize` constant
- `session_scanner.go`: `runTitleConsumerLoop`, `runTitleConsumerCycle`
- `server.go`: `sessionSummaryCh` and `summarizedSessions` fields on `Server`; `s.initSessionSummarizer()` call in `Start`; `runLoop("session-summarizer", ...)` and `runLoop("title-consumer", ...)` goroutine launches

Keep in `internal/server/session_summarizer.go` (or move to a more appropriate filename — implementation detail):

- `generateSessionSummary` — pure function, called directly by the new auto-summary loop
- `buildSummarizerSystemPrompt`
- Constants: `summarizerModel`, `summarizerTimeout`, `summarizerMaxPromptLen`, `summarizerMaxOutputLen`

Add to `internal/server/`:

- `runCustomTitleLoop(ctx context.Context)` and `runCustomTitleCycle()` (3-second ticker, mirroring `runSearchIndexerLoop`)
- `runAutoSummaryLoop(ctx context.Context)` and `runAutoSummaryCycle()` (3-second ticker)
- Goroutine launches: `runLoop("custom-title", ...)` and `runLoop("auto-summary", ...)` in `Server.Start`

### File-scan helpers

In `internal/server/session_scanner.go`, replace `scanJSONLFromOffset` (which conflated two scans) with two single-purpose helpers:

- `scanJSONLForCustomTitle(jsonlFilepath string, offset int64) (customTitle string, err error)` — reads offset → EOF, returns last `custom-title` metadata seen. Uses the existing `tryExtractCustomTitle` line filter.
- `scanJSONLForFirstUserMessage(jsonlFilepath string, offset int64) (msg string, err error)` — reads offset → EOF, returns on the first user-role line with string content. Uses the existing `tryExtractUserMessage` line filter, which correctly skips array-content lines (tool results, multimodal). The early-return makes this O(few lines) in the common case.

The offset parameter on `scanJSONLForFirstUserMessage` is meaningful: it supports the future periodic re-summarization use case. In the current one-shot case, the offset is 0 until success, then advances to `known_file_size`.

### Loop bodies

Auto-summary cycle (pseudocode reflecting the actual error-handling pattern):

```
for each sess in SessionsNeedingAutoSummary():
    path = resolveSessionJSONLPath(sess); if "" → continue
    knownSize = *sess.KnownFileSize  (snapshot from query)

    msg, err = scanJSONLForFirstUserMessage(path, sess.LastAutoSummaryScanOffset)
    if err: log; continue                        # I/O failure → retry next cycle

    if msg == "":
        UpdateAutoSummaryScanOffset(sess.ID, knownSize)
        continue                                  # no user message yet → advance and wait

    summary, err = generateSessionSummary(ctx, agencDirpath, msg, maxWords)
    if err: log; continue                         # Haiku failure → offset stays → retry

    UpdateAutoSummaryAndOffset(sess.ID, summary, knownSize)
    reconcileTmuxWindowTitle(sess.MissionID)
```

Custom-title cycle:

```
for each sess in SessionsNeedingCustomTitleUpdate():
    path = resolveSessionJSONLPath(sess); if "" → continue
    knownSize = *sess.KnownFileSize

    title, err = scanJSONLForCustomTitle(path, sess.LastCustomTitleScanOffset)
    if err: log; continue

    if title != "" and title != sess.CustomTitle:
        UpdateCustomTitleAndOffset(sess.ID, title, knownSize)
        reconcileTmuxWindowTitle(sess.MissionID)
    else:
        UpdateCustomTitleScanOffset(sess.ID, knownSize)
```

### Error handling — by enumerated case

| Case | Handling |
|------|----------|
| JSONL file doesn't exist (`resolveSessionJSONLPath` empty) | Skip silently — file watcher creates the row later. |
| Scan returns I/O error | Log with session ID; offset stays; retry next cycle. |
| Custom-title scan finds no metadata in new bytes | `UpdateCustomTitleScanOffset` only. |
| Custom-title scan finds title equal to existing `sess.CustomTitle` | `UpdateCustomTitleScanOffset` only (no spurious write, no tmux reconcile). |
| Auto-summary scan finds no first user message | `UpdateAutoSummaryScanOffset` only — session selected again when file grows. |
| `generateSessionSummary` fails (CLI killed, missing, OAuth invalid, empty / oversized response) | Log; offset stays; retry next cycle. **This is the bug fix.** |
| DB write fails (locked, disk full) | Log; nothing committed; retry next cycle. |
| Migration partial failure | Each step is idempotent against `PRAGMA table_info`; retry on next startup completes the missing step. |
| Existing buggy sessions post-migration | Naturally re-summarized — default offset 0 + empty `auto_summary` matches the new query. |

### Testing

E2E test (`scripts/e2e-test.sh`) — happy path:

- Create a mission, wait for the file watcher to populate `known_file_size`, send a first user prompt, wait for the auto-summary cycle (≤6s), assert `auto_summary` is non-empty in the DB.

Unit tests:

- `session_scanner_test.go`: `scanJSONLForFirstUserMessage` early-returns on first match; skips array-content user lines; returns empty when no string-content user line in offset range.
- `session_scanner_test.go`: `scanJSONLForCustomTitle` returns last `custom-title` metadata; returns empty when none in range.
- `session_scanner_test.go` (new) or via the integration tests: `runAutoSummaryCycle` with mocked summarizer-success → row updated, offset advanced; with mocked summarizer-failure → row NOT updated, offset NOT advanced; session re-selected on subsequent call.
- `session_scanner_test.go` (new): `runCustomTitleCycle` with title found / not found / unchanged → correct UPDATE path taken.
- Migration test (in `database/migrations_test.go` if it exists, otherwise via E2E): pre-migration row with `last_title_update_offset=42` and `auto_summary='X'` → post-migration row has `last_custom_title_scan_offset=42`, `last_auto_summary_scan_offset=0`, `auto_summary='X'` preserved.

Manual smoke test: open a mission via the palette, send a prompt, watch the tmux window title transition from "Repo Name" → auto-summary text within ~6 seconds.

### Architecture doc

Update `docs/system-architecture.md`:

- Replace component "5. Session summarizer worker" + "9. Title consumer" with two components: "Custom-title loop" and "Auto-summary loop." Each independent, DB-backed retry, no channel.
- Update the file listing under `internal/server/` to drop the channel/dedup-map description for `session_summarizer.go` and add the two new loops.

Implementation order
--------------------

1. Schema migration (rename + add column) + `Session` struct + SELECT scan updates.
2. New DB functions (queries + atomic update variants).
3. New scan helpers (`scanJSONLForCustomTitle`, `scanJSONLForFirstUserMessage`).
4. New server loops (`runCustomTitleCycle`, `runAutoSummaryCycle`).
5. Wire loops into `Server.Start`; remove old goroutines, channel, worker, dedup map.
6. Delete old DB functions + old loop functions + old scan helper.
7. Unit tests + E2E test.
8. Architecture doc update.
9. `make check` + `make e2e` + manual smoke test in `_test-env`.

Provenance
----------

Designed in AgenC mission `e62182e8-a318-4f6d-934e-40e3850add48`. Run `agenc mission print e62182e8` for the full discussion.
