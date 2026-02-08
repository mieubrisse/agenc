Shadow Config: AgenC-Owned Claude Config Tracking
====================================================

Status: Proposed
Supersedes: Config source repo mechanism in `per-mission-claude-config.md`

Problem
-------

AgenC needs hermetic, reproducible Claude config for each mission. The current
approach requires users to maintain a separate config source repo and register
it with AgenC. This works but adds friction:

1. **Setup burden** — Users must create a dotfiles repo, organize Claude config
   into a subdirectory, register it with `agenc config set claude-config-repo`.
2. **Hardcoded paths** — Claude Code writes absolute paths into files it manages
   (e.g., `plugins/installed_plugins.json` contains `"installPath":
   "/Users/odyssey/.claude/plugins/cache/..."`). These paths break when copied
   to a different `CLAUDE_CONFIG_DIR`.
3. **Two sources of truth** — The user's `~/.claude/` and their config repo can
   diverge. Changes made via Claude Code (plugin installs, settings edits) go to
   `~/.claude/` but not the repo. Changes pushed to the repo don't appear in
   `~/.claude/` until synced.

Solution Overview
-----------------

AgenC maintains an internal git repo (the "shadow repo") that automatically
tracks a defined set of files from `~/.claude/`. The shadow repo is the
versioned, path-normalized copy of the user's Claude config. At mission
creation, files are rendered from the shadow repo into the mission's
`claude-config/` directory.

**Key properties:**

- `~/.claude/` is the canonical source. AgenC only reads from it.
- The shadow repo is internal to AgenC — no remote, no user-facing repo.
- Path normalization on ingest: absolute `~/.claude` paths → `${CLAUDE_CONFIG_DIR}`.
- Path expansion on render: `${CLAUDE_CONFIG_DIR}` → actual mission config path.
- A filesystem watcher (fsnotify) triggers immediate sync on changes.
- The external config repo mechanism (`ClaudeConfig.Repo`, `ClaudeConfig.Subdirectory`)
  is removed entirely.

Tracked Files
-------------

These files/directories are shadowed from `~/.claude/` into the shadow repo:

| Item | Type | Notes |
|------|------|-------|
| `CLAUDE.md` | file | User's global instructions |
| `settings.json` | file | Permissions, hooks, preferences |
| `skills/` | directory | Custom skills |
| `hooks/` | directory | Hook scripts |
| `commands/` | directory | Slash commands |
| `agents/` | directory | Agent definitions |

**NOT tracked** (runtime/machine state — generated fresh per mission or
symlinked):

| Item | Reason |
|------|--------|
| `.claude.json` | Account identity — symlinked per mission |
| `.credentials.json` | Auth tokens — dumped from Keychain per mission |
| `plugins/` | Symlinked to `~/.claude/plugins/` per mission (see Plugins) |
| `projects/` | Per-project tool approvals — regenerated per mission |
| `history.jsonl` | Machine-generated conversation history |
| `stats-cache.json` | Machine-generated statistics |
| `cache/` | Ephemeral cache |
| `debug/` | Debug logs |
| `session-env/` | Session environment snapshots |
| `todos/` | Claude-managed todo state |
| `tasks/` | Claude-managed task state |
| `shell-snapshots/` | Shell state captures |
| `file-history/` | File modification history |
| `paste-cache/` | Clipboard cache |
| `plans/` | Plan mode state |
| `chrome/` | Chrome extension state |
| `ide/` | IDE integration state |
| `telemetry/` | Telemetry data |
| `statsig/` | Feature flag state |
| `.update.lock` | Update lockfile |

Shadow Repo
-----------

### Location

```
~/.agenc/claude-config-shadow/
├── .git/
├── CLAUDE.md
├── settings.json
├── skills/
│   ├── my-skill/
│   │   └── SKILL.md
│   └── ...
├── hooks/
│   ├── my-hook.sh
│   └── ...
├── commands/
│   └── ...
└── agents/
    └── ...
```

### Path Normalization

On ingest (copying from `~/.claude/` to shadow repo), all text files are
scanned for absolute paths referencing `~/.claude`. Three patterns are
normalized:

1. **Full absolute path**: `/Users/odyssey/.claude/` → `${CLAUDE_CONFIG_DIR}/`
2. **Home-relative path**: `${HOME}/.claude/` → `${CLAUDE_CONFIG_DIR}/`
3. **Tilde path**: `~/.claude/` → `${CLAUDE_CONFIG_DIR}/`

The normalization uses the resolved value of `$HOME` at ingest time to match
pattern #1. Pattern #3 is a literal string replacement.

**Scope**: Only text files are normalized (determined by file extension: `.json`,
`.md`, `.sh`, `.bash`, `.py`, `.yml`, `.yaml`, `.toml`). Binary files are copied
as-is. Symlinks in `~/.claude/` that point to tracked files are resolved (the
target content is copied, not the symlink itself).

**Pre-commit hook**: The shadow repo has a pre-commit hook that rejects any
commit containing the literal strings `/Users/<username>/.claude/` or
`${HOME}/.claude/` or `~/.claude/` in tracked files. This catches normalization
bugs and prevents path leaks.

### Initialization

The shadow repo is auto-created on the first `mission new` if it doesn't exist:

1. `mission new` detects `~/.agenc/claude-config-shadow/` doesn't exist
2. Creates the directory, runs `git init`
3. Installs the pre-commit hook
4. Copies tracked files from `~/.claude/`, applying path normalization
5. Creates initial commit: `"Initial snapshot of ~/.claude config"`
6. Proceeds with normal mission creation

If `~/.claude/` doesn't exist or has no tracked files, the shadow repo is
created empty (with just a `.gitkeep`). This is not an error — the user may not
have configured Claude Code yet.

### Filesystem Watcher

A daemon goroutine uses `fsnotify` to watch `~/.claude/` for changes to tracked
files. When a change is detected:

1. Wait for a short debounce period (500ms) to batch rapid changes
2. Copy changed tracked files into shadow repo with path normalization
3. If any files changed, auto-commit: `"Sync: <list of changed files>"`

The watcher only monitors tracked files/directories. Changes to runtime files
(history.jsonl, debug/, etc.) are ignored.

**Symlink handling**: If a tracked file in `~/.claude/` is a symlink (common
when users link to a dotfiles repo), the watcher monitors the symlink target for
changes. When the target changes, the resolved content is ingested (not the
symlink itself).

**Edge cases**:

- If `~/.claude/` doesn't exist at daemon start, the watcher waits for it to be
  created (watches `~/` for the `.claude` directory appearing).
- If a tracked directory is deleted in `~/.claude/`, the corresponding directory
  in the shadow repo is removed and committed.

Per-Mission Config Build
------------------------

`BuildMissionConfigDir` changes to read from the shadow repo instead of an
external config source repo:

### Source

**Before**: `~/.agenc/repos/github.com/owner/dotfiles/claude/` (external repo)
**After**: `~/.agenc/claude-config-shadow/` (internal shadow repo)

### Build Steps

1. Create `~/.agenc/missions/<uuid>/claude-config/`
2. Copy tracked files from shadow repo into `claude-config/`, applying path
   expansion (`${CLAUDE_CONFIG_DIR}` → actual mission config dirpath)
3. Apply AgenC modifications:
   - Merge AgenC's CLAUDE.md content into user's CLAUDE.md
   - Deep-merge AgenC's settings.json into user's settings.json
   - Append AgenC operational hooks (Stop, UserPromptSubmit)
   - Append repo library deny permissions
4. Symlink `.claude.json` → `~/.claude/.claude.json` (fallback: `~/.claude.json`)
5. Dump `.credentials.json` from Keychain (macOS) or copy from
   `~/.claude/.credentials.json` (Linux)
6. Symlink `plugins/` → `~/.claude/plugins/`
7. Record shadow repo HEAD commit hash in `config_commit` DB column

### Path Expansion

On render (building mission config), `${CLAUDE_CONFIG_DIR}` in text files is
expanded to the actual mission config directory path. This is the reverse of
normalization.

**Example**: `"installPath": "${CLAUDE_CONFIG_DIR}/plugins/cache/lua-lsp/1.0.0"`
becomes `"installPath": "/Users/odyssey/.agenc/missions/<uuid>/claude-config/plugins/cache/lua-lsp/1.0.0"`

Note: Since plugins are now symlinked (not copied), plugin-related paths will
naturally resolve through `~/.claude/plugins/`. Path expansion is primarily
relevant for settings.json hook commands and any other config that references
`CLAUDE_CONFIG_DIR`.

Plugins
-------

Each mission's `claude-config/plugins/` is a **symlink** to `~/.claude/plugins/`.
This means:

- All missions share the same installed plugins
- Plugin installs/uninstalls in any context (mission or direct `claude`) are
  immediately visible everywhere
- No plugin metadata path rewriting is needed
- The shadow repo does NOT track plugins

**Trade-off**: This is a slight leak of hermeticism — missions are not fully
isolated for plugins. A plugin update mid-mission could change behavior. This is
acceptable because:

1. Plugin updates are rare (user-initiated)
2. Plugin behavior changes are typically backward-compatible
3. Full plugin isolation would require duplicating large git clones per mission

Config Update Flow
------------------

`mission update-config` changes:

**Before**: Fetches latest from external config source repo, re-copies, re-applies
modifications.

**After**: Reads current state of shadow repo (which is kept up-to-date by the
filesystem watcher), re-copies, re-applies modifications.

The diff is computed between the mission's current `config_commit` and the
shadow repo's HEAD:

```
git -C ~/.agenc/claude-config-shadow/ diff <old-commit>..HEAD -- .
```

The rest of the flow (show diff, confirm, rebuild, update DB) is unchanged.

Mission Config Export
---------------------

When a user modifies tracked config files inside a mission (e.g., edits
`CLAUDE.md` via a Claude session, or a slash command modifies `settings.json`),
those changes live only in that mission's `claude-config/`.

An explicit command propagates changes back:

```
agenc config export [mission-id]
```

This command:

1. Diffs the mission's `claude-config/` tracked files against the shadow repo
2. Shows the diff to the user
3. On confirmation, copies changed files to `~/.claude/` (with reverse path
   normalization: `${actual_mission_path}` → `~/.claude`)
4. The filesystem watcher picks up the `~/.claude/` changes and auto-ingests
   them into the shadow repo

This is an explicit, user-initiated workflow. No automatic propagation from
missions back to `~/.claude/`.

Changes to Existing Code
-------------------------

### Remove: Config Source Repo Mechanism

- **`internal/config/agenc_config.go`**: Remove `ClaudeConfig` struct
  (`Repo`, `Subdirectory` fields) from `AgencConfig`. Remove `GetAllSyncedRepos`
  method's inclusion of `ClaudeConfig.Repo`.
- **`cmd/config_init.go`**: Remove config repo registration wizard steps.
  Replace with auto-scaffold of shadow repo.
- **`cmd/mission_new.go`**: Remove `requireConfigSourceRepo()` and
  `resolveConfigSourceDirpath()`. Replace with shadow repo existence check.
- **`cmd/mission_update_config.go`**: Update to diff against shadow repo
  instead of external config repo.
- **`internal/claudeconfig/build.go`**: Change `BuildMissionConfigDir` to read
  from shadow repo path. Remove `configSourceDirpath` parameter. Add path
  expansion logic. Add plugins symlink step.

### Add: Shadow Repo Management

- **`internal/claudeconfig/shadow.go`** (new): Shadow repo initialization,
  ingest (copy + normalize), path normalization/expansion functions.
- **`internal/claudeconfig/shadow_test.go`** (new): Tests for path
  normalization, expansion, ingest logic.
- **`internal/daemon/config_watcher.go`** (new): fsnotify-based watcher for
  `~/.claude/` tracked files. Debounced ingest + auto-commit.
- **`internal/daemon/daemon.go`**: Add watcher goroutine to `Run()`.
- **`cmd/config_export.go`** (new): `agenc config export` command.

### Modify: Build Flow

- **`internal/claudeconfig/build.go`**:
  - `BuildMissionConfigDir(agencDirpath, missionID)` — no longer takes
    `configSourceDirpath`
  - Reads from `GetShadowRepoDirpath(agencDirpath)` instead
  - After copying tracked files, applies path expansion
  - After auth setup, creates plugins symlink
  - `ResolveConfigCommitHash` reads from shadow repo instead of external repo

### Remove: Config Source Resolution

- **`internal/claudeconfig/build.go`**: Remove `ResolveConfigSourceDirpath()`
  function. Callers use `GetShadowRepoDirpath()` instead.

Path Normalization Implementation
---------------------------------

### Normalize (ingest: `~/.claude` → shadow repo)

```go
func normalizePaths(content []byte, homeDirpath string) []byte {
    claudeDirpath := filepath.Join(homeDirpath, ".claude")

    // Order matters: most specific first
    // 1. Absolute path: /Users/odyssey/.claude/ → ${CLAUDE_CONFIG_DIR}/
    content = bytes.ReplaceAll(content,
        []byte(claudeDirpath+"/"),
        []byte("${CLAUDE_CONFIG_DIR}/"))

    // 2. Trailing without slash (end of string or followed by quote)
    content = bytes.ReplaceAll(content,
        []byte(claudeDirpath),
        []byte("${CLAUDE_CONFIG_DIR}"))

    // 3. ${HOME}/.claude/ → ${CLAUDE_CONFIG_DIR}/
    content = bytes.ReplaceAll(content,
        []byte("${HOME}/.claude/"),
        []byte("${CLAUDE_CONFIG_DIR}/"))
    content = bytes.ReplaceAll(content,
        []byte("${HOME}/.claude"),
        []byte("${CLAUDE_CONFIG_DIR}"))

    // 4. ~/.claude/ → ${CLAUDE_CONFIG_DIR}/
    content = bytes.ReplaceAll(content,
        []byte("~/.claude/"),
        []byte("${CLAUDE_CONFIG_DIR}/"))
    content = bytes.ReplaceAll(content,
        []byte("~/.claude"),
        []byte("${CLAUDE_CONFIG_DIR}"))

    return content
}
```

### Expand (render: shadow repo → mission config)

```go
func expandPaths(content []byte, claudeConfigDirpath string) []byte {
    return bytes.ReplaceAll(content,
        []byte("${CLAUDE_CONFIG_DIR}"),
        []byte(claudeConfigDirpath))
}
```

### Pre-commit Hook

```bash
#!/usr/bin/env bash
set -euo pipefail

home_dirpath="${HOME}"
username="$(basename "${home_dirpath}")"

# Check staged files for un-normalized paths
if git diff --cached --diff-filter=ACM -z -- | \
   xargs -0 git show 2>/dev/null | \
   grep -qE "(${home_dirpath}/\.claude|\\$\\{HOME\\}/\\.claude|~/\\.claude)"; then
    echo "ERROR: Staged files contain un-normalized ~/.claude paths."
    echo "Paths must use \${CLAUDE_CONFIG_DIR} instead."
    exit 1
fi
```

Plugins Symlink
---------------

During `BuildMissionConfigDir`, after creating the mission config directory:

```go
pluginsLinkPath := filepath.Join(claudeConfigDirpath, "plugins")
pluginsTargetPath := filepath.Join(homeDir, ".claude", "plugins")
os.Symlink(pluginsTargetPath, pluginsLinkPath)
```

If `~/.claude/plugins/` doesn't exist, the symlink is still created (it will
resolve when the user installs their first plugin).

Migration
---------

### From Config Source Repo

Users who currently have `claude-config-repo` registered:

1. On first `mission new` after upgrade, AgenC detects the shadow repo doesn't
   exist
2. Initializes shadow repo from `~/.claude/` (NOT from the old config source
   repo — `~/.claude/` is canonical now)
3. The old `ClaudeConfig` block in `config.yml` is ignored (and eventually
   cleaned up)
4. If `~/.claude/` files are symlinks to the config source repo, the symlink
   targets are resolved and content is copied into the shadow repo

### Existing Missions

Existing missions continue to work — they already have a `claude-config/`
directory with real files. The only change is that `mission update-config`
reads from the shadow repo instead of the external config repo.

### Backward Compatibility

`GetMissionClaudeConfigDirpath()` still falls back to `~/.agenc/claude/` for
legacy missions without `claude-config/`. No change here.

Directory Structure
-------------------

```
~/.agenc/
├── claude-config-shadow/           # NEW: internal shadow repo
│   ├── .git/
│   ├── CLAUDE.md                   # normalized copy from ~/.claude/
│   ├── settings.json               # normalized copy from ~/.claude/
│   ├── skills/                     # normalized copy from ~/.claude/
│   ├── hooks/                      # normalized copy from ~/.claude/
│   ├── commands/                   # normalized copy from ~/.claude/
│   └── agents/                     # normalized copy from ~/.claude/
├── config/                         # agenc's own config
│   ├── config.yml                  # NO MORE ClaudeConfig block
│   └── claude-modifications/       # agenc's CLAUDE.md/settings.json overlays
├── missions/
│   └── <uuid>/
│       ├── agent/                  # repo checkout
│       ├── claude-config/          # rendered CLAUDE_CONFIG_DIR
│       │   ├── .claude.json        # → symlink to ~/.claude/.claude.json
│       │   ├── .credentials.json   # real file (dumped from Keychain)
│       │   ├── CLAUDE.md           # expanded + agenc modifications
│       │   ├── settings.json       # expanded + agenc modifications + hooks
│       │   ├── skills/             # expanded copy from shadow repo
│       │   ├── hooks/              # expanded copy from shadow repo
│       │   ├── commands/           # expanded copy from shadow repo
│       │   ├── agents/             # expanded copy from shadow repo
│       │   └── plugins/            # → symlink to ~/.claude/plugins/
│       ├── claude-state
│       ├── pid
│       └── wrapper.log
└── repos/                          # repo library (unchanged)
```

Beads
-----

Each bead is independently shippable.

1. **Implement shadow repo core** — Create `internal/claudeconfig/shadow.go`
   with `InitShadowRepo()`, `IngestFromClaudeDir()`, path normalization/expansion
   functions, pre-commit hook installation. Write tests. No blocking
   dependencies.

2. **Add fsnotify watcher** — Create `internal/daemon/config_watcher.go` with
   debounced filesystem watcher for `~/.claude/` tracked files. Add goroutine
   to daemon's `Run()`. Blocked by: #1.

3. **Switch BuildMissionConfigDir to shadow repo** — Remove
   `configSourceDirpath` parameter. Read from shadow repo. Add path expansion
   step. Add plugins symlink. Update all callers. Blocked by: #1.

4. **Remove config source repo mechanism** — Remove `ClaudeConfig` struct from
   `AgencConfig`. Remove `requireConfigSourceRepo()`, `resolveConfigSourceDirpath()`,
   config repo registration wizard. Update `config.yml` schema. Blocked by: #3.

5. **Add `config export` command** — `agenc config export [mission-id]` to
   propagate mission config changes back to `~/.claude/`. Blocked by: #3.

6. **Auto-init shadow repo on mission new** — Add shadow repo existence check
   to `mission new`. Auto-snapshot `~/.claude/` if shadow repo doesn't exist.
   Blocked by: #1.

7. **Update `mission update-config`** — Diff against shadow repo HEAD instead
   of external config repo. Blocked by: #3.

Verification
------------

1. **Shadow repo initialization**: Run `mission new` on a clean install. Verify
   `~/.agenc/claude-config-shadow/` is created with normalized copies of
   `~/.claude/` tracked files. Verify no absolute paths leak into the shadow
   repo (pre-commit hook should prevent this).

2. **Filesystem watcher**: Edit `~/.claude/settings.json` while the daemon is
   running. Verify the change appears in the shadow repo within 1 second.
   Verify `git log` in shadow repo shows the auto-commit.

3. **Mission config build**: Create a mission. Verify `claude-config/` contains
   expanded paths (not `${CLAUDE_CONFIG_DIR}`). Verify `plugins/` is a symlink
   to `~/.claude/plugins/`. Verify `.credentials.json` is a real file.

4. **Config export**: Inside a running mission, modify `CLAUDE.md`. Run
   `agenc config export <mission-id>`. Verify the change appears in
   `~/.claude/CLAUDE.md` and then in the shadow repo.

5. **Pre-commit hook**: Manually try to commit a file containing
   `/Users/<username>/.claude/` to the shadow repo. Verify the commit is
   rejected.

6. **Migration**: With an existing config source repo registered, run
   `mission new`. Verify shadow repo is initialized from `~/.claude/` (not
   from the old config source repo). Verify old missions still work.

Open Questions
--------------

### Symlink Resolution During Ingest

If `~/.claude/settings.json` is a symlink to a dotfiles repo, and the user edits
the symlink target directly, will fsnotify fire on `~/.claude/settings.json`?
Most fsnotify implementations do NOT follow symlinks — they watch the symlink
itself, not the target. The watcher may need to resolve symlinks and watch the
target paths instead. This needs testing and may require `filepath.EvalSymlinks`
on each tracked item to determine the actual paths to watch.

### Hook Commands Referencing `~/.claude`

User hook commands like `bash ~/.claude/hooks/my-hook.sh` currently reference
`~/.claude` with a tilde. After normalization, these become `bash
${CLAUDE_CONFIG_DIR}/hooks/my-hook.sh`. When expanded in a mission, this
becomes `bash /Users/odyssey/.agenc/missions/<uuid>/claude-config/hooks/my-hook.sh`.
This is correct — the hook script exists in the mission's config dir.

However, if the hook script internally references `~/.claude/`, those internal
references are NOT rewritten (the script is a tracked file, so its content IS
normalized in the shadow repo, but shell variable expansion of
`${CLAUDE_CONFIG_DIR}` won't work in arbitrary shell scripts). This means hook
scripts should use `$CLAUDE_CONFIG_DIR` environment variable (which Claude Code
sets) rather than hardcoded `~/.claude` paths. This is already best practice
but may need documentation.

### AgenC Modifications Merge Order

Currently, AgenC modifications are merged into user config. With the shadow
repo, the merge order is:

1. Shadow repo content (normalized user config)
2. Path expansion applied
3. AgenC modifications merged on top

This means AgenC modifications can use absolute paths (they're not stored in
the shadow repo). The `BuildRepoLibraryDenyEntries` function already uses
absolute paths, which is correct.
