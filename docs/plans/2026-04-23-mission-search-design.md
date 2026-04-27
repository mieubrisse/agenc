Mission Search with FTS5
========================

Status: Approved (revised 2026-04-27)
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

### Session Processing Architecture

The current session scanner tangles file watching with content processing. This design splits them into a clean three-layer architecture:

**Layer 1: File Watcher** (one goroutine, ~3s cadence)

The file watcher's only job is to discover JSONL files and track their current size. It updates a `known_file_size` column on the sessions table. This is where ALL file-discovery optimization lives — tmux pane lookup, directory walking, stat calls, and any future improvements (fsevents, inotify, etc.).

The file watcher does NOT read file content. It just tracks size.

**Layer 2: Consumers** (independent goroutines, each at their own cadence)

Each consumer has its own offset column on the sessions table. When it wakes up, it queries for sessions where `known_file_size > my_offset_column`. For each such session, it opens the file, seeks to its offset, reads to `known_file_size`, does its work, and advances its offset.

Consumers have no awareness of file paths, tmux panes, or filesystem mechanics. They just see "sessions with unprocessed content" via a DB query.

**Consumers:**

| Consumer | Offset Column | Cadence | Scope |
|----------|---------------|---------|-------|
| Title scanner | `last_title_update_offset` | ~3s | Sessions for running missions (title changes only happen while running) |
| FTS indexer | `last_indexed_offset` | ~30s | All sessions with unprocessed content |

**Layer 3: Sessions table** (the coordination point)

The sessions table gains:
- `known_file_size` — written by the file watcher, read by consumers
- `last_title_update_offset` — renamed from `last_scanned_offset`
- `last_indexed_offset` — new, default 0

Adding a future consumer = add a new offset column + a simple processing loop. No changes to the file watcher.

### Indexing Details

**Natural backfill:** On first deployment, all sessions have `last_indexed_offset = 0`. The FTS consumer progressively works through all historical content, limited only by its cadence and batch size.

**Atomic advancement:** Each consumer wraps its work + offset update in a single SQLite transaction. Either both succeed or neither does — no duplicates, no lost content.

**File path resolution:** Consumers compute JSONL file paths from session metadata: `mission_id` → agent dir → project dir → `{session_id}.jsonl`. This is pure string computation (no filesystem I/O).

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

- **Missing JSONL file:** File watcher skips silently. Consumers never see the session (known_file_size stays 0).
- **Corrupt JSONL entry:** Consumer skips the entry, continues with rest of file. Log warning.
- **FTS5 index corruption:** The FTS5 table is derived data — JSONL files are the source of truth. Resetting `last_indexed_offset` to 0 for all sessions triggers a full rebuild via the normal consumer loop.
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
- **Indexing:** Progressive backfill over multiple 30-second consumer cycles. Incremental updates negligible.
- **CPU during search:** Negligible. Read-only B-tree lookups, no write locks.
- **File watcher:** 10K stat calls every 3s for running missions ≈ ~10ms. Negligible.
