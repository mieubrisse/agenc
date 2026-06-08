Watcher Migration: `fsnotify` → `rjeczalik/notify`
====================================================

Date: 2026-06-08
Bead: `agenc-ku7h` (primary FD-leak bug)
Related: `agenc-88uu` (downstream `git_corrupt` misclassification), `agenc-jcvv` (broader writeable-copy reconcile exploration)
Mission: ab2584f3-1f48-43e8-a61e-e05cfb493707
Session: 0f366f73

Motivation
----------

The AgenC server crashes when its open-FD count exceeds the system limit. On 2026-06-07 it lost ~19 minutes to "too many open files" before being killed by SIGTERM. Investigation on 2026-06-08 found the live server holding **118,144 open FDs** (94k regular files, 23k directories) — 94k of them under `/Users/odyssey/code/alembiq/app/node_modules/`.

Root cause: `internal/server/writeable_copies_watcher.go:200 addWatchesRecursiveExcludingGit` walks the working tree of each writeable-copy and calls `fsnotify.Watcher.Add()` on every subdirectory, excluding only `.git/`. On macOS, fsnotify backs Watch by kqueue, which holds one FD per watched directory **plus** one FD per file inside each watched directory. Recursively watching a repo containing `node_modules/` consumes tens of thousands of FDs per writeable-copy. The same architectural bug class exists in `internal/server/config_watcher.go:125 addTrackedWatches`, which recursively walks the user's `~/.claude/` tracked directories — currently bounded by `~/.claude/skills/`, `~/.claude/hooks/`, etc. staying small, but the latent failure mode is identical.

The fix is to stop asking macOS kqueue to track every file we don't care about, and to drop in a watcher library that uses macOS FSEvents recursively (one stream per repo, zero per-directory FDs) on the relevant platform.

Decision
--------

**Migrate every `fsnotify` call site in the codebase to `github.com/rjeczalik/notify`** and drop the `fsnotify` dependency. On macOS, recursive watches go through FSEvents (path-based, FD-cheap); non-recursive watches behave identically to today. On Linux the library uses inotify (same backend shape as `fsnotify`), kept in scope but not the focus of this fix.

For the writeable-copy watcher specifically, also add `github.com/go-git/go-git/v5/plumbing/format/gitignore` as a runtime post-filter — events whose path matches the repo's gitignore (nested `.gitignore` files, `.git/info/exclude`, global excludes) are discarded before debouncing. This eliminates redundant reconciles on uninteresting files (build artifacts, lockfiles, etc.) and keeps the design correct if/when an unusual project layout adds a non-trivially-large gitignored tree.

Scope
-----

Five `fsnotify.NewWatcher()` instances migrate, covering six logical Add patterns. Note that `config_watcher.go` uses a single Watcher to watch both a single file and a recursive tracked-directory set — that Watcher becomes two `notify.Watch(...)` calls sharing one event channel under the new library.

| File | Shape | Notes |
|---|---|---|
| `internal/server/writeable_copies_watcher.go:79` (working-tree Watcher) | **Recursive** (working tree) | FSEvents-recursive on macOS. Gitignore post-filter added. |
| `internal/server/writeable_copies_watcher.go:144` (ref Watcher) | Non-recursive (`.git/refs/remotes/origin/<branch>`) | Mechanical translation. |
| `internal/server/config_watcher.go:52` (combined Watcher) | **Mixed**: single-file (`config.yml`) + recursive (`~/.claude` tracked dirs) | Splits into two `notify.Watch` calls sharing one event channel. FSEvents-recursive on macOS for the tracked-dirs leg. No gitignore filter (not a git working tree). |
| `internal/wrapper/credential_sync.go:149` | Non-recursive (`agencDirpath`) | Mechanical translation. |
| `internal/wrapper/wrapper.go:678` | Non-recursive (`refsDirpath`) | Mechanical translation. |

After migration, `fsnotify` is removed from `go.mod` / `go.sum`.

Out of scope
------------

- **Linux scaling.** Linux inotify uses per-directory watches and has a per-user limit (~8192 default). Today's writeable-copies fit; if they ever don't, the escalation path is Watchman-as-subprocess, matching what jj/Mercurial/git's own fsmonitor use. Tracked elsewhere if it ever bites.
- **Restructuring the writeable-copy reconcile loop.** Adopting GitJournal/git-auto-sync's writeable-leg patterns and kubernetes/git-sync's atomic-symlink-flip for the read-only mirror leg are valid follow-ups (tracked in `agenc-jcvv`) but explicitly not coupled to this fix.
- **The `git_corrupt` misclassification downstream of EMFILE** (`agenc-88uu`). Will be addressed separately; preventing the FD exhaustion that triggers it is sufficient defense for now.

Architecture
------------

### Recursive watchers (writeable-copy working tree, `~/.claude` tracked dirs)

Single `notify.Watch(rootPath+"/...", eventCh, notify.Create|notify.Write|notify.Remove|notify.Rename)` per watcher. On macOS this opens one FSEvents stream per root — no per-directory FDs. New directories created at runtime (e.g., a fresh `npm install`) are picked up automatically by FSEvents; the existing fsnotify `Create`-event re-walk machinery (`writeable_copies_watcher.go:106-108`, `config_watcher.go`'s recursive add) is deleted.

Cleanup is `notify.Stop(eventCh)` on context cancellation, matching the existing teardown shape.

### Gitignore post-filter (writeable-copy only)

At watcher startup, load the repo's gitignore matcher using `go-git`'s `gitignore.ReadPatterns(billyFs, nil)`. The matcher composes nested `.gitignore` files, `.git/info/exclude`, and the global `core.excludesFile` in correct precedence — same code that backs go-git's own tree walks.

On each incoming event, split the event path into segments (relative to the repo root), call `matcher.Match(segments, event.IsDir())`, and discard the event if matched-as-ignored. This is CPU-only work, runs on the event-receive goroutine, and adds no FDs.

The matcher is loaded once per watcher lifetime. It is read-only after construction, so the event-receive goroutine can call `Match` without synchronization. If the user edits `.gitignore` during a session, the change takes effect on the next watcher restart — a known minor staleness window, acceptable given how rarely `.gitignore` changes in practice. A more sophisticated implementation would re-load on `.gitignore` Write events; not required for this fix.

### Simple watchers (single file, single dir)

Mechanical translation. `fsnotify.NewWatcher() + Add(path)` becomes `notify.Watch(path, eventCh, eventMask)`. `Events`/`Errors` channels become a single `EventInfo` channel; an event's `.Event()` returns the platform-independent event type and `.Path()` the affected path. Error handling around watcher creation simplifies (no separate error channel; setup errors come back from `Watch` directly).

Event type translation table:

| fsnotify | rjeczalik/notify |
|---|---|
| `fsnotify.Create` | `notify.Create` |
| `fsnotify.Write` | `notify.Write` |
| `fsnotify.Remove` | `notify.Remove` |
| `fsnotify.Rename` | `notify.Rename` |
| `event.Op & fsnotify.Write != 0` | `event.Event() & notify.Write != 0` (or check `==`) |

Testing
-------

**Unit tests** for the gitignore post-filter: fixture repo with a representative `.gitignore` (entries for `node_modules/`, `dist/`, plus a negation), feed synthetic events through the filter, assert which pass through. Lives next to the watcher in `internal/server/`.

**E2E test** in `scripts/e2e-test.sh`: spin up a test writeable-copy with a synthetic `node_modules`-like ignored directory (a few hundred fake files is enough — we're testing the filter, not load), touch a file inside it, assert no reconcile fires. As a defensive lower bound, also assert the open-FD count of the test-env server stays under 1,000 after creating the writeable-copy — a direct regression guard for this incident. The threshold is conservative; baseline healthy FD usage today is in the low hundreds, and the bug took the count into the six-figure range.

**Manual verification before declaring done:** run the production binary against the real environment for ~30 minutes with the real alembiq writeable-copy attached, then `lsof -p <pid> | wc -l` and confirm the FD count is in single-digit thousands instead of six-digit thousands.

Migration sequence
------------------

1. Add `github.com/rjeczalik/notify` and `github.com/go-git/go-git/v5/plumbing/format/gitignore` dependencies; run `go mod tidy`.
2. Migrate the four simple watchers first (smallest diff per file, lowest risk, gives confidence in the API mapping):
   `internal/wrapper/credential_sync.go`, `internal/wrapper/wrapper.go`, `internal/server/config_watcher.go:64`, `internal/server/writeable_copies_watcher.go:144`.
3. Migrate `config_watcher.go`'s recursive watcher (no gitignore concern, simpler than the writeable-copy case).
4. Migrate `writeable_copies_watcher.go`'s recursive watcher + add the gitignore matcher.
5. Remove `fsnotify` from `go.mod`; `go mod tidy`.
6. Run `make check` and `make e2e`.
7. Manual `lsof` verification against real environment.

Rollback
--------

If a critical regression surfaces after deploy, the rollback is a `git revert` of the migration commits — the fsnotify dependency comes back, code returns to the leak-state-but-functional behavior. The leak is gradual (hours), not a hard fault, so a controlled rollback window is realistic.

Open questions
--------------

None blocking. Behavior of `rjeczalik/notify`'s Linux backend for the recursive case (does it auto-add new dirs the same way fsnotify-based libraries do, or does it require explicit re-walks?) is irrelevant on macOS where we use FSEvents-recursive; verify when implementation lands and document any gotcha in code comments.
