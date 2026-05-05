Writeable Copies & Notifications — Design
==========================================

Provenance
----------

- Designed in AgenC mission `18449652-9fc7-4f46-b241-ca396fff291f`, session `6badf313`. Run `agenc session print 6badf313 --all` for the full discussion.
- Earlier exploration of the underlying problem: AgenC mission `25ee8b3d-e0b6-4a2f-909f-7011915666bb`. Run `agenc mission print 25ee8b3d-e0b6-4a2f-909f-7011915666bb` for that conversation.

Problem
-------

The user's dotfiles repo lives in the AgenC repo library at `~/.agenc/repos/github.com/mieubrisse/dotfiles/`, and `~/.claude/CLAUDE.md` plus the global skills are symlinks into it. The repo library is read-only by design: the server's repo update worker hard-resets every clone to `origin/<default-branch>` on every sync cycle, so any in-place edits are wiped.

Agents naturally want to edit `~/.claude/CLAUDE.md` directly, but those edits get destroyed. The only way to safely edit the dotfiles is to spawn a mission targeting the dotfiles repo — slow, clunky, and fights against how agents work.

Goal
----

Let the user designate a path on disk as a **writeable copy** of a repo — an additional clone that AgenC keeps continuously synced with the same git remote. Edits made there (by agents or humans) are auto-committed and pushed; remote changes are pulled and reconciled. Symlinks point at the writeable copy. Conflicts halt the sync loop and surface to the user via a new **notifications** subsystem.

The repo library remains the master and the only source missions clone from. The writeable copy is a peer of the library that synchronizes via the shared git remote.

Architecture
------------

```
                                   ┌──────────────────┐
                                   │   Git Remote     │  (e.g. github.com)
                                   └────────┬─────────┘
                                ┌───────────┴───────────┐
                                ↓                       ↓
                  ┌────────────────────┐    ┌────────────────────┐
                  │   Repo library     │    │  Writeable copy    │
                  │   ~/.agenc/repos/… │←───│  ~/app/dotfiles    │
                  │   (read-only)      │    │  (read-write,      │
                  │                    │    │   symlink target)  │
                  └─────────┬──────────┘    └─────────┬──────────┘
                            │                         │
                            │   fan-out trigger       │   fsnotify on
                            │   on library update     │   working tree
                            ↓                         ↓
                  ┌─────────────────────────────────────────────┐
                  │           AgenC Server background loops      │
                  │                                              │
                  │  • repo update worker (existing)             │
                  │  • writeable-copy reconcile worker (NEW)     │
                  │  • notifications subsystem (NEW)             │
                  └─────────────────────────────────────────────┘
                                       │
                              writes   ↓   reads
                            ┌─────────────────────┐
                            │  database.sqlite    │
                            │                     │
                            │  notifications      │  (NEW table)
                            │  writeable_copy_    │
                            │    pauses           │  (NEW table)
                            └─────────────────────┘
```

Three triggers drive the writeable-copy reconcile worker:

1. **fsnotify on the working tree**, debounced 15s (working-tree edits → commit + push)
2. **Library update fan-out** — when the existing repo update worker successfully updates a repo with a writeable copy, it enqueues a reconcile request (remote changes → pull + rebase)
3. **Server startup** — one reconcile pass per writeable copy (crash recovery)

The "writeable-copy push triggers library refresh" half is **free** — the same fsnotify-on-`refs/remotes/origin/<branch>` machinery that wrappers use today fires when the writeable copy successfully pushes, posting to `/repos/<name>/push-event` and refreshing the library via the existing worker.

Components
----------

### Configuration

`internal/config/agenc_config.go` — add a single field to `repoConfig`:

```yaml
repoConfig:
  github.com/mieubrisse/dotfiles:
    writeableCopy: /Users/odyssey/app/dotfiles    # NEW. Empty = none.
    alwaysSynced: true                             # implied; coerced at config load
    emoji: 🐚
    title: Dotfiles
```

Setting `writeableCopy` non-empty implies `alwaysSynced: true`. Coerced both at config-file load (defense in depth) and rejected at the CLI when a user tries to set them inconsistently.

### Database — two new tables (one migration)

```sql
CREATE TABLE notifications (
    id              TEXT    PRIMARY KEY,    -- UUID
    kind            TEXT    NOT NULL,
    source_repo     TEXT,
    title           TEXT    NOT NULL,
    body_markdown   TEXT    NOT NULL,
    created_at      DATETIME NOT NULL,
    read_at         DATETIME
);
CREATE INDEX idx_notifications_unread ON notifications(read_at) WHERE read_at IS NULL;

CREATE TABLE writeable_copy_pauses (
    repo_name              TEXT    PRIMARY KEY,
    paused_at              DATETIME NOT NULL,
    paused_reason          TEXT    NOT NULL,    -- e.g. "rebase_conflict", "auth_fail"
    local_head_at_pause    TEXT    NOT NULL,    -- SHA, for resume detection
    notification_id        TEXT    NOT NULL REFERENCES notifications(id)
);
```

Notifications are **append-only**: the only mutation is mark-as-read (sets `read_at`). No deletion. Pauses are deleted when the loop auto-resumes.

### Server code

- `internal/server/writeable_copies.go` — reconcile worker, tick logic, fsnotify watchers, boot reconcile, pause persistence.
- `internal/server/notifications.go` — DB CRUD + HTTP handlers.
- `internal/server/repo_update_worker.go` — existing file, ~5 line addition: after a successful `ForceUpdateRepo` for a repo with a writeable copy, enqueue a reconcile request.

New HTTP endpoints:
- `GET /notifications?unread=true` — list (default unread)
- `POST /notifications` — create
- `POST /notifications/{id}/read` — mark as read

### CLI

```
agenc repo writeable-copy set <repo> <path>     # configure & clone-if-needed
agenc repo writeable-copy unset <repo>          # remove from config (does NOT delete on-disk clone)
agenc repo writeable-copy ls                    # list with status

agenc notifications ls                          # default: unread, table
agenc notifications ls --all                    # full history
agenc notifications ls --repo <name>            # filter by source repo
agenc notifications ls --kind <kind>            # filter by kind
agenc notifications show <id>                   # full Markdown body to stdout
agenc notifications read <id>                   # mark as read
agenc notifications create                      # for agents; --kind, --title, --body or --body-file=-
```

ID display reuses `database.ShortID()` — first 8 hex chars; resolution accepts full UUID or 8-char prefix, error on ambiguity (matching mission ID semantics).

### Adjutant integration

A new section in `internal/claudeconfig/adjutant_claude.md` explains the notifications system, the conflict-resolution recipe, and when to mark on the user's behalf. A new palette entry "Show Notifications" spawns Adjutant with `agenc notifications ls` injected as the initial prompt. The palette footer shows `⚠ N unread notifications` when N > 0; suppressed when 0.

Data flow
---------

### Trigger paths

```
fsnotify (working tree, 15s debounce)  ┐
library-update fan-out                 ├──→ writeableCopyReconcileCh ──→ reconcile worker
server startup                         ┘            (buffered, 16)            │
                                                                              ↓
                                                                     run tick (per-repo flock)
```

### Tick state machine

```
                    ┌─────────────────────────┐
                    │  Tick fires for repo R  │
                    └────────────┬────────────┘
                                 ↓
                  ┌──────────────────────────────┐
                  │  Read pause state from DB    │
                  └──────────────┬───────────────┘
                       paused?      not paused
                          │              │
                          ↓              ↓
              ┌──────────────────┐    sanity checks
              │ Resume probe:    │       │
              │ tree clean +     │       ↓ pass / fail
              │ HEAD != pause.   │  ┌────────────────┐
              │ local_head?      │  │  commit local  │
              └────┬─────────┬───┘  │  if dirty      │
            yes  → │       no│      └────┬───────────┘
                   ↓         ↓           ↓
            delete pause   exit       fetch + reconcile:
                                      equal | ahead | behind | diverged
                                          ↓
                                  on rebase fail / non-FF / auth fail:
                                          ↓
                                  insert notification + insert pause row
                                          (single transaction)
```

### Atomic pause + notification

```go
func postWriteableCopyConflict(ctx, repo, kind, title, body, localHead) {
    tx := db.Begin()
    defer tx.Rollback()

    // If a pause row already exists for this repo, the conflict was already
    // posted. Skip — no duplicate notification.
    if pauseExists(tx, repo) {
        return
    }

    notif_id := uuid.New()
    insertNotification(tx, notif_id, kind, repo, title, body)
    insertPause(tx, repo, paused_at=now, reason=kind,
                local_head_at_pause=localHead, notification_id=notif_id)
    tx.Commit()
}
```

This guarantees one pause → one notification, regardless of crashes or restarts. No `divergence_signature` is needed — pause persistence is the dedupe mechanism.

### Resume condition

When a tick fires and the pause row exists:

```
git status --porcelain produces no output   AND
git rev-parse HEAD ≠ pause.local_head_at_pause
```

On clear: delete the pause row in a transaction. The notification stays — it's append-only.

Error handling
--------------

### Path validation (at `set` time, once)

Reject if any holds:

- Path is relative after `~` expansion and `filepath.Abs`
- Path is under `agencDirpath`
- Path is under another configured writeable copy
- `filepath.EvalSymlinks(path)` differs from path AND target is under `agencDirpath`
- Parent directory does not exist
- Repo is not in the library
- Repo already has a writeable copy configured

When path exists and is the right repo at the right URL: adopt. When path exists but isn't a git repo or doesn't match: error with self-serve recovery instructions (different path, or remove existing).

### Tick sanity checks (every tick, before mutation)

| Condition | Action |
|-----------|--------|
| `.git/index.lock` exists | abort tick silently, retry next trigger |
| Rebase/merge/cherry-pick in progress | abort tick silently, retry next trigger |
| Working tree has conflict markers (`UU`, `AA`, etc.) | abort tick silently, retry next trigger |
| HEAD not on default branch | pause + notify |
| `.git/` corrupt (`rev-parse` fails) | pause + notify |
| Working tree path missing | pause + notify |
| `git remote get-url origin` doesn't match registered URL | pause + notify |

### Sync errors

| Error | Action |
|-------|--------|
| `git fetch` transient (single failure) | log, no notification, retry next trigger |
| `git fetch` failed 10 times across ≥5 minutes | pause + notify (`writeable_copy.fetch_failure`) |
| `git rebase` conflict | abort, pause + notify (`writeable_copy.conflict`), body lists conflicted files + diverging commits + recipe |
| `git push` rejected non-fast-forward | pause + notify (`writeable_copy.non_ff_reject`) |
| `git push` auth failure | pause + notify (`writeable_copy.auth_failure`) with credential-renewal recipe |
| Disk full during commit/clone | pause + notify (`writeable_copy.disk_error`) after sustained failure |

### Notification body cap

Bodies enforced ≤ 256KB at the API/CLI boundary. Larger bodies truncated with footer `\n\n---\n*[truncated: original was N bytes]*`. Titles containing newlines are rejected.

### ANSI sanitization

`agenc notifications show <id>` reads `body_markdown` from DB, strips ANSI escape sequences (regex: `\x1b\[[0-9;]*[a-zA-Z]` and `\x1b\][^\x07]*\x07`) before writing to stdout. The stored body is unchanged — sanitization is at display time only, preserving the authored content for archival or alternate renderers.

### Server startup

```
1. Probe `git --version`. Fatal log if missing.
2. For each writeable copy in config:
   a. Validate on-disk state (path exists, is a git repo, origin matches).
      Failure → post a notification, do not block server startup.
   b. Install fsnotify watchers (working tree + .git/refs/remotes/origin/<default>).
   c. Enqueue one reconcile request (boot reconcile).
3. Start reconcile worker goroutine.
```

Paused writeable copies stay paused across restart (pause is in DB). The boot reconcile fires for them too, but the resume probe runs first and either confirms still-paused or detects local resolution.

### CLI errors and self-serve

Every error path provides the exact command(s) to recover. Examples:

- Repo not in library → "Add it first: `agenc repo add <repo>`"
- Path collision → list of three numbered alternatives
- Disabling `alwaysSynced` while a writeable copy is set → "Remove the writeable copy first: `agenc repo writeable-copy unset <repo>`"

Testing
-------

### Unit tests

- **Path validation** (`internal/config/writeable_copy_path_test.go`): all rejection rules, expansion of `~`, symlink resolution, nested-path detection.
- **Config coercion** (`internal/config/agenc_config_test.go`): `WriteableCopy != "" → AlwaysSynced == true` regardless of file content.
- **ANSI strip** (`internal/server/notifications_strip_test.go`): regex against escape-injection corpus.
- **Pause + notification atomicity** (`internal/database/notifications_test.go`): concurrent calls produce at most one pause + one notification per repo.

### Tick logic tests

`internal/server/writeable_copies_test.go` — table-driven, with git invocation behind an injected interface so no real git calls. Cover:

- clean repo + clean remote → no-op
- dirty work tree → commit + push
- behind → fast-forward
- diverged + clean rebase → push succeeds
- diverged + conflict → abort + pause + notify
- non-FF push reject → pause + notify with distinct kind
- already-paused, HEAD unchanged → tick exits (resume probe fails)
- already-paused, HEAD moved + clean tree → resume (pause deleted)
- HEAD on non-default branch → pause + notify
- origin URL drift → pause + notify

### E2E tests (`scripts/e2e-test.sh`)

Use a local `bare.git` as origin so no GitHub round-trip:

- `agenc repo writeable-copy set` clones to the path
- Edit a file, sleep past debounce, verify auto-commit + push to bare.git
- `agenc repo writeable-copy ls` shows `ok` status
- Simulate a divergence by pushing a conflicting change to bare.git from a third clone, then commit locally; wait, verify `agenc notifications ls` shows the conflict
- Resolve the conflict manually, push, wait, verify `agenc repo writeable-copy ls` shows `ok` again
- Verify the notification persists (still readable via `agenc notifications ls --all`)

### Manual verification (cannot be E2E'd — per CLAUDE.md)

- Edit a file via Finder/editor, confirm auto-commit fires within ~20s
- Open command palette, confirm footer shows `⚠ N unread` when notifications exist
- Run "Show Notifications" palette entry, confirm Adjutant launches with the listing as prompt
- Stop and restart the server while a writeable copy is paused, confirm pause survives and no duplicate notification is posted

Out of scope for v1
-------------------

- Persistent event/outbox store (relying on disk state + boot reconcile)
- Squash policy for auto-commits
- Multi-host concurrent-edit coordination beyond best-effort rebase
- Mission-launch blocking on writeable-copy push propagation (sub-second window in practice)
- Tmux status-bar notification indicator (palette footer is the only indicator)
- LFS or submodules in writeable copies (document as unsupported)
- Auto-archive of read notifications (revisit if table size becomes a problem)
- Per-host branches or merge-bot for multi-machine concurrent edits

Architecture-doc updates required
---------------------------------

When this lands, update `docs/system-architecture.md`:

- Add the writeable-copy reconcile worker to the background-loops list
- Add the writeable-copy push-event watch to the IPC table (mirrors wrapper's existing entry)
- Document `notifications` and `writeable_copy_pauses` tables
- New "Writeable copies" section under Background loops, with the trigger diagram

CLI documentation under `docs/cli/` is auto-generated by `make build` from the Cobra command tree, so it picks up new commands automatically.
