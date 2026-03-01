Claude Modifications Config API
================================

Problem
-------

AgenC stores per-installation Claude Code overrides in `~/.agenc/config/claude-modifications/` — a `CLAUDE.md` and `settings.json` that get merged into every mission's config. Currently, these files can only be modified by direct filesystem access. This creates two problems:

1. **Agents edit the wrong file.** The Adjutant, when asked to update "the CLAUDE.md," edits its own project-level `CLAUDE.md` instead of the claude-modifications one. There are no CLI commands to guide it to the right file.

2. **Race conditions.** Multiple agents (or an agent and the user) could modify these files concurrently with no coordination, leading to lost writes.

Decision
--------

Add server API endpoints and CLI commands for reading and writing the claude-modifications files. Writes use optimistic concurrency control: the caller provides a content hash from their last read, and the server rejects the write if the file has changed since then.

The CLI commands are `agenc config claude-md get/set` and `agenc config settings-json get/set`. The internal directory name (`claude-modifications`) is an implementation detail not exposed to users.

API Endpoints
-------------

| Method | Path | Purpose |
|--------|------|---------|
| `GET /config/claude-md` | Read CLAUDE.md content + content hash | |
| `PUT /config/claude-md` | Write CLAUDE.md with optimistic locking | |
| `GET /config/settings-json` | Read settings.json content + content hash | |
| `PUT /config/settings-json` | Write settings.json with optimistic locking | |

### GET /config/claude-md

Response (200):

```json
{"content": "...file content...", "contentHash": "a1b2c3d4e5f6..."}
```

`contentHash` is the hex-encoded SHA-256 of the file's current content. An empty file produces the SHA-256 of zero bytes.

### PUT /config/claude-md

Request:

```json
{"content": "new content here", "expectedHash": "a1b2c3d4e5f6..."}
```

Success response (200):

```json
{"contentHash": "d4e5f6a7b8c9..."}
```

Conflict response (409):

```json
{"message": "file has been modified since last read; run 'agenc config claude-md get' to fetch the current version, then retry your update"}
```

Server-side write flow:

1. Read current file, compute SHA-256
2. Compare against `expectedHash` — return 409 if mismatch
3. Write new content to `~/.agenc/config/claude-modifications/CLAUDE.md`
4. `git add` + `git commit` in the config repo (message: `Update claude-modifications/CLAUDE.md`)
5. Log the operation
6. Return new content hash

### GET /config/settings-json

Same pattern as `GET /config/claude-md`, reading `claude-modifications/settings.json`.

### PUT /config/settings-json

Same pattern as `PUT /config/claude-md`, with one addition: validates that the content is valid JSON before writing. Returns 400 if not.

CLI Commands
------------

### agenc config claude-md get

Calls `GET /config/claude-md` and renders a human-readable output:

```
Content-Hash: a1b2c3d4e5f6

--- Content ---
<raw file content, unescaped>
```

The hash is on a labeled first line. A `--- Content ---` header separates it from the raw content. No JSON wrapping — the file content is printed verbatim.

### agenc config claude-md set --content-hash=HASH

Reads new content from stdin. Calls `PUT /config/claude-md` with the content and hash.

Success output:

```
Updated CLAUDE.md (content hash: d4e5f6a7b8c9)
```

Conflict output:

```
Error: CLAUDE.md has been modified since last read.

To resolve:
  1. agenc config claude-md get    (fetch current content and hash)
  2. Re-apply your changes to the new content
  3. agenc config claude-md set --content-hash=<new-hash>
```

### agenc config settings-json get / set

Identical pattern to the claude-md commands, targeting `settings.json`. The `set` command validates JSON client-side before sending (fail fast).

Adjutant Prompt Updates
-----------------------

Replace the "Claude Modifications Directory" section in `adjutant_claude.md` with guidance that uses the CLI commands:

- Use `agenc config claude-md get` / `set` to manage AgenC-specific CLAUDE.md instructions
- Use `agenc config settings-json get` / `set` to manage AgenC-specific settings overrides
- Explain the content-hash flow: get returns a hash, set requires it, conflict means re-read and retry
- Explain what these files are (AgenC-specific overrides merged into every mission's config, separate from `~/.claude/`)
- Explain when changes take effect (new missions automatically; existing missions via `agenc mission reconfig`)
- Do NOT edit files directly — always use the CLI commands

Add "Managing AgenC-specific Claude instructions and settings" to the "What You Help With" list.

Prime Content
-------------

No changes needed. `genprime` introspects the Cobra command tree automatically, so the new subcommands appear in the CLI quick reference as soon as they're registered.

Concurrency Model
-----------------

The content hash is the SHA-256 of the file's raw bytes. This means:

- Changes to `CLAUDE.md` do not invalidate the hash for `settings.json` and vice versa
- The first writer to an empty file passes the hash of zero bytes (well-known value)
- Two readers who read the same content get the same hash — first writer wins, second gets 409
- The git commit on write is synchronous — the response is not sent until the commit completes

The git commit ensures durability and integrates with the existing config auto-sync (the server pushes the config repo periodically).
