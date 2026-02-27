Post-Update Hook Design
=======================

Bead: agent-egn

Overview
--------

Add a per-repo `postUpdateHook` field to RepoConfig that the daemon executes
in the repo library clone after each successful git pull where HEAD actually
changed. This allows repos to define setup commands (`make setup`, `npm
install`, etc.) that run automatically, so mission clones created via rsync
inherit a fully-configured working tree.

Config Schema
-------------

Add a `PostUpdateHook` string field to `RepoConfig` in
`internal/config/agenc_config.go`:

```yaml
repoConfig:
  github.com/owner/repo:
    alwaysSynced: true
    postUpdateHook: "make setup"
```

The field is a single shell command string executed via `sh -c`. Empty string
means no hook. The CLI gets a `--post-update-hook` flag on
`agenc config repoConfig set`.

Centralized Update Worker
-------------------------

Today, two independent codepaths call `ForceUpdateRepo`:

1. The 60-second cron ticker in `template_updater.go`
2. The push-event HTTP handler in `repos.go`

This design replaces both with a single **repo update worker** goroutine. Both
the cron ticker and push handler send update requests to the worker via a
channel instead of calling `ForceUpdateRepo` directly.

### Request type

```go
type repoUpdateRequest struct {
    repoName            string
    refreshDefaultBranch bool
    forceRunHook        bool
}
```

- `refreshDefaultBranch`: set by the cron ticker every 10th cycle
- `forceRunHook`: set after a fresh clone to run the hook even though HEAD
  didn't "change" from the worker's perspective

### Worker goroutine

Started in `Server.Run()` alongside existing background loops. Reads from the
channel and for each request:

1. Captures HEAD via `git rev-parse HEAD`
2. Calls `ForceUpdateRepo`
3. Captures HEAD again
4. If HEAD changed OR `forceRunHook`, runs the postUpdateHook (if configured)

### Debounce

No explicit dedup logic. If the same repo is requested twice (e.g., cron ticker
and push event fire simultaneously), the second `ForceUpdateRepo` finds HEAD
unchanged and skips the hook. Two redundant `git fetch` calls are harmless.

### Push handler changes

Changes from synchronous to fire-and-forget:

- Validates the repo name and checks the library directory exists
- Enqueues the update request
- Returns `202 Accepted`

### First clone

After `ensureRepoCloned` succeeds for a new repo, it enqueues a request with
`forceRunHook: true`. This ensures the hook runs on initial clone without
waiting for the next update cycle.

Hook Execution
--------------

The worker's hook runner:

1. Reads the agenc config to get the `PostUpdateHook` for the repo
2. If empty, returns immediately
3. Runs `sh -c "<hook>"` with working directory set to the repo library path
4. Inherits the daemon's environment as-is
5. **Timeout**: 30-minute hard timeout; WARN log emitted after 5 minutes of
   execution (and periodically thereafter)
6. **Error handling**: Hook failure does NOT fail the update. The repo is
   already updated; the hook is best-effort. Logs the error and stderr, then
   continues.
7. **Success logging**: Always logs a message on completion — success or failure

Testing
-------

Unit tests for:

- RepoConfig serialization round-trip with PostUpdateHook field
- Worker processes requests correctly (mock ForceUpdateRepo, verify hook runs
  when HEAD changes, skipped when unchanged)
- Hook runner executes `sh -c` with correct working directory, logs on failure,
  does not propagate errors
- CLI `--post-update-hook` flag on `config repoConfig set`

Real-world E2E: configure `postUpdateHook: "make setup"` on the
`mieubrisse/agenc` repo and validate via normal usage.

Files Changed
-------------

| File | Change |
|------|--------|
| `internal/config/agenc_config.go` | Add `PostUpdateHook` to `RepoConfig` |
| `internal/server/template_updater.go` | Add worker goroutine, refactor cron ticker to enqueue requests, refactor `ensureRepoCloned` to enqueue after clone |
| `internal/server/repos.go` | Refactor push handler to enqueue + return 202 |
| `internal/server/server.go` | Add channel + start worker goroutine in `Run()` |
| `cmd/config_repo_config_set.go` | Add `--post-update-hook` flag |
| `cmd/command_str_consts.go` | Add flag name constant |
| `docs/system-architecture.md` | Update daemon description to reflect worker goroutine |

Related Beads
-------------

- **agent-r4i** (closed): Pre-commit hook / `make setup` — provides the
  concrete command this hook would run
- **agent-xfm** (open, P4): Investigate whether persistence is needed for
  repo update requests across server restarts
