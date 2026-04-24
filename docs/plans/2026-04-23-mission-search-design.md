Mission Search with FTS5
========================

Status: Approved (revised 2026-04-24)
Date: 2026-04-23

Problem
-------

Finding missions is painful. The `mission attach` picker shows truncated auto-generated titles, fzf's client-side fuzzy matching doesn't work well when the user doesn't remember exact terms, and there's no way to search by conversation content. Users frequently lose track of missions and can't get back to them.

Design
------

### Approach

Native SQLite FTS5 full-text search over mission transcripts. No external dependencies. The FTS5 index lives in AgenC's existing SQLite database.

### What Gets Indexed

- Mission prompt (first user prompt, already cached in the `prompt` column)
- Session names (custom_title, agenc_custom_title, auto_summary)
- User messages from JSONL transcripts (full text)
- Assistant text blocks from JSONL transcripts (prose only — skip tool_use, thinking blocks, system messages)

Tool call results and thinking blocks are excluded — they're bulky and low-signal for topic search.

### FTS5 Schema

A virtual table in the existing database:

```sql
CREATE VIRTUAL TABLE mission_search_index USING fts5(
    mission_id UNINDEXED,
    session_id UNINDEXED,
    content,
    tokenize='porter unicode61'
);
```

One row per indexed message. `porter` tokenizer enables stemming ("authenticating" matches "authentication"). `unicode61` handles Unicode normalization.

### Indexing — Separate FTS Indexer Goroutine

The FTS indexer runs as its own server goroutine, independent of the existing session scanner. It iterates ALL sessions in the database (not just running ones), checks `last_indexed_offset` vs JSONL file size, and indexes any unprocessed content.

**Key properties:**
- **Separate offset column:** `last_indexed_offset` on the sessions table (default 0), independent of the existing `last_title_update_offset` (renamed from `last_scanned_offset`).
- **Atomic advancement:** FTS insert and offset update are wrapped in a single SQLite transaction. Either both succeed or neither does — no duplicates, no lost content.
- **Natural backfill:** On first deployment, all sessions have `last_indexed_offset = 0`, so the indexer progressively works through all historical content at its own pace. No explicit reindex command needed.
- **Cadence:** Runs every ~30 seconds (slower than the 3-second title scanner since search doesn't need real-time updates).
- **Scope:** Processes all sessions, including stopped/archived missions — unlike the title scanner which only processes running missions.

### Column Rename

`last_scanned_offset` → `last_title_update_offset`. This column is only used by the title/summary scanner. The rename makes the purpose clear now that a second offset (`last_indexed_offset`) exists alongside it.

### Search API

New server endpoint. Accepts a query string and limit. Runs:

```sql
SELECT mission_id, snippet(mission_search_index, 2, '[', ']', '...', 20) as snippet,
       bm25(mission_search_index) as rank
FROM mission_search_index
WHERE content MATCH ?
GROUP BY mission_id
ORDER BY rank
LIMIT ?
```

Enriches results with mission metadata (status, repo, session name) via JOIN or post-query lookup. Returns JSON array of ranked results with snippets.

User input is wrapped in double quotes by default to treat it as literal/phrase search, preventing accidental FTS5 operator syntax. Users can opt into operators if needed.

### CLI: `mission search <query>`

Programmatic search for agents and scripts. Calls the search API, returns ranked results. Supports `--json` for machine-readable output and `--limit N` for result count.

### Interactive Picker: Enhanced `mission attach`

Replaces the current fzf picker with a live-search experience using fzf's `--disabled` + `--bind 'change:reload(...)'` pattern:

- fzf opens. Empty query shows all missions sorted by recency (current behavior).
- As the user types, each keystroke triggers `agenc mission search-fzf {q}` via the reload binding.
- fzf displays FTS5-ranked results instead of doing its own fuzzy matching.
- Each result row shows: short ID, session name, repo, and a match snippet (showing why it matched).
- If input is a valid hex short ID, exact ID match floats to top.
- Enter attaches to the selected mission.

No debouncing needed — FTS5 queries at this scale return in single-digit milliseconds, well within the ~100-200ms between keystrokes.

### Error Handling

- **Missing JSONL file:** Skip silently during indexing, log at debug. Missions without transcripts appear in browse mode (recency) but not in content search.
- **Corrupt JSONL entry:** Skip entry, continue indexing rest of file. Log warning.
- **FTS5 index corruption:** The FTS5 table is derived data — JSONL files are the source of truth. Resetting `last_indexed_offset` to 0 for all sessions triggers a full rebuild.
- **Server not running:** `mission search` returns clear error. Interactive picker falls back to current behavior (load all missions, fzf client-side matching) so `mission attach` never breaks.
- **Query syntax:** User input wrapped in double quotes by default to prevent accidental FTS5 operator activation.

### Testing

- **Unit tests:** FTS5 indexing and querying with in-memory SQLite. Verify ranking, snippets, edge cases (empty query, no results, special characters, long messages).
- **E2E tests:** Create mission with known prompt → trigger indexing → `mission search` finds it → verify JSON output.

### Cost Profile

At 10K missions with user messages + assistant prose indexed:

- **Disk:** ~200-400MB FTS5 index
- **Memory:** Disk-backed, a few MB active during queries
- **Query latency:** Sub-10ms for FTS5, ~20-50ms end-to-end including process spawn and socket round-trip
- **Indexing:** Progressive backfill over multiple 30-second cycles. Incremental updates negligible.
- **CPU during search:** Negligible. Read-only B-tree lookups, no write locks.
