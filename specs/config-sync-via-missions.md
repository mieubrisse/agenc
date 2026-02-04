Config Sync via Missions
========================

Status: Proposed

Problem
-------

Users want to version-control their `$AGENC_DIRPATH/config/` directory (which contains `config.yml` and `claude-modifications/`) in a Git repository. The previous approach — `agenc config sync <repo>` — replaced the config directory with a symlink to a cloned repo. The daemon then force-pulled that repo every cycle, clobbering any local changes made by CLI commands (e.g., `agenc template add`).

The fundamental conflict: the config directory is both **locally writable** (by CLI commands) and **remotely synced** (by the daemon pulling from the repo). A force-pull destroys local writes.

Desired Behavior
----------------

- Config changes made locally (via CLI or manual edits) are pushed to the remote repo.
- Config changes made remotely (direct pushes to the repo) are pulled locally.
- Conflicts between local and remote changes are resolved automatically when possible, with human escalation when not.
- The config directory is always a real directory, never a symlink.

Proposed Design
---------------

Use AgenC's own mission machinery to manage config sync. A dedicated **config sync mission** runs periodically (via crons, once implemented) or on demand, with the sole job of reconciling local config with the remote repo.

### How it works

1. **Setup**: The user registers a config sync repo via a new CLI command (e.g., `agenc config repo set <repo>`). This stores the repo reference in a well-known location (e.g., a `configRepo` field in `config.yml` or a separate file in `$AGENC_DIRPATH/`). The repo is cloned into the repo library as usual.

2. **Periodic sync via cron mission**: A cron mission (see `specs/crons-and-messaging.md`) runs on a schedule. The agent in this mission:
   - Compares the local config directory contents against the repo library clone.
   - If local changes exist that aren't in the repo, commits and pushes them.
   - If remote changes exist that aren't local, pulls and applies them.
   - If both sides have changed, attempts a merge. If the merge fails, the agent either resolves the conflict using its judgment (for simple cases like non-overlapping YAML changes) or flags it for the user.

3. **The config directory stays a real directory.** The repo library clone is the Git working tree. Sync means copying files between `$AGENC_DIRPATH/config/` and the repo library clone — not symlinking.

4. **CLI writes are authoritative in the short term.** Between sync cycles, local changes from CLI commands are the source of truth. The sync mission's job is to propagate them to the remote.

### Agent responsibilities

The config sync agent needs to:
- Detect diffs between local config and the repo clone
- Stage, commit, and push local changes to the remote
- Pull remote changes and apply them to local config
- Resolve merge conflicts (or escalate)
- Report sync status (last sync time, any unresolved conflicts)

### Conflict resolution strategy

Most config changes are additive (adding a template, adding a synced repo) and won't conflict. For the rare case where both sides modify the same field:
- **YAML scalar conflicts**: Remote wins by default (the user explicitly pushed a change).
- **YAML list conflicts**: Union merge (combine both lists, deduplicate).
- **Unresolvable conflicts**: The agent flags the conflict and leaves both versions available for the user to choose.

### Prerequisites

- Cron missions (`specs/crons-and-messaging.md`) — needed for periodic scheduling
- An agent template purpose-built for config sync (minimal, no workspace needed)

### Open questions

- Should the sync be bidirectional from the start, or start with push-only (local -> remote)?
- What's the right sync frequency? Every 5 minutes matches the old daemon cycle, but could be configurable.
- Should the agent have direct file-system access to `$AGENC_DIRPATH/config/`, or should it work through a workspace copy?
- How should the user be notified of unresolved conflicts?
