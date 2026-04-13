Devcontainer Mission Isolation
==============================

Problem
-------

AgenC missions constantly trigger Claude Code's permission prompts — Bash commands, network requests, Python scripts, etc. This blocks agents and exhausts users. The current filesystem isolation (ephemeral per-mission directories) prevents accidental cross-mission damage but doesn't provide the safety boundary needed to eliminate permission prompts. A rogue command could damage the host filesystem, and prompt injection from web content could cause malicious actions.

Goal: make it safe to grant broad permissions by running missions inside containers, so the container boundary — not Claude Code's permission system — provides the safety net.

Design
------

### Core principle

Containerization is **per-repo opt-in**. A repo with a `devcontainer.json` gets containerized. A repo without one runs exactly as today. AgenC is an operational overlay on the repo's devcontainer — it doesn't define the environment, just adds its plumbing.

```
Repo has devcontainer.json?
  YES → Merge AgenC overlay → devcontainer up → wrapper(container(claude))
  NO  → Current behavior (no container)
```

### Lifecycle hierarchy: wrapper(container(claude))

Three components with strict nesting. Container exists if and only if wrapper is running.

| Action            | wrapper  | container      | claude   |
|-------------------|----------|----------------|----------|
| Mission start     | starts   | starts         | starts   |
| Reload Mission    | stays    | stays          | restarts |
| Rebuild Container | stays    | restarts       | restarts |
| Idle kill / stop  | stops    | stops          | stops    |
| Mission resume    | starts   | starts         | starts   |
| Mission rm        | stops    | stops + removed| stops    |

### Wrapper owns all container lifecycle

The server does not know about containerization. It spawns a wrapper in a tmux pane, same as today. The wrapper detects a devcontainer.json in the repo and manages the container:

```
wrapper startup:
  1. Check: does agent/.devcontainer/devcontainer.json exist?
  2. If no → non-containerized path (unchanged)
  3. If yes:
     a. Generate merged devcontainer overlay config
     b. Start wrapper socket server (creates wrapper.sock)
     c. devcontainer up (start container)
     d. Generate claude-config (no symlinks, curl hooks)
     e. devcontainer exec -- claude <args>

reload mission (Claude restart):
  1. Kill current devcontainer exec
  2. Regenerate claude-config (picks up latest global config)
  3. New devcontainer exec -- claude <args>
  4. Container stays running (dev processes intact)

rebuild container:
  1. Kill current devcontainer exec
  2. devcontainer stop → docker rm
  3. devcontainer up --build
  4. Regenerate claude-config
  5. New devcontainer exec -- claude <args>

wrapper shutdown (idle killer, manual, error):
  1. Kill devcontainer exec
  2. devcontainer stop
  3. Wrapper exits
```

### What AgenC overlays

The user's devcontainer.json defines the environment (image, tools, Claude CLI, cache volumes, features). AgenC only adds operational plumbing:

**Mounts:**

| Mount | Source (host) | Target (container) | Mode |
|-------|--------------|-------------------|------|
| Claude config (base) | `~/.agenc/missions/<uuid>/claude-config` | `~/.claude` | read-write |
| CLAUDE.md (protected) | `claude-config/CLAUDE.md` | `~/.claude/CLAUDE.md` | read-only |
| settings.json (protected) | `claude-config/settings.json` | `~/.claude/settings.json` | read-only |
| Session projects | `~/.claude/projects/<host-encoded>` | `~/.claude/projects/<container-encoded>` | read-write |
| Todos | `~/.claude/todos` | `~/.claude/todos` | read-write |
| Tasks | `~/.claude/tasks` | `~/.claude/tasks` | read-write |
| Debug | `~/.claude/debug` | `~/.claude/debug` | read-write |
| File history | `~/.claude/file-history` | `~/.claude/file-history` | read-write |
| Shell snapshots | `~/.claude/shell-snapshots` | `~/.claude/shell-snapshots` | read-write |
| Wrapper socket | `~/.agenc/missions/<uuid>/wrapper.sock` | `/var/run/agenc/wrapper.sock` | read-write |

**Environment variables:**

- `AGENC_MISSION_UUID` — mission identifier
- `AGENC_WRAPPER_SOCKET=/var/run/agenc/wrapper.sock` — hook target
- `CLAUDE_CODE_OAUTH_TOKEN` — Claude API auth

No `CLAUDE_CONFIG_DIR` override. Claude uses default `~/.claude/` discovery, which resolves to the bind-mounted claude-config.

**Mount rationale:**

- Claude-config as `~/.claude` base: gives Claude its config (CLAUDE.md, settings.json, skills, hooks) via default discovery
- CLAUDE.md and settings.json as read-only overlays: prevents missions from editing their own config
- `.claude.json` writable: Claude writes trust entries and other runtime state here
- Data directory overlays (todos, tasks, debug, etc.): all use UUIDs for namespacing, no collision across missions, data persists on host
- Session projects encoding translation: Claude inside the container writes to `~/.claude/projects/<container-encoded>/` but the bind mount redirects to `~/.claude/projects/<host-encoded>/` on the host, so the session scanner works unchanged

### Config regeneration on every Claude spawn

Claude-config is regenerated right before each `devcontainer exec -- claude` call. This means:
- Latest global CLAUDE.md and settings changes take effect on reload
- Container mode config (no symlinks, curl hooks) is generated at the right time
- No stale config across Claude restarts

The container's bind mounts are set once at `devcontainer up` and don't change on Claude reload. Only the content of the mounted claude-config changes.

### Hook communication

Hooks inside the container use curl to reach the wrapper on the host:

```bash
# Stop hook
curl -s --unix-socket $AGENC_WRAPPER_SOCKET \
  -X POST http://w/claude-update/Stop -d @- || true

# Notification hook
curl -s --unix-socket $AGENC_WRAPPER_SOCKET \
  -X POST http://w/claude-update/Notification -d @- || true
```

Event type is in the URL path. Body is raw Claude hook JSON, forwarded as-is. Wrapper parses server-side. No agenc binary needed in the container — only curl.

The wrapper socket is bind-mounted from the host at `/var/run/agenc/wrapper.sock`, following the Docker convention for socket mounts.

`AGENC_WRAPPER_SOCKET` env var tells hooks where to find the socket. For non-containerized missions, the same env var is set to the host path. Hooks that fire outside AgenC (no env var set) fail silently (`|| true`).

### Devcontainer config merging

AgenC reads the repo's devcontainer.json, adds its overlay (mounts, env vars), and writes a merged config.

**Path resolution**: Relative paths in devcontainer.json (`build.dockerfile`, `build.context`, `dockerComposeFile`) are resolved relative to the config file's location. Since AgenC writes the merged config to a different directory than the original, all relative paths are absolutized during the merge.

**Config location**: `~/.agenc/missions/<uuid>/devcontainer.json` (outside the repo, no git contamination risk).

**CLI invocation**: `devcontainer up --workspace-folder <agent-dir> --config <merged-config-path>`

**Research result (Task 11)**: The devcontainer CLI supports `--mount` (repeatable, additive) and `--remote-env` (repeatable) flags that could inject mounts/env without JSON merge. However, `--override-config` replaces the config entirely (not a merge). Mount ordering matters for the read-only overlay pattern (CLAUDE.md/settings.json mounted atop claude-config directory), and `--remote-env` sets `remoteEnv` not `containerEnv`. The JSON merge approach is retained for reliability, particularly to guarantee mount ordering for the overlay pattern. The CLI flags may be adopted as a simplification in a future iteration.

**Merge rules:**

| Field | Rule | Rationale |
|-------|------|-----------|
| `image` / `build` | Repo wins | Repo defines environment |
| `features` | Repo wins | AgenC has no features to inject |
| `mounts` | Concatenate (repo's + AgenC's) | Different targets, no conflicts |
| `containerEnv` | AgenC overrides on conflict | Operational vars must be correct |
| `workspaceFolder` | Repo wins | Repo controls mount target |
| `postCreateCommand` | Repo wins | AgenC doesn't inject lifecycle commands (v1) |
| `forwardPorts` | Repo wins | AgenC doesn't need ports |
| `remoteUser` / `containerUser` | Repo wins | Repo knows its needs |

**Deferred**: Docker Compose-backed devcontainers (`dockerComposeFile`). v1 supports `image`-based and `build`-based configs only.

### Session persistence

Claude encodes the working directory path into a project directory name under `~/.claude/projects/`. Inside the container, the workspace is at a path like `/workspaces/<repo>`. On the host, the mission's agent directory is at `~/.agenc/missions/<uuid>/agent/`.

AgenC computes both encoded paths and creates a bind mount that translates:

```
Container writes to: ~/.claude/projects/-workspaces-<repo>/
Which is actually:   ~/.claude/projects/-Users-odyssey--agenc-missions-<uuid>-agent/ (on host)
```

The session scanner on the host reads from the host-encoded path — unchanged.

Other data directories (todos, tasks, debug, file-history, shell-snapshots) use UUIDs for file naming and are safe to share across missions. They're bind-mounted directly from the host.

### User responsibilities

The user provides (in their repo's devcontainer.json):
- Image or Dockerfile
- Claude CLI installation (devcontainer feature or postCreateCommand)
- Language toolchains and tools
- Cache volumes (go-cache, npm-cache, etc.)

The user configures (in their Claude settings):
- Permission levels (e.g., `permissions.allow: ["*"]` if they want zero prompts)

### Error handling

| Scenario | Behavior |
|----------|----------|
| Docker Desktop not running | `devcontainer up` fails. Error shown in tmux pane. Wrapper exits. User starts Docker, runs `agenc mission resume`. |
| `devcontainer` CLI not installed | Wrapper detects missing binary, displays install instructions, exits. |
| Image pull / build fails | `devcontainer up` fails. Error shown in tmux pane. Wrapper exits. User fixes Dockerfile, resumes. |
| Claude CLI not in container | `devcontainer exec -- claude` fails. Error shown. User adds Claude to devcontainer.json, runs rebuild. |
| Container crashes | `devcontainer exec` exits unexpectedly. Wrapper detects, exits. Resume restarts container. |
| Hooks fail (socket not mounted) | Hooks fail silently (`|| true`). Wrapper misses idle detection events. Logged in wrapper.log. |
| Host project dir missing for bind mount | AgenC creates directory before `devcontainer up`. |
| Devcontainer.json malformed | `devcontainer up` fails. Error shown. Wrapper exits. |

Principle: errors cause wrapper exit. Resume retries. Rebuild forces clean slate.

### Testing

**Unit tests** (no Docker required):

- Devcontainer detection: find devcontainer.json in various repo layouts
- Overlay generation: merge AgenC config into various repo configs, verify path absolutization
- Project path encoding: compute host↔container path mappings
- Claude-config container mode: verify no symlinks, curl hooks generated

**Manual testing**:

- Full lifecycle: create containerized mission, verify container starts, Claude runs, hooks work, session files land on host
- Reload: verify container persists, Claude restarts
- Rebuild: verify container recreated, Claude starts fresh
- Idle kill: verify container stops with wrapper
- Resume: verify container restarts, Claude resumes
- Error recovery: Docker not running, bad Dockerfile, missing CLI

Components
----------

| # | Component | Location | What |
|---|-----------|----------|------|
| 1 | Devcontainer detection | `internal/devcontainer/detection.go` | Check repo for devcontainer.json |
| 2 | Devcontainer overlay generation | `internal/devcontainer/overlay.go` | Merge AgenC mounts/env into repo config, absolutize paths |
| 3 | Project path encoding | `internal/devcontainer/project_path_encoding.go` | Compute host↔container project directory mapping |
| 4 | Wrapper devcontainer lifecycle | `internal/wrapper/devcontainer.go` | `devcontainer up/exec/stop`, idempotent management |
| 5 | Claude config container mode | `internal/claudeconfig/build.go` | No symlinks for containerized missions; curl-based hooks |
| 6 | Claude config regeneration on spawn | `internal/wrapper/wrapper.go` | Regenerate claude-config before each Claude spawn |
| 7 | Mission removal cleanup | `internal/server/missions.go` | Stop and remove container on mission rm |
| 8 | Mission rebuild command | `cmd/mission_rebuild.go` | Rebuild container from updated devcontainer.json |
| 9 | Tmux palette: Rebuild Container | Tmux keybinding config | New palette action for containerized missions |
