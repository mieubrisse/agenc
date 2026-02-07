Per-Mission Claude Config Isolation and Tracking
=================================================

Problem
-------

Today, all AgenC missions share a single `CLAUDE_CONFIG_DIR` at `~/.agenc/claude/`. The daemon merges the user's `~/.claude/` with AgenC's operational modifications every 5 minutes. This creates three problems:

1. **No config capture** — Can't record which Claude config was active during a specific agent turn. No forensic trail for config-caused regressions.
2. **No session restoration** — Checking out a past session doesn't restore the Claude config it ran with.
3. **No update control** — Config changes propagate silently to all running missions.

Solution Overview
-----------------

Each mission gets its own `CLAUDE_CONFIG_DIR`, built from a user-registered config source repo. Config is pinned at mission creation and updated explicitly. The config source repo's commit hash is recorded for state capture.

Config Source Repo
------------------

### Registration

Users register a git repo and subdirectory containing their Claude config:

```
agenc config set claude-config-repo github.com/mieubrisse/dotfiles
agenc config set claude-config-subdir claude/
```

AgenC clones and periodically syncs this repo in `~/.agenc/repos/` (existing repo library infrastructure).

Only the default branch is tracked (no branch selection in v1).

### Subdirectory Is Practically Required

The config source repo **must** store Claude config in a subdirectory (e.g., `claude/`), not at the repo root. If `CLAUDE.md` or `settings.json` live at the root, Claude Code will import them as project-level config whenever anyone opens a mission targeting that repo — including when editing the dotfiles repo itself. The `claude-config-subdir` setting exists to handle this.

### Mandatory Requirement

Config source registration is mandatory. If a user runs `mission new` without a registered config repo, AgenC blocks and tells them to register one:

```
agenc config set claude-config-repo <repo>
agenc config set claude-config-subdir <subdir>
```

The repo must already exist and contain the expected config files in the specified subdirectory. AgenC does not create or scaffold config repos (this may be added later as a convenience command).

### What Gets Tracked

**Tracked files** (copied from config source subdir into each mission's config dir):

- `CLAUDE.md`
- `settings.json`
- `skills/`
- `hooks/`
- `commands/`
- `agents/`
- `plugins/`

**NOT tracked** (runtime/machine state, never copied):

- `.claude.json` — auth account identity (symlinked from user's Claude install; see Authentication)
- `.credentials.json` — auth tokens (symlinked from AgenC central copy; see Authentication)
- `history.jsonl`, `stats-cache.json`, `projects/` — machine-generated runtime

Per-Mission Config Directory
----------------------------

### Directory Structure

```
~/.agenc/missions/<uuid>/
├── agent/                          # repo checkout (after repo-direct-checkout)
│   └── .claude/                    # project-level Claude config (from repo)
│       └── settings.json
├── claude-config/                  # per-mission CLAUDE_CONFIG_DIR (NEW)
│   ├── .claude.json                # → symlink to ~/.claude/.claude.json
│   ├── .credentials.json           # → symlink to ~/.agenc/claude/.credentials.json
│   ├── CLAUDE.md                   # from config source + agenc modifications
│   ├── settings.json               # from config source + agenc modifications + hooks
│   ├── skills/                     # from config source
│   ├── hooks/                      # from config source
│   ├── commands/                   # from config source
│   ├── agents/                     # from config source
│   └── plugins/                    # from config source
├── config-commit                   # config source commit hash at last build
├── claude-state
├── pid
└── wrapper.log
```

### Two-Layer Config Model

- **Global layer** (`claude-config/`): User's CLAUDE.md, settings, skills, hooks — shared identity across projects
- **Project layer** (`agent/.claude/`): Repo-specific settings — checked into the target repo itself

Claude Code natively handles this layering: `CLAUDE_CONFIG_DIR` provides the global config, and `.claude/` in the working directory provides project overrides.

### Tool Approvals

Claude creates `$CLAUDE_CONFIG_DIR/projects/<hash>/` with per-project allowed tools and trust settings. With per-mission config dirs, each mission starts with a clean slate for tool approvals. This is intentional — each mission being explicit about permissions is safer.

### Config Source Checkout Strategy

The repo library (`~/.agenc/repos/`) holds the main clone of the config source repo. Each mission needs files from a specific commit of that repo. Two options:

**Option A: File copy.** At mission creation, copy trackable files from the repo library clone's subdir into `claude-config/`. Simple, but duplicates data.

**Option B: Git worktree.** Create a worktree from the repo library clone into the mission directory (e.g., `~/.agenc/missions/<uuid>/config-source/`). The repo library clone serves as the main working tree; each mission gets a lightweight worktree checked out at its pinned commit. AgenC reads from the worktree subdir and renders the final config (with modifications applied) into `claude-config/`.

Worktrees are appealing because:
- No file duplication — worktrees share the git object store with the main clone
- Each worktree can be independently pinned to a different commit
- `git rev-parse HEAD` in the worktree gives the commit hash directly
- Updating config is `git checkout <new-commit>` + re-render

With worktrees, the structure becomes:

```
~/.agenc/repos/github.com/owner/dotfiles/           # main clone (repo library)
~/.agenc/missions/<uuid>/
├── config-source/                                   # worktree from main clone
│   └── claude/                                      # subdir with user's config
├── claude-config/                                   # rendered CLAUDE_CONFIG_DIR
│   ├── .claude.json → ~/.claude/.claude.json
│   ├── .credentials.json → ~/.agenc/claude/.credentials.json  # symlink
│   ├── CLAUDE.md                                    # rendered: source + agenc mods
│   ├── settings.json                                # rendered: source + agenc mods + hooks
│   ├── skills/ → ../config-source/claude/skills/    # symlink to worktree (optional)
│   └── ...
├── config-commit
└── ...
```

Decision: Start with file copy (simpler). Consider worktrees as an optimization later if duplication becomes a concern.

Mission Creation Flow
---------------------

When `mission new` is called:

1. Verify `claude-config-repo` is registered (block and prompt if not)
2. Ensure config source repo is cloned and up-to-date in `~/.agenc/repos/`
3. Create standard mission directory structure
4. **Build mission config dir** (`BuildMissionConfigDir()`):
   a. Create `~/.agenc/missions/<uuid>/claude-config/`
   b. Copy trackable files from config source repo subdir into `claude-config/`
   c. Apply AgenC modifications:
      - Append AgenC's CLAUDE.md content to the user's CLAUDE.md
      - Deep-merge AgenC's settings.json into the user's settings.json
      - Append AgenC operational hooks (Stop/UserPromptSubmit for state tracking)
   d. Symlink `.claude.json` → `~/.claude/.claude.json` (fallback: `~/.claude.json`)
   e. Ensure central credentials exist at `~/.agenc/claude/.credentials.json` (see Authentication)
   f. Symlink `.credentials.json` → `~/.agenc/claude/.credentials.json`
   g. Record config source HEAD commit hash to `config-commit` file
5. Launch wrapper with `CLAUDE_CONFIG_DIR=~/.agenc/missions/<uuid>/claude-config/`

Config Update Flow
------------------

Config is **pinned** at mission creation. Updates are explicit:

```
agenc mission update-config [mission-id]
```

This command:
1. Fetches latest from config source repo (`git pull` in repo library)
2. Shows diff between pinned commit and HEAD (filtered to config subdir)
3. Asks user to confirm
4. Re-copies trackable files into mission's `claude-config/`
5. Re-applies AgenC modifications (hooks, merged CLAUDE.md, merged settings.json)
6. Updates `config-commit` file
7. If mission is running, signals wrapper to restart Claude (deferred until idle)

**Batch update:** `agenc mission update-config --all` updates all active missions.

Authentication
--------------

No `agenc login` command is needed. AgenC relies on the user having already logged in via `claude login` (which stores credentials in the OS and creates `.claude.json`).

### `.claude.json` (account identity)

- Contains the `oauthAccount` block (account UUID, email, org info) — needed for Claude to identify the logged-in user
- Each mission's `claude-config/.claude.json` is a **symlink** to the user's existing file
- Lookup order: `~/.claude/.claude.json` (primary), `~/.claude.json` (fallback)
- If neither exists, mission creation fails with a message telling the user to run `claude login` first

### `.credentials.json` (auth tokens)

Claude Code reads auth tokens from `$CLAUDE_CONFIG_DIR/.credentials.json`. On macOS, the canonical source is the Keychain; on Linux, the file is the only source. AgenC manages a **central copy** and **symlinks** it into each mission's config dir.

**Central copy location:** `~/.agenc/claude/.credentials.json` (mode `0600`)

**macOS — dumping from Keychain:**

```bash
security find-generic-password -a "$USER" -w -s "Claude Code-credentials" \
  > ~/.agenc/claude/.credentials.json
chmod 600 ~/.agenc/claude/.credentials.json
```

AgenC runs this dump once when it first needs the credentials (e.g., during mission creation). If the central copy already exists and is non-empty, the dump is skipped. If the Keychain entry doesn't exist, mission creation fails with a message telling the user to run `claude login` first.

**Linux — copying from default location:**

```bash
cp ~/.claude/.credentials.json ~/.agenc/claude/.credentials.json
chmod 600 ~/.agenc/claude/.credentials.json
```

**Per-mission linkage:**

Each mission's `claude-config/.credentials.json` is a **symlink** to the central copy. Updating the central copy (e.g., after token refresh) is visible to all missions immediately.

**Token refresh:** If credentials expire or are rotated (user re-runs `claude login`), AgenC re-dumps from Keychain (macOS) or re-copies (Linux) to the central location. All missions see the update via the symlink. This can be triggered manually via `agenc auth refresh` or automatically by the daemon when it detects a stale token.

**Note:** If symlinks cause issues with Claude Code's file resolution (e.g., it doesn't follow symlinks for `.credentials.json`), switch to hardlinks. Hardlinks share the same inode and appear as regular files.

### Open Question: Concurrent `.claude.json` Writes

Multiple Claude instances (across missions) will write to the same `.claude.json` via symlinks. This file stores per-project state, session IDs, usage stats, and caches. Today, running multiple `claude` instances without AgenC already creates this same situation, so it's likely acceptable. However:

- **Investigate:** Does Claude Code use lockfiles or any concurrency protection for `.claude.json`?
- **Fallback:** If corruption occurs, Claude Code creates `.claude.json.backup.*` files automatically.
- **Decision:** Proceed with symlink approach. If concurrent writes prove problematic, switch to copying `.claude.json` at creation with periodic sync of the `oauthAccount` block.

State Capture Integration
-------------------------

The `config-commit` file records the config source repo's commit hash. This feeds into the state tuple (from hyperspace spec):

```
turns table:
  repo_commit     — from auto-commit of workspace changes
  config_commit   — from config-commit file (NEW)
  cli_invocation  — from wrapper's claude command
  session_id      — from Claude Code
```

Since config is pinned, `config_commit` only changes when the user explicitly runs `update-config`. This makes the state tuple fully deterministic.

Changes to Existing Systems
---------------------------

### Wrapper (`internal/wrapper/wrapper.go`)

- Set `CLAUDE_CONFIG_DIR` to per-mission path instead of global `~/.agenc/claude/`
- Remove `watchGlobalConfig()` goroutine (config is pinned, not live-watched)
- Keep template watching for now (removed separately by `agenc-ei3`)

### Mission creation (`internal/mission/mission.go`)

- Add `BuildMissionConfigDir()` function
- Add `UpdateMissionConfigDir()` function (for update-config command)
- Update `CreateMissionDir()` to call `BuildMissionConfigDir()`

### Config schema (`internal/config/agenc_config.go`)

- Add `ClaudeConfigRepo string` field to `AgencConfig`
- Add `ClaudeConfigSubdir string` field (defaults to `""`, meaning repo root — but users should set a subdir)
- Add `GetMissionClaudeConfigDirpath()` path function

### Daemon (`internal/daemon/claude_config_sync.go`)

- Keep the config source repo synced (git fetch/pull in `~/.agenc/repos/`)
- Remove the global merge loop (`syncSymlinks`, `syncClaudeMd`, `syncSettings`) — no longer needed for missions
- Add credential freshness check: periodically verify `~/.agenc/claude/.credentials.json` is valid; re-dump from Keychain (macOS) or re-copy (Linux) if stale

### CLI commands

- `agenc config set claude-config-repo <repo>` — register config source
- `agenc config set claude-config-subdir <subdir>` — set subdir within repo
- `agenc mission update-config [mission-id] [--all]` — update pinned config
- `agenc auth refresh` — re-dump credentials from Keychain (macOS) or re-copy from `~/.claude/` (Linux) to central location; all missions pick up the update via symlinks

### Database

- Add `config_commit` column to missions table (nullable, for state tracking)
- The `turns` table (from `agenc-1g9`) will also store `config_commit` per-turn

Migration
---------

### Existing missions

- Continue to work with the old global `CLAUDE_CONFIG_DIR=~/.agenc/claude/`
- `GetMissionClaudeConfigDirpath()` checks if `claude-config/` exists in the mission dir:
  - If yes: use per-mission config dir (new path)
  - If no: fall back to global `~/.agenc/claude/` (legacy path)
- Users can run `agenc mission update-config <id>` on old missions to create per-mission config dirs

### First-run experience

- On first `mission new` after upgrade, user is prompted to register a config source repo
- Clear messaging about the change and what it means

Beads
-----

Each bead is independently shippable. Parent: `agenc-3l2`.

1. **Add config source registration to schema** — Add `ClaudeConfigRepo` and `ClaudeConfigSubdir` to `AgencConfig`. Add `agenc config set/get` support for these fields. Validate that registered repo exists in repo library (or offer to add it). No blocking dependencies.

2. **Build per-mission config dir** — Implement `BuildMissionConfigDir()`: copy from config source subdir, apply AgenC modifications (CLAUDE.md merge, settings.json deep-merge + hooks), symlink `.claude.json` from `~/.claude/.claude.json` (fallback `~/.claude.json`), ensure central `.credentials.json` exists (dump from Keychain on macOS / copy on Linux), symlink `.credentials.json` to central copy. Update `CreateMissionDir()` to call it. Add `GetMissionClaudeConfigDirpath()`. Blocked by: #1.

3. **Switch wrapper to per-mission CLAUDE_CONFIG_DIR** — Change `buildClaudeCmd()` to use `GetMissionClaudeConfigDirpath()`. Add legacy fallback for missions without `claude-config/`. Remove `watchGlobalConfig()` from wrapper. Blocked by: #2.

4. **Implement `mission update-config` command** — CLI command to fetch latest config source, show diff, re-copy, re-apply modifications, update `config-commit`, trigger wrapper restart. Support `--all` flag. Blocked by: #2.

5. **Add config-commit tracking** — Record config source commit hash in `config-commit` file during mission creation and updates. Add `config_commit` column to missions table. Blocked by: #2.

6. **Remove global config merge loop** — Remove or reduce daemon's `runConfigSyncLoop`. Keep repo sync for config source. Remove `syncSymlinks`, `syncClaudeMd`, `syncSettings`. Blocked by: #3.

7. **Require config repo on `mission new`** — Add enforcement that config source is registered before creating missions. Block and tell user how to register. Blocked by: #1.

Verification
------------

End-to-end test:

1. `agenc config set claude-config-repo <test-repo>` — register config source
2. `agenc mission new` — verify `claude-config/` dir is created with correct contents
3. Verify Claude starts without re-auth prompt
4. Edit a skill in config source repo, commit, push
5. Verify running mission does NOT see the change
6. `agenc mission update-config <id>` — verify diff shown, config updated, Claude restarted
7. Verify `config-commit` file reflects correct commit hash
8. Create a second mission — verify it gets independent config dir
