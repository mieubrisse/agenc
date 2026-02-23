Session Print & Mission Print Commands
=======================================

**Date:** 2026-02-21
**Type:** Feature
**Status:** Approved
**Related:** agenc-316

Purpose
-------

Allow agents to retrieve session history from other missions they launch.
Primary use case: agent-to-agent communication where one agent checks on the
recent state of another mission's session.

Commands
--------

### `agenc session print <session-uuid>`

Print raw JSONL transcript for a Claude session.

- **Argument:** Session UUID (required, no fzf picker)
- **Default:** Last 20 JSONL lines to stdout
- **Flags:**
  - `--tail N` — override default line count
  - `--all` — print entire session
- **Resolution:** Search `~/.claude/projects/*/` for `<session-uuid>.jsonl`

### `agenc mission print [mission-uuid]`

Print raw JSONL transcript for a mission's current session.

- **Argument:** Mission UUID (optional, supports short 8-char IDs)
- **No argument:** fzf picker (matches `mission inspect` pattern)
- **Default:** Last 20 JSONL lines to stdout
- **Flags:**
  - `--tail N` — override default line count
  - `--all` — print entire session
- **Resolution:**
  1. Resolve mission UUID via database
  2. Read `claude-config/.claude.json` to get `currentSessionId`
  3. Find and print that session's JSONL

### `mission inspect` Enhancement

Add a "Sessions" section listing all session UUIDs for the mission, with `*`
marking the current session.

Example output:

```
Sessions:    3 total
             * 18749fb5-02ba-4b19-b989-4e18fbf8ea92  (current)
               a1b2c3d4-5678-90ab-cdef-1234567890ab
               f9e8d7c6-b5a4-3210-fedc-ba9876543210
```

Output Format
-------------

- Raw JSONL to stdout, one JSON object per line
- No headers, no color, no decoration
- Each JSONL line counts as one "message" for `--tail` counting
- Designed for agent consumption — clean machine-readable output

Error Handling
--------------

| Scenario                                        | Behavior                             |
|-------------------------------------------------|--------------------------------------|
| Session UUID not found in `~/.claude/projects/` | Exit 1, error to stderr              |
| Mission UUID not found in database              | Exit 1, error to stderr              |
| Mission has no current session in `.claude.json` | Exit 1, error to stderr             |
| Mission has no project directory yet            | Exit 1, "no sessions for this mission" |
| JSONL file exists but is empty                  | Exit 0, print nothing                |
| `--tail 0`                                      | Error: invalid value                 |

File Changes
------------

### New files

- `cmd/session.go` — top-level `session` parent command
- `cmd/session_print.go` — `session print` subcommand
- `cmd/mission_print.go` — `mission print` subcommand

### Modified files

- `cmd/command_str_consts.go` — add `sessionCmdStr`, `printCmdStr` constants
- `cmd/root.go` — register `session` command
- `cmd/mission_inspect.go` — add session UUID listing
- `internal/session/session.go` — add `FindSessionJSONLPath()` and
  `ListSessionIDsForMission()` helpers

### No new dependencies

Uses existing `os`, `filepath`, `encoding/json`, and project helpers
(`stacktrace`, `cobra`, `config`, `claudeconfig`).

### No database schema changes

Session Resolution Logic
------------------------

### Finding a session JSONL by session UUID

Search all directories under `~/.claude/projects/` for a file named
`<session-uuid>.jsonl`. Return the first match.

### Finding current session for a mission

1. Resolve mission ID to full UUID via `db.ResolveMissionID()`
2. Build path: `<agenc-dir>/missions/<mission-uuid>/claude-config/.claude.json`
3. Parse JSON, find `projects[<agent-dirpath>].currentSessionId`
4. Return that session UUID

### Listing all sessions for a mission

1. Find the mission's project directory under `claude-config/projects/`
   (directory name contains the mission UUID)
2. List all `.jsonl` files in that directory
3. Strip `.jsonl` extension to get session UUIDs
4. Read `.claude.json` to identify which is current

Design Decisions
----------------

- **Each JSONL line = 1 message** for counting purposes. No semantic parsing of
  message types. Semantic filtering can be added later if needed.
- **No `--mission` flag** on `session print`. Separate commands for separate
  concerns: `session print` takes session IDs, `mission print` takes mission
  IDs.
- **No fzf picker on `session print`** because session UUIDs aren't browseable
  without mission context.
- **Raw output only** — no pretty-printing, no `--format` flag. Agents can pipe
  to `jq` or other tools. YAGNI.
- **Default 20 lines** balances recency with context for agent-to-agent checks.
