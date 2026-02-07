Repo Direct Checkout
====================

Overview
--------

This spec covers the first concrete implementation step toward the Hyperspace vision (see `specs/hyperspace.md`). It makes three structural changes to how AgenC creates and manages missions:

1. **Repos clone directly into `agent/`** — no more `workspace/` subdirectory.
2. **`mission new` picker shows repos only** — same machinery as `repo edit`, with a blank mission escape hatch.
3. **`--agent` flag removed** — agent templates are no longer selected at mission creation time.

It also adds one new capability:

4. **Wrapper live-reloads on skill/plugin changes** — closing bead `agenc-csy`.

Together, these changes decouple AgenC from the agent template system (without yet removing the template code) and eliminate the workspace subdirectory that caused permission prompts and conceptual overhead.


Motivation
----------

### The workspace subdirectory problem

Currently, missions have this structure:

```
missions/<uuid>/agent/              ← Claude's CWD (template files here)
                    CLAUDE.md
                    .claude/
                    .mcp.json
                    workspace/
                        <repo>/     ← actual repo clone
```

Claude Code's CWD is `agent/`, but the code lives in `agent/workspace/<repo>/`. This causes:

- **Permission prompts**: Claude asks for permission to access the `workspace/` subdirectory, which is a separate project root from its perspective.
- **Conceptual overhead**: The CLAUDE.md must explain "effective working directory" logic, workspace confinement rules, and "do not modify agent configuration" constraints — all artifacts of the indirection.
- **Conflicting CLAUDE.md files**: The repo's own CLAUDE.md cannot be used as the project CLAUDE.md because the template's CLAUDE.md occupies that slot.

### The agent template coupling problem

Agent templates require choosing an agent persona before starting work. This conflicts with Claude Code's native skill system, which lets users invoke personas on demand. The `--agent` flag adds complexity to mission creation and forces a decision that skills make unnecessary.


Design
------

### Change 1: Clone repo directly into `agent/`

**New mission directory structure:**

```
missions/<uuid>/
    pid
    claude-state
    wrapper.log
    agent/                  ← Claude's CWD, IS the repo
        .git/
        CLAUDE.md           ← repo's own (if present)
        .claude/            ← repo's own (if present)
        <repo files...>
```

For blank missions (no repo selected), `agent/` is an empty directory.

**Changes to `internal/mission/mission.go`:**

`CreateMissionDir` currently:
1. Creates `missionDirpath`, `agentDirpath`, `workspaceDirpath`
2. Copies repo into `workspace/<short-name>/`
3. Rsyncs template into `agent/`
4. Writes `template-commit`

New behavior:
1. Creates `missionDirpath` only (no `agentDirpath` — it comes from the repo copy)
2. If a repo is provided: copies repo directly as `agentDirpath` via `CopyRepo(gitRepoSource, agentDirpath)`
3. If no repo: creates empty `agentDirpath` directory
4. No template rsync, no template-commit file
5. `agentTemplate` parameter is passed as `""` (signature unchanged for now — cleanup deferred)

The repo copy continues to use `CopyRepo` (rsync from the daemon-maintained library cache). This preserves the existing benefits: fast, offline-ready, and keeps the repo library clone in sync via the daemon's update loop.

**Changes to `CopyWorkspace` (used by `--clone`):**

Rename to `CopyAgentDir`. Instead of copying `workspace/` → `workspace/`, it copies `agent/` → `agent/`. The function signature changes:

```go
// Before
func CopyWorkspace(srcWorkspaceDirpath string, dstWorkspaceDirpath string) error

// After
func CopyAgentDir(srcAgentDirpath string, dstAgentDirpath string) error
```

The `--clone` flow in `runMissionNewWithClone` updates accordingly: copies the source mission's `agent/` directory instead of `workspace/`.

**Changes to `internal/config/config.go`:**

- `GetMissionWorkspaceDirpath` — remove (no longer used)
- `WorkspaceDirname` constant — remove

**Changes to `internal/mission/mission.go`:**

- `RsyncTemplate` — leave in place (called by wrapper for template changes on existing missions; removal deferred to template cleanup)
- `ReadTemplateCommitHash` — leave in place (same reason)

### Change 2: Simplify `mission new` picker

**Current flow in `runMissionNewWithPicker`:**
1. `listRepoLibrary` scans repos AND cross-references template config
2. If `--agent` set, filters to repos only
3. Shows fzf with templates, repos, and "blank mission" sentinel
4. Branches on selection type (template / repo / blank)

**New flow:**
1. `findReposOnDisk` scans repos (same function `repo edit` already uses)
2. Shows fzf with repos and "blank mission" sentinel
3. Selected repo → clone into `agent/`, launch wrapper
4. Blank mission → create empty `agent/`, launch wrapper

This eliminates:
- `repoLibraryEntry` struct
- `repoLibrarySelection` struct
- `listRepoLibrary` function
- `selectFromRepoLibrary` function (replaced by direct use of the repo fzf picker + sentinel)
- `launchFromLibrarySelection` function (no template-vs-repo branching)
- `resolveAgentTemplate` function
- `isAgentTemplate` helper
- `formatLibraryFzfLine` function

The picker uses the same `Resolve` pattern and `FormatRow` as `repo edit`, adding only the "blank mission" sentinel row.

**`createAndLaunchMission` simplification:**

The `agentTemplate` parameter is always passed as `""`. The function signature is left unchanged for now (template cleanup pass will clean it).

```go
// Called for repo selection
createAndLaunchMission(agencDirpath, "", repoName, cloneDirpath, promptFlag)

// Called for blank mission
createAndLaunchMission(agencDirpath, "", "", "", promptFlag)
```

### Change 3: Remove `--agent` flag

Remove from both `mission new` and `repo edit`:

- `mission_new.go`: Remove `agentFlag` var, flag registration, all code paths that reference it
- `repo_edit.go`: Remove `repoEditAgentFlag` var, flag registration
- `repo_edit.go`: `launchRepoEditMission` passes `""` for agent template directly
- `mission_new.go`: `runMissionNewWithClone` / `resolveCloneAgentTemplate` — the clone flow inherits the source mission's `agent_template` DB value but passes `""` to `CreateMissionDir` (no template rsync). The inherited value is stored in the DB for historical record only.
- Update command `Long` help text for both commands

### Change 4: Wrapper watches skills/plugins for reload

**Problem:** The wrapper currently reloads Claude only when `settings.json` or `CLAUDE.md` change in the global config directory. Skills and plugins are symlinked from `~/.claude/` into `$AGENC_DIRPATH/claude/` but changes to the real directories behind the symlinks are invisible to the existing fsnotify watcher.

**Approach: Polling with content fingerprint.**

A new goroutine `pollSkillsAndPlugins` runs every 10 seconds (same cadence as the existing template commit poll). On each tick:

1. Walk `$AGENC_DIRPATH/claude/skills/` and `$AGENC_DIRPATH/claude/plugins/` using `filepath.WalkDir` (which follows symlinks by default via `os.Stat`)
2. For each regular file found, record `(relative path, mod time, size)` as a tuple
3. Sort tuples, compute a SHA-256 hash of the concatenated representation
4. Compare to the previously stored hash
5. If different, send on `globalConfigChanged` (same channel used for settings/CLAUDE.md changes — same restart-at-idle behavior)

**Why polling over fsnotify:**
- `fsnotify` does not follow symlinks — watching the symlink sees nothing when the real directory changes
- `fsnotify` does not support recursive directory watching natively — skills can have subdirectories
- Polling is simple, reliable, and the 10-second latency is acceptable for config changes
- The template commit poll was already using this exact pattern successfully

**Implementation in `internal/wrapper/wrapper.go`:**

```go
func (w *Wrapper) pollSkillsAndPlugins(ctx context.Context) {
    globalClaudeDirpath := config.GetGlobalClaudeDirpath(w.agencDirpath)
    watchDirpaths := []string{
        filepath.Join(globalClaudeDirpath, "skills"),
        filepath.Join(globalClaudeDirpath, "plugins"),
    }

    var previousHash string

    ticker := time.NewTicker(skillsPollInterval) // 10s
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            currentHash := computeDirFingerprint(watchDirpaths)
            if previousHash != "" && currentHash != previousHash {
                w.logger.Info("Skills/plugins changed, triggering reload")
                select {
                case w.globalConfigChanged <- struct{}{}:
                default:
                }
            }
            previousHash = currentHash
        }
    }
}
```

The `computeDirFingerprint` function walks the directories, collects `(path, mtime, size)` tuples, sorts them, and returns a hash string. If a directory doesn't exist (e.g., no skills configured), it contributes nothing to the hash.

**Wrapper startup changes:**

In `Wrapper.Run`, add the new goroutine:

```go
go w.pollSkillsAndPlugins(ctx)
```

The existing `pollTemplateChanges` goroutine is left in place (still gated on `w.agentTemplate != ""`). Since we now always pass `""` for agent template, it will never start. Removal is deferred to the template cleanup pass.

### Change 5: Update wrapper remote refs watcher path

**Current path:** `agent/workspace/<repo>/.git/refs/remotes/origin/`

**New path:** `agent/.git/refs/remotes/origin/`

In `watchWorkspaceRemoteRefs`:

```go
// Before
workspaceDirpath := config.GetMissionWorkspaceDirpath(w.agencDirpath, w.missionID)
repoShortName := filepath.Base(w.gitRepoName)
workspaceRepoDirpath := filepath.Join(workspaceDirpath, repoShortName)

// After
agentDirpath := config.GetMissionAgentDirpath(w.agencDirpath, w.missionID)
```

The rest of the function is unchanged — it watches `agentDirpath/.git/refs/remotes/origin/<default-branch>` and calls `ForceUpdateRepo` on the library clone when a push is detected.

### Change 6: Hook path validation

The idle/busy hooks use `$CLAUDE_PROJECT_DIR/../claude-state`:

```json
"echo idle > \"$CLAUDE_PROJECT_DIR/../claude-state\""
```

`CLAUDE_PROJECT_DIR` is `agent/`. One level up is `missions/<uuid>/`. `claude-state` lives at `missions/<uuid>/claude-state`. **This still works.** No change needed.


Beads Closed
------------

| Bead | Title | How |
|---|---|---|
| `agenc-csy` | Reload on non-CLAUDE.md/settings.json changes | Change 4: wrapper polls skills/ and plugins/ directories |
| `agenc-q74` | Fix asking for permissions to read repos error | Change 1: repo is directly in `agent/`, eliminating the workspace subdirectory that triggered permission prompts |


Beads NOT Closed (Deferred)
---------------------------

| Bead | Title | Why deferred |
|---|---|---|
| `agenc-ei3` | Eliminate agent template system | This change stops *using* templates but does not remove the code. Full removal is a separate cleanup pass. |
| `agenc-1al` | Make missions repo-oriented with per-session branches | Per-session branches are Hyperspace Phase 2. This change makes missions repo-oriented (repo in `agent/`) but does not create session branches. |
| `agenc-90i` | Roll agenc-config global CLAUDE settings into agenc defaults | Populating `claude-modifications/CLAUDE.md` with operational defaults is a separate task. |


Files Changed
-------------

| File | Change |
|---|---|
| `internal/mission/mission.go` | `CreateMissionDir`: remove workspace dir creation, clone repo to `agent/` directly, skip template rsync/commit. Leave `RsyncTemplate` and `ReadTemplateCommitHash` in place. |
| `internal/mission/repo.go` | Rename `CopyWorkspace` → `CopyAgentDir`, update parameter names. |
| `internal/config/config.go` | Remove `GetMissionWorkspaceDirpath`, remove `WorkspaceDirname` constant. |
| `cmd/mission_new.go` | Remove `--agent` flag and `agentFlag` var. Replace `listRepoLibrary`/`selectFromRepoLibrary`/`launchFromLibrarySelection` with `findReposOnDisk` + direct picker. Remove `resolveAgentTemplate`, `isAgentTemplate`, `formatLibraryFzfLine`, `repoLibraryEntry`, `repoLibrarySelection`. Always pass `""` for agent template. Keep blank mission sentinel. |
| `cmd/repo_edit.go` | Remove `--agent` flag and `repoEditAgentFlag`. Pass `""` for agent template in `launchRepoEditMission`. |
| `internal/wrapper/wrapper.go` | Add `pollSkillsAndPlugins` goroutine. Update `watchWorkspaceRemoteRefs` to use `agent/` path instead of `workspace/<repo>/`. Leave `pollTemplateChanges` in place (it won't start since agentTemplate is always `""`). |
| `cmd/mission_helpers.go` | `buildMissionPickerEntries` still references `AgentTemplate` for display — leave as-is (shows historical value from DB). |
| `internal/daemon/claude_config_sync.go` | No changes. Symlinks and settings merge are unaffected. |
| `internal/database/database.go` | No schema changes. `CreateMission` still accepts `agentTemplate` param — callers pass `""`. |


Migration
---------

### Existing missions

Existing missions retain their `workspace/<repo>/` structure. The wrapper code that references workspace paths only runs for those missions (the `watchWorkspaceRemoteRefs` goroutine resolves paths at startup).

However, `mission resume` will need attention: when resuming an existing mission that has a `workspace/` structure, the wrapper must use the old path. The simplest approach: at wrapper startup, check if `agent/workspace/` exists and contains a subdirectory. If so, use the old path structure for `watchWorkspaceRemoteRefs`. Otherwise use the new path (`agent/.git/`).

```go
func (w *Wrapper) resolveRepoDirpath() string {
    agentDirpath := config.GetMissionAgentDirpath(w.agencDirpath, w.missionID)

    // Check for legacy workspace/<repo>/ structure
    legacyWorkspaceDirpath := filepath.Join(agentDirpath, "workspace")
    if entries, err := os.ReadDir(legacyWorkspaceDirpath); err == nil && len(entries) > 0 {
        for _, entry := range entries {
            if entry.IsDir() {
                return filepath.Join(legacyWorkspaceDirpath, entry.Name())
            }
        }
    }

    // New structure: repo is directly in agent/
    return agentDirpath
}
```

### New missions

All new missions use the direct checkout structure. No `workspace/` directory is created.

### Database

No schema migration needed. The `agent_template` column stores `""` for new missions. Existing missions retain their historical template value.


Testing
-------

1. **`mission new <repo>`** — verify repo cloned directly into `agent/`, Claude starts in repo root, no `workspace/` directory created
2. **`mission new` (blank)** — verify empty `agent/` directory, Claude starts there
3. **`mission new --clone <id>`** — verify source mission's `agent/` is copied to new mission's `agent/`
4. **`repo edit <repo>`** — verify no `--agent` flag, repo cloned into `agent/`
5. **`mission resume <old-mission>`** — verify old missions with `workspace/` structure still work correctly
6. **Skill change detection** — modify a file in `~/.claude/skills/`, verify Claude restarts within ~10 seconds at next idle transition
7. **Plugin change detection** — same as above for `~/.claude/plugins/`
8. **Push detection** — from within the mission, `git push` and verify the repo library clone is updated
9. **Permission prompts** — verify Claude does NOT prompt for permission to access repo files (the original bug that `agenc-q74` tracks)
