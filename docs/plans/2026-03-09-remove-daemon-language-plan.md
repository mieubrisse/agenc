# Remove "Daemon" Language — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Remove all "daemon" language from the codebase, replacing it with "server" where appropriate, and add daemon directory cleanup to server startup.

**Architecture:** The daemon-to-server migration already happened structurally. This plan removes the remaining linguistic artifacts: a deprecated CLI shim, stale comments, and outdated docs. It also adds cleanup logic that removes the `daemon/` directory from `$AGENC_DIRPATH` on server start.

**Tech Stack:** Go (Cobra CLI), Makefile build system

---

### Task 1: Delete the deprecated `agenc daemon` CLI subcommand

**Files:**
- Delete: `cmd/daemon.go`
- Delete: `cmd/daemon_start.go`
- Delete: `cmd/daemon_stop.go`
- Delete: `cmd/daemon_status.go`
- Delete: `cmd/daemon_restart.go`
- Modify: `cmd/command_str_consts.go`

**Step 1: Delete the five daemon command files**

```bash
rm cmd/daemon.go cmd/daemon_start.go cmd/daemon_stop.go cmd/daemon_status.go cmd/daemon_restart.go
```

**Step 2: Remove `daemonCmdStr` and update the comment in `cmd/command_str_consts.go`**

Remove line 16:

```go
	daemonCmdStr   = "daemon"
```

Change the comment on line 76 from `// Daemon subcommands` to `// Server subcommands` (these constants — `startCmdStr`, `restartCmdStr`, `statusCmdStr` — are shared with the server commands).

Also change the comment on line 114 from `// daemon status flags` to `// server status flags`.

**Step 3: Build and run tests**

```bash
make check
```

Expected: All tests pass. The daemon commands are no longer registered with Cobra.

**Step 4: Commit**

```bash
git add -A
git commit -m "Remove deprecated 'agenc daemon' CLI subcommand"
```

---

### Task 2: Rename `daemon_version_check.go` and add daemon dir cleanup

**Files:**
- Rename: `cmd/daemon_version_check.go` → `cmd/server_version_check.go`
- Modify: `cmd/server_start.go`
- Modify: `cmd/mission_helpers.go`
- Modify: `internal/config/config.go`

**Step 1: Rename the file**

```bash
git mv cmd/daemon_version_check.go cmd/server_version_check.go
```

**Step 2: Add `cleanupDaemonDir` to `cmd/server_version_check.go`**

Add a new function after `stopStaleDaemon`:

```go
// cleanupDaemonDir removes the legacy daemon/ directory from the agenc root.
// This cleans up files left behind by pre-server versions of agenc. All errors
// are silently ignored — cleanup must never block server start.
func cleanupDaemonDir(agencDirpath string) {
	stopStaleDaemon(agencDirpath)
	daemonDirpath := config.GetDaemonDirpath(agencDirpath)
	_ = os.RemoveAll(daemonDirpath)
}
```

Make sure `"os"` is in the import block (it already is).

**Step 3: Replace `stopStaleDaemon` call in `checkServerVersion` with `cleanupDaemonDir`**

In `cmd/server_version_check.go`, change line 20 from:

```go
	stopStaleDaemon(agencDirpath)
```

to:

```go
	cleanupDaemonDir(agencDirpath)
```

**Step 4: Add `cleanupDaemonDir` to `ensureServerRunning` in `cmd/server_start.go`**

Add `cleanupDaemonDir(agencDirpath)` as the first line of the function body (before the `IsRunning` check):

```go
func ensureServerRunning(agencDirpath string) {
	cleanupDaemonDir(agencDirpath)
	pidFilepath := config.GetServerPIDFilepath(agencDirpath)
	// ... rest unchanged
```

**Step 5: Stop creating the daemon directory in `EnsureDirStructure`**

In `internal/config/config.go`, remove `filepath.Join(agencDirpath, DaemonDirname)` from the `dirs` slice in `EnsureDirStructure` (line 85). The daemon directory should no longer be created on fresh installs.

**Step 6: Build and run tests**

```bash
make check
```

Expected: All tests pass.

**Step 7: Commit**

```bash
git add -A
git commit -m "Add daemon directory cleanup to server startup"
```

---

### Task 3: Fix stale "daemon" comments in active Go code

**Files:**
- Modify: `internal/server/server.go:31`
- Modify: `internal/server/template_updater.go:44`
- Modify: `internal/server/keybindings_writer.go:11-13`
- Modify: `internal/server/cron_syncer.go` (no daemon reference — was on `keybindings_writer.go`. Double-check during implementation.)
- Modify: `internal/database/migrations.go:224`
- Modify: `internal/config/agenc_config.go:368`
- Modify: `internal/tmux/keybindings.go:145`

**Step 1: Apply all comment fixes**

Each fix is a single-line or two-line comment update. Replace "daemon" with "server" in context:

| File | Line | Old | New |
|------|------|-----|-----|
| `internal/server/server.go` | 31 | `// Background loop state (formerly in the Daemon struct)` | `// Background loop state` |
| `internal/server/template_updater.go` | 44 | `// refreshDefaultBranchInterval controls how often (in cycles) the daemon` | `// refreshDefaultBranchInterval controls how often (in cycles) the server` |
| `internal/server/keybindings_writer.go` | 11-13 | `how often the daemon regenerates` / `daemon restart` | `how often the server regenerates` / `server restart` |
| `internal/database/migrations.go` | 224 | `for daemon-driven` | `for server-driven` |
| `internal/config/agenc_config.go` | 368 | `whether the daemon keeps` | `whether the server keeps` |
| `internal/tmux/keybindings.go` | 145 | `the daemon's` | `the server's` |

**Step 2: Build and run tests**

```bash
make check
```

**Step 3: Commit**

```bash
git add -A
git commit -m "Replace stale 'daemon' comments with 'server' in active code"
```

---

### Task 4: Fix daemon references in Cobra help strings

**Files:**
- Modify: `cmd/config_repo_config_set.go:33`
- Modify: `cmd/config_repo_config.go:15`
- Modify: `cmd/repo_add.go:31,39`

**Step 1: Update flag/help strings**

| File | Old text | New text |
|------|----------|----------|
| `cmd/config_repo_config_set.go:33` | `"keep this repo continuously synced by the daemon"` | `"keep this repo continuously synced by the server"` |
| `cmd/config_repo_config.go:15` | `daemon keeps the repo continuously fetched` | `server keeps the repo continuously fetched` |
| `cmd/repo_add.go:31` | `synced by the daemon.` | `synced by the server.` |
| `cmd/repo_add.go:39` | `"keep this repo continuously synced by the daemon"` | `"keep this repo continuously synced by the server"` |

**Step 2: Regenerate CLI docs**

The CLI docs under `docs/cli/` are auto-generated. Regenerate them:

```bash
go run ./cmd/gendocs
```

Verify the daemon references are gone from `docs/cli/agenc_config_repoConfig_set.md`, `docs/cli/agenc_config_repoConfig.md`, and `docs/cli/agenc_repo_add.md`.

**Step 3: Build and run tests**

```bash
make check
```

**Step 4: Commit**

```bash
git add -A
git commit -m "Replace 'daemon' with 'server' in CLI help strings"
```

---

### Task 5: Update test fixture

**Files:**
- Modify: `internal/config/agenc_config_test.go:643-644`

**Step 1: Update the test fixture**

Change:

```go
				Title:   StringPtr("📋 Daemon logs"),
				Command: StringPtr("agenc tmux window new -- agenc daemon logs"),
```

to:

```go
				Title:   StringPtr("📋 Server logs"),
				Command: StringPtr("agenc tmux window new -- agenc server logs"),
```

**Step 2: Run tests**

```bash
make check
```

**Step 3: Commit**

```bash
git add -A
git commit -m "Update test fixture to use 'server' instead of 'daemon'"
```

---

### Task 6: Update documentation

**Files:**
- Modify: `CLAUDE.md`
- Modify: `README.md`
- Modify: `docs/system-architecture.md`
- Modify: `docs/configuration.md`
- Modify: `docs/metrics-baseline.md`

**Step 1: Update `CLAUDE.md`**

Change line 12 from:

```
- Changing process boundaries (CLI, daemon, wrapper) or their goroutine structure
```

to:

```
- Changing process boundaries (CLI, server, wrapper) or their goroutine structure
```

**Step 2: Update `README.md`**

Multiple changes:

1. Line 352: `so the daemon knows` → `so the server knows`
2. Lines 358-368: Replace the "Daemon" section header and body:
   - `### Daemon` → `### Server`
   - `The **daemon** is a background process` → `The **server** is a background process`
   - `the daemon auto-commits` → `the server auto-commits`
   - `The daemon starts automatically when you run most \`agenc\` commands. If it crashes, just restart it with \`agenc daemon restart\`` → `The server starts automatically when you run most \`agenc\` commands. If it crashes, just restart it with \`agenc server stop\` then \`agenc server start\``
3. Line 374: `The daemon keeps the library fresh` → `The server keeps the library fresh`
4. Lines 438-442: Update uninstall section:
   - `agenc daemon stop` → `agenc server stop`
   - `This stops the agenc daemon and removes` → `This stops the agenc server and removes`

**Step 3: Update `docs/system-architecture.md`**

1. Line 110: Remove `. The \`agenc daemon\` subcommand is deprecated and delegates to \`agenc server\`.` — just end the sentence after "socket file".
2. Line 114: `The server runs eight concurrent background goroutines (formerly the daemon):` → `The server runs eight concurrent background goroutines:`
3. Line 260: Remove `│   ├── daemon/                   # Deprecated (replaced by server)` entirely
4. Lines 331-334: Remove the entire `daemon/` directory block from the runtime directory layout
5. Line 386: `HTTP API server that listens on a unix socket. Serves mission lifecycle endpoints and runs background maintenance loops (formerly the daemon).` → Remove ` (formerly the daemon)`.

**Step 4: Update `docs/configuration.md`**

1. Line 20: `# daemon fetches every 60s` → `# server fetches every 60s`
2. Lines 64-65: Change the example palette command:
   - `title: "Daemon logs"` → `title: "Server logs"`
   - `command: "agenc daemon logs"` → `command: "agenc server logs"`
3. Line 86: `the daemon keeps the repo` → `the server keeps the repo`
4. Line 118: `The daemon evaluates cron expressions` → `The server evaluates cron expressions`
5. Line 236: `the daemon automatically commits` → `the server automatically commits`

**Step 5: Update `docs/metrics-baseline.md`**

1. Line 18: `github.com/odyssey/agenc/internal/daemon | 14.6%` — This package no longer exists. Remove the row entirely.
2. Line 38: Remove `daemon (14.6%),` from the bullet point.
3. Line 113: Remove `daemon: 14.6% → target 60%+` line.

**Step 6: Build and run tests**

```bash
make check
```

**Step 7: Commit**

```bash
git add -A
git commit -m "Replace 'daemon' with 'server' in documentation"
```

---

### Task 7: Final verification

**Step 1: Search for any remaining "daemon" references in active code**

```bash
grep -ri "daemon" --include="*.go" cmd/ internal/
```

Expected: Only `stopStaleDaemon`, `cleanupDaemonDir`, and the `GetDaemon*`/`Daemon*` constants in `internal/config/config.go` — all of which are intentionally kept for legacy cleanup.

**Step 2: Push**

```bash
git pull --rebase
git push
```
