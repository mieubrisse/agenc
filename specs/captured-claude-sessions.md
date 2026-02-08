Captured Claude Sessions
========================

Status: Design Draft


Problem
-------

Today, AgenC missions are opaque from the outside. The conversation history lives in Claude Code's internal JSONL files, and the repo state evolves without per-turn snapshots. This means:

- You can't see what the agent did at each turn without scrolling through the terminal
- You can't fork a conversation at a specific past turn to explore an alternative approach
- You can't reproduce a specific turn's starting state (code + conversation) for debugging

The hyperspace spec (`specs/hyperspace.md`) describes a broader vision around state tuples, friction capture, and tmux integration. This spec focuses narrowly on the **first concrete workflow**: `agenc mission fork`.


Goal
----

Enable `agenc mission fork <mission-id>`, which:

1. Shows an fzf picker of all conversation turns in the source mission
2. When selected, creates a new mission whose repo is checked out at that turn's code state
3. Starts a Claude session that resumes the conversation up to (and including) that turn


Claude Code Primitives
----------------------

Claude Code provides three flags that make this possible:

| Flag | Purpose |
|---|---|
| `claude -r SESSION_UUID` / `--resume SESSION_UUID` | Resume a conversation by its session ID |
| `--fork-session` | When resuming, create a new session ID instead of reusing the original |
| `--session-id <uuid>` | Use a specific session ID for the conversation |

The combination `claude -r SESSION_UUID --fork-session` resumes a conversation's full history but under a new session ID — the original session is untouched. This is the primitive for forking.

**Open question:** `-r` resumes the *entire* conversation. To fork at a specific *turn* (not just the end), we likely need to truncate the JSONL history file before resuming. Claude Code stores conversation history as `~/.claude/projects/<project-id>/<session-id>.jsonl`. The fork flow would:

1. Copy the source session's JSONL file
2. Truncate it at the desired turn boundary
3. Resume from the truncated copy via `--fork-session`

This requires understanding the JSONL line structure well enough to identify turn boundaries. If truncation proves fragile, an alternative is to always fork from the latest turn (simpler but less powerful).


Architecture
------------

### Per-Mission Branches

Each mission gets its own branch on the target repo:

```
agenc/session/<short-id>     # e.g., agenc/session/a1b2c3d4
```

This is already specified in `specs/hyperspace.md`. The branch is the mission's workspace — all code changes happen here, pushed to remote.

### Auto-Commit After Every Turn

A Claude Code hook commits all workspace changes after every agent turn. This produces one git commit per turn, which is the key mechanism that makes forking possible — each commit is a forkable checkpoint.

The hook is injected via AgenC's existing hook injection in `internal/daemon/claude_config_sync.go`:

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "cd \"$CLAUDE_PROJECT_DIR\" && git add -A && git diff --cached --quiet || git commit -m \"turn $(git rev-list --count HEAD)\""
          }
        ]
      }
    ]
  }
}
```

Each commit message includes the turn number for easy identification in the fzf picker.

**Auto-push** follows each commit to ensure no work is lost (missions are ephemeral).

### Session UUID Tracking

AgenC must track the Claude Code session UUID for each mission. This is the key that links a mission to its conversation history.

The session UUID appears in:
- The JSONL filename: `~/.claude/projects/<project-id>/<session-uuid>.jsonl`
- The `sessions-index.json` file

AgenC already resolves session data in `internal/session/session.go` by scanning the project directory for files matching the mission ID. The session UUID can be extracted from the JSONL filename and stored in the database.

**Database change:** Add a `session_uuid` column to the `missions` table (or reuse/rename the existing `session_name` column). This is populated after the first turn completes.

### Turn Enumeration

Each git commit on the mission's branch represents one turn. The fork picker needs to display these as a selectable list.

Data available per turn:
- **Commit hash** — the code state
- **Commit message** — includes turn number
- **Commit timestamp** — when the turn completed
- **Diff summary** — files changed (from `git diff --stat`)

The fzf picker shows something like:

```
Turn 12  2m ago   +3 -1  internal/auth/handler.go, cmd/login.go
Turn 11  5m ago   +47 -12  internal/database/migrations.go
Turn 10  8m ago   +2 -2  go.mod, go.sum
...
```


Fork Workflow
-------------

### `agenc mission fork <mission-id>`

1. **Resolve source mission** — look up the mission in the database, get its repo, branch, and session UUID

2. **Enumerate turns** — `git log` on the source mission's branch, one commit per turn. Format for fzf display.

3. **fzf picker** — user selects a turn (commit)

4. **Create new mission** — new database entry, new mission directory

5. **Branch from fork point** — create a new branch from the selected commit:
   ```
   agenc/session/<new-short-id>
   ```
   The new branch starts at the selected commit. The source branch is untouched.

6. **Prepare conversation history** — copy the source session's JSONL file. If forking at a turn earlier than the latest, truncate to that turn boundary.

7. **Launch Claude** — start the wrapper with:
   ```
   claude -r <source-session-uuid> --fork-session
   ```
   This resumes from the (possibly truncated) conversation history under a new session ID. The new mission's `session_uuid` is updated after launch.


Open Questions
--------------

### JSONL truncation for mid-conversation forks

Forking at the latest turn is straightforward — just `--fork-session` with the full history. Forking at an earlier turn requires truncating the JSONL file to exclude turns after the fork point.

Questions:
- What is the JSONL line structure for turn boundaries? Are there clear delimiters between user/assistant exchanges?
- Is there a risk of corrupting the session state by truncating? (e.g., missing summary entries, tool state)
- Should we start with "fork from latest only" and add mid-conversation forking later?

### Commit-to-turn mapping

The auto-commit hook produces one commit per turn, but we need to verify this 1:1 mapping is reliable:
- What if the agent makes no file changes on a turn? (no commit produced)
- What if the user manually commits between turns?
- Should we tag commits with metadata (e.g., `git notes`) to explicitly mark them as turn boundaries?

### Session UUID discovery

The session UUID isn't known until Claude starts and creates its first JSONL entry. Options:
- Use `--session-id <uuid>` to pre-assign a UUID at launch time (gives us control)
- Discover the UUID after the first turn by scanning the project directory (current approach for session names)

Pre-assigning via `--session-id` is cleaner and avoids the race condition of "when does the JSONL file appear?"

### Relationship to hyperspace turns table

The hyperspace spec describes a `turns` table that captures a full state tuple (repo commit, config commit, CLI invocation, session ID). This spec's auto-commit-per-turn mechanism produces the same data but stores it in git history rather than a database table.

Options:
- **Git-only:** Turn history lives entirely in git commits. The fzf picker reads `git log`. Simple, no database changes beyond `session_uuid`.
- **Database + git:** The `turns` table records each turn with its commit hash, enabling richer queries (e.g., "which turns had friction annotations?"). More infrastructure but enables the full hyperspace vision.

For the initial implementation, git-only is sufficient. The turns table can be added later when friction capture needs it.


Incremental Build Order
-----------------------

1. **Per-mission branches** — `mission new` creates `agenc/session/<short-id>` branch
2. **Auto-commit hook** — inject Stop hook that commits after every turn
3. **Auto-push** — push after each commit
4. **Session UUID tracking** — store session UUID in database (prefer `--session-id` pre-assignment)
5. **`mission fork` (latest turn)** — fork from the most recent turn (no JSONL truncation needed)
6. **`mission fork` (any turn)** — add turn picker and JSONL truncation for mid-conversation forks


Related Specs
-------------

- `specs/hyperspace.md` — broader vision including state tuples, friction capture, tmux control plane
- `specs/repo-direct-checkout.md` — repo cloned directly into `agent/` (prerequisite — already implemented)
- `specs/working-with-git-repos.md` — how AgenC manages repo clones
