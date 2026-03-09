Remove "Daemon" Language
========================

Status: Approved
Date: 2026-03-09

Problem
-------

The codebase still contains "daemon" language from before the daemon-to-server migration. This creates confusion — readers encounter "daemon" references alongside "server" references and cannot tell which is current. The deprecated `agenc daemon` CLI subcommand still exists as a shim, and stale comments throughout active code reference "the daemon" when they mean the server.

Additionally, the `daemon/` subdirectory under `$AGENC_DIRPATH` may still exist on users' machines from pre-server installations, leaving stale cruft on disk.

Design
------

### A. Delete the deprecated `agenc daemon` CLI subcommand

Remove all five `cmd/daemon*.go` files that implement the deprecated shim:
- `cmd/daemon.go`
- `cmd/daemon_start.go`
- `cmd/daemon_stop.go`
- `cmd/daemon_status.go`
- `cmd/daemon_restart.go`

Remove `daemonCmdStr` from `cmd/command_str_consts.go`. The "Daemon subcommands" comment block goes too (the constants `startCmdStr`, `restartCmdStr`, `statusCmdStr` are shared with the server commands and stay).

### B. Rename and consolidate the version-check file

Rename `cmd/daemon_version_check.go` to `cmd/server_version_check.go`. This file is primarily about server version checking; the stale-daemon cleanup is a small helper within it.

`stopStaleDaemon` and the `GetDaemon*` path helpers in `internal/config/config.go` are kept — they reference real filesystem paths (`~/.agenc/daemon/`) that may still exist on disk.

### C. Clean up the daemon directory on server start

Add a `cleanupDaemonDir` function that:
1. Calls `stopStaleDaemon` to kill any running daemon process
2. Calls `os.RemoveAll` on the `daemon/` directory

This runs inside `ensureServerRunning` (the common path for all implicit server starts) and `forkServer` (the explicit `agenc server start` path). All errors are silently ignored — daemon cleanup must never block server start.

Remove the existing `stopStaleDaemon` call from `checkServerVersion` since it is now covered by server startup.

Once this cleanup has been in the wild for a release or two, `GetDaemon*` path helpers, `stopStaleDaemon`, and `cleanupDaemonDir` can all be deleted.

### D. Fix stale "daemon" references in active code and docs

Update comments and documentation that say "daemon" when they mean "server":

**Active code:**
- `internal/server/server.go` — "formerly in the Daemon struct"
- `internal/server/template_updater.go` — "daemon keeps"
- `internal/server/cron_syncer.go` — "how often the daemon"
- `internal/server/keybindings_writer.go` — "daemon regenerates"
- `internal/database/migrations.go` — "daemon-driven"
- `internal/config/agenc_config.go` — "daemon keeps"
- `internal/config/agenc_config_test.go` — test fixture "Daemon logs" → "Server logs"

**Documentation:**
- `CLAUDE.md` — "daemon, wrapper" → "server, wrapper"
- `docs/system-architecture.md` — remove "formerly the daemon" parentheticals, update directory layout
- `docs/configuration.md` — replace daemon language with server language
- `README.md` — replace daemon references with server

**Left alone:** Historical spec and plan documents stay as-is since they describe past decisions.

Error Handling
--------------

All daemon cleanup errors are silently ignored, matching the existing pattern. The cleanup is best-effort and must never prevent the server from starting.

Testing
-------

No new tests. The daemon dir cleanup is a single `os.RemoveAll` call behind an error-swallowing wrapper — the risk of a bug is lower than the cost of maintaining a test for it.
