Hyperspace
==========

Overview & Motivation
---------------------

The bottleneck in AI-assisted development is not agent execution speed — it is the human's supervisory loop. Senior developers using Claude Code today act as quality signal detectors: they monitor agent output, spot friction points, intervene when something goes wrong, and capture learnings back into the system. The metric that matters is not "code produced per day" but "captured learnings per day" — how many unique decisions get made, validated, and rolled back into the development process.

Today, this supervisory loop is inefficient. Developers play whack-a-mole across terminal tabs, lose context when switching between sessions, and have no systematic way to capture friction points or reproduce problematic interactions. Learnings evaporate. Bad interactions repeat.

Hyperspace restructures AgenC around three pillars:

1. **Git state capture** — Every agent turn produces a reproducible state tuple (repo commit, Claude config commit, CLI invocation, conversation history, user prompt). This enables replay, debugging, A/B testing, and forensic analysis.
2. **Tmux control plane** — Replace ad-hoc terminal management with a structured tmux environment that provides hotkeys for forking conversations, interrogating sessions, and stack-based mission switching.
3. **Repo-oriented missions** — Every mission starts inside a repo clone. Sessions are branches. Auto-commit and auto-push after every turn. The repo becomes the single source of truth.

### What This Replaces

In the current AgenC architecture:

- **Agent templates** (separate Git repos with CLAUDE.md, settings.json, .mcp.json) are synced into missions by the daemon and wrapper. The wrapper polls for template changes every 10 seconds and hot-reloads at idle moments. **Hyperspace eliminates this entire layer.**
- **The daemon's template updater goroutine** (fetches template repos from GitHub every 60 seconds) becomes unnecessary. **Removed.**
- **Template rsync into mission directories** (with exclusion rules for workspace/, .git/, settings.local.json) is no longer needed. **Removed.**
- **Config YAML for agent templates** (nicknames, defaultFor, canonical names) is no longer needed. **Removed.**

The daemon retains its other responsibilities: Claude config sync (hooks injection), config auto-commit, and cron scheduling.


Eliminating Agent Templates
----------------------------

### Current State

Agent templates are Git repositories that define agent configuration. They live in `$AGENC_DIRPATH/repos/github.com/owner/template/` and contain files like `CLAUDE.md`, `.claude/settings.json`, and `.mcp.json`. The system has three moving parts:

1. **Daemon template updater** — Background goroutine that runs `git fetch` + `git reset --hard` every 60 seconds to keep local template repos in sync with GitHub.
2. **Wrapper template polling** — Every 10 seconds, the wrapper checks whether the template commit has changed. If so, it sets `StateRestartPending` and waits for Claude to become idle before rsyncing the new template and restarting.
3. **Template configuration** — Stored in `config.yml` under `agentTemplates`, with support for nicknames, `defaultFor` context-based selection (emptyMission, repo, agentTemplate), and canonical `github.com/owner/repo` names.

This machinery exists to give each agent a customized configuration. But Claude Code now has native mechanisms for this: plugins, skills, subagents, and global/project CLAUDE.md files. The template layer is redundant.

### New State

AgenC has no config layer for agent behavior. Users configure Claude natively:

- **Project CLAUDE.md** — Lives in the repo. Travels with the code. Reviewed in PRs.
- **Global CLAUDE.md** — User's personal instructions at `~/.claude/CLAUDE.md`.
- **Skills and plugins** — Claude Code's native extension mechanisms.
- **Subagents** — Claude Code's native delegation mechanism.
- **Per-project settings** — `.claude/settings.json` in the repo.

AgenC's role narrows to what it does well: mission lifecycle, workspace isolation, state capture, and orchestration.

### What Gets Removed

| Component | Location | Status |
|---|---|---|
| Template updater goroutine | `internal/daemon/template_updater.go` | Remove |
| Template rsync logic | `internal/mission/mission.go` (`RsyncTemplate`) | Remove |
| Template polling in wrapper | `internal/wrapper/wrapper.go` (`pollTemplateChanges`) | Remove |
| Template CLI commands | `cmd/template_new.go`, `cmd/template_edit.go`, `cmd/template_ls.go` | Remove |
| Template config schema | `internal/config/agenc_config.go` (`agentTemplates` section) | Remove |
| Template-commit control file | `$AGENC/missions/<uuid>/template-commit` | Remove |
| Wrapper restart-on-template-change state machine | `internal/wrapper/wrapper.go` (`StateRestartPending`) | Simplify |

The wrapper retains its ability to restart on Claude config changes (global settings.json, CLAUDE.md) and workspace repo remote ref changes — these are orthogonal to templates.

### Migration

Existing template users move their configuration into Claude's native system:

1. Move `CLAUDE.md` content into the target repo's `CLAUDE.md` (project-level) or `~/.claude/CLAUDE.md` (global).
2. Move `.claude/settings.json` content into the repo's `.claude/settings.json`.
3. Move `.mcp.json` into the repo root.
4. Convert any template-specific behavior into Claude skills or plugins.
5. Remove agent template entries from `config.yml`.


Repo-Oriented Missions
-----------------------

### Core Change

`agenc mission new` always takes a repo. There are no more "empty missions." Every mission is a session inside a repo clone.

```
agenc mission new github.com/owner/repo "Fix the authentication bug"
agenc mission new github.com/owner/repo   # interactive, no preset prompt
```

### Per-Session Branches

When a mission starts, AgenC creates a branch on the cloned repo:

```
agenc/session/<short-id>
```

Where `<short-id>` is the first 8 characters of the mission UUID (already stored in the database as `short_id`). This branch is the agent's workspace. All changes happen here.

### Auto-Commit After Every Turn

Using the existing Stop hook mechanism, AgenC commits all changes after every agent turn:

```json
{
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "cd \"$CLAUDE_PROJECT_DIR/../workspace/$(ls $CLAUDE_PROJECT_DIR/../workspace/)\" && git add -A && git diff --cached --quiet || git commit -m \"auto-commit after agent turn\""
          }
        ]
      }
    ]
  }
}
```

This builds on the existing hook injection in `claude_config_sync.go`. The Stop hook already writes "idle" to `claude-state`; the auto-commit is an additional command in the same hook.

### Auto-Push After Every Commit

Every auto-commit is followed by an auto-push to the remote. This ensures no work is lost inside an ephemeral mission directory, and enables other tooling (branch management, merge workflows) to operate on the remote.

### Session Branches Behind Main

When the target repo's default branch (e.g., `main`) advances after a session branch was created, that session branch is now behind. AgenC should detect this and flag it.

**Detection:** During the daemon's existing repo polling cycle (or on-demand via `mission ls`), AgenC compares each active session branch against the remote default branch. If `origin/main` has commits not in the session branch, it is flagged as "behind main."

**Surfacing:** `agenc mission ls` displays a visual indicator (e.g., `↓3` meaning 3 commits behind) next to missions whose session branches are behind the default branch. This makes it easy to spot sessions that may need rebasing.

**Resolution:** The user can:
- Rebase interactively: resume the mission and instruct the agent to rebase
- Rebase via command: `agenc mission rebase <mission-id>` triggers a `git fetch origin && git rebase origin/main` on the session branch
- Ignore: some sessions may intentionally diverge

This prevents sessions from silently drifting out of date, which would cause painful merge conflicts later.


Git State Capture — The Reproducible Tuple
-------------------------------------------

Every agent turn produces a reproducible state tuple. Given the same tuple, you can reconstruct exactly what the agent saw and did.

### The Five Components

| # | Component | Source | Capture Mechanism |
|---|---|---|---|
| 1 | **Repo commit** | Target repo, session branch | Auto-commit via Stop hook |
| 2 | **Claude config commit** | User's Claude config repo | AgenC reads HEAD of config repo each turn |
| 3 | **Claude CLI invocation** | Wrapper's spawn of `claude` | Wrapper records full argument list |
| 4 | **Conversation history** | Claude Code's JSONL files | Referenced by session ID |
| 5 | **User prompt** | Mission database | Already captured in `missions.prompt` |

### Component Details

**1. Repo commit.** After every agent turn, the Stop hook auto-commits all workspace changes to the session branch (`agenc/session/<short-id>`). The commit hash is the repo state at that point in time.

**2. Claude config commit.** The user explicitly registers a Claude config repo with AgenC:

```
agenc config set claude-config-repo github.com/owner/claude-config
```

This repo contains the user's Claude configuration: global CLAUDE.md, skills, plugins, settings — whatever the user version-controls. AgenC clones and syncs this repo (similar to how it currently syncs template repos). After each agent turn, AgenC records the config repo's HEAD commit hash alongside the turn's state.

This enables diffing configuration between turns: "Did my CLAUDE.md change cause this regression?"

**3. Claude CLI invocation.** The wrapper already controls how `claude` is spawned. It constructs the command line (interactive vs. headless, resume flags, prompt text). Hyperspace extends this to record the full invocation — including any flags the user passes through (like `--system-prompt`, `--model`, `--allowedTools`) — as a string stored per-turn.

This matters because users may override behavior via CLI flags that are invisible to the config repo. Capturing the invocation closes this gap.

**4. Conversation history.** Claude Code writes conversation history to JSONL files in its project directory. The session ID (already tracked by AgenC in the `session_name` database column) identifies which JSONL file contains the conversation. No additional capture is needed — just a reference.

**5. User prompt.** Already stored in the `missions.prompt` database column.

### Storage

The state tuple is stored in a new database table:

```sql
CREATE TABLE turns (
    id TEXT PRIMARY KEY,             -- UUID
    mission_id TEXT NOT NULL,        -- FK to missions.id
    repo_commit TEXT NOT NULL,       -- commit hash on session branch
    config_commit TEXT,              -- commit hash of Claude config repo (nullable if not configured)
    cli_invocation TEXT NOT NULL,    -- full claude command line
    session_id TEXT NOT NULL,        -- Claude Code session identifier
    created_at TEXT NOT NULL,
    FOREIGN KEY (mission_id) REFERENCES missions(id)
);
```

### What This Enables

- **Replay:** Reconstruct the exact inputs to any turn and re-run it.
- **Debugging:** When an agent produces bad output, inspect exactly what it saw.
- **A/B testing:** Change one component (e.g., CLAUDE.md wording) and compare results.
- **Forensic analysis:** Trace a bug to the specific turn that introduced it, with full context.
- **Friction correlation:** When a user annotates a session with a friction note, the state tuple provides the forensic context (see Friction Capture System below).


Tmux Control Plane
------------------

### Problem

Developers today manage agent sessions in ad-hoc terminal tabs. Switching between sessions requires remembering which tab is which. There is no way to fork a conversation without stopping it, no way to interrogate a session without interrupting it, and no structured way to navigate between missions.

### Architecture

AgenC manages a tmux session (e.g., `agenc`) with one window per active mission. The session orchestrator is responsible for creating, naming, and managing windows.

```
tmux session: agenc
  ├── window 0: "auth-fix"     (mission abc123)
  ├── window 1: "perf-audit"   (mission def456)
  ├── window 2: "docs-update"  (mission ghi789)
  └── window 3: "orchestrator" (personal orchestrator agent)
```

### Core Hotkeys

All hotkeys use the tmux prefix (default: `Ctrl-b`) followed by the key.

#### Fork Conversation

**Hotkey:** `prefix + F`

Snapshots the current session state (captures the state tuple), creates a new tmux window with a forked mission that starts from the same state. The original session continues uninterrupted.

Use case: "I want to explore an alternative approach without losing my current progress." Or: "I want to A/B test two different prompts from this exact state."

Implementation: Creates a new mission with the same repo state (branch from current commit), same config, and a new Claude session initialized with the conversation history up to the fork point.

#### Interrogate

**Hotkey:** `prefix + I`

Opens a side pane (tmux split) that can read the current session's conversation history without interrupting the running agent. The interrogation pane runs a separate Claude instance with read-only access to the session's JSONL history.

Use case: "What happened a few turns ago? Did the agent make a mistake?" — without having to ESC and lose the agent's current train of thought.

#### Pop / Push (Mission Stack)

**Hotkey:** `prefix + P` (pop) / `prefix + U` (push)

Stack-based quick-switch between missions. Push saves the current mission to a stack and switches to a target mission. Pop returns to the most recently pushed mission.

Use case: "I noticed something in this session that needs a quick fix in another repo. Let me pop over, fix it, and come back." This replaces the ad-hoc tab-hunting that developers do today.

### Session Orchestrator

A background component that manages the tmux session lifecycle:

- Creates windows when missions start
- Names windows based on mission context (repo name + short description)
- Cleans up windows when missions end
- Maintains the mission stack for pop/push navigation
- Provides `agenc sessions` command to list all active tmux windows with their mission status

### Personal Orchestrator Agent

An always-running agent (in its own tmux window) that serves as the developer's control plane:

- **Query sessions:** "What's happening in the auth-fix session?" — reads the session's conversation history and summarizes.
- **Query all sessions:** "Which sessions are stuck or idle?" — aggregates status across all active missions.
- **Surface lessons proactively:** "That last interaction in perf-audit looked like a bad pattern. Want to capture it as a friction point?"
- **Accept friction annotations:** The developer can tell the orchestrator "I was frustrated with that interaction in window 2, here's why" and it records the annotation with full state context.

The orchestrator aggregates multiple sessions into a single decision-making interface, reducing the cognitive cost of the supervisory loop.


Missions as Branches
--------------------

### Branch Lifecycle

Every session creates a branch on the target repo:

```
agenc/session/<short-id>     # e.g., agenc/session/a1b2c3d4
```

This branch exists on the remote (auto-pushed). It represents the agent's complete body of work for that session.

### Branch Management Tooling

**List sessions with unmerged branches:**

```
agenc mission ls --unmerged
```

Shows all missions whose session branches have not been merged into the default branch. Includes:
- Mission ID and description
- Branch name
- Number of commits ahead of main
- Number of commits behind main (flagged for rebase — see "Session Branches Behind Main" above)
- Last activity timestamp

**Merge session branches:**

```
agenc mission merge <mission-id>          # merge into default branch
agenc mission merge <mission-id> --squash # squash merge
```

Creates a merge (or squash merge) of the session branch into the default branch. Can be run after reviewing the agent's work.

**Automated merge via PR:**

```
agenc mission pr <mission-id>             # create a GitHub PR from session branch
```

Creates a pull request from the session branch, enabling code review before merge.

**Branch cleanup:**

```
agenc mission archive <mission-id>        # archives mission, optionally deletes branch
agenc mission cleanup --merged            # delete all session branches that have been merged
```

Merged session branches can be cleaned up in bulk.


Background Jobs
---------------

### Purpose

Not all agent work requires active supervision. Some tasks are better run in the background: exploration, documentation cleanup, security audits, test coverage analysis, dependency updates.

### Leveraging Existing Infrastructure

AgenC already has cron support (`specs/crons.md`) with:
- Scheduled headless missions on crontab expressions
- Overlap policies (skip or allow)
- Concurrency limits
- Timeout enforcement

Hyperspace extends this with repo-orientation: background crons now always target a repo and create session branches, just like interactive missions. This means background work is automatically captured in git and can be reviewed, merged, or discarded.

### GitHub Actions Integration

For teams, background work can run via GitHub Actions:

```yaml
# .github/workflows/agent-audit.yml
on:
  schedule:
    - cron: '0 2 * * 1'  # Weekly Monday 2 AM
jobs:
  security-audit:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Run AgenC security audit
        run: agenc mission new . --headless -p "Audit this repo for security issues. Create a report."
```

This leverages existing CI/CD infrastructure for interval-based background work without requiring a persistent daemon.

### Example Background Jobs

- **Documentation sync:** Compare design docs with implementation, flag drift
- **Security audits:** Scan for vulnerabilities, create GitHub issues from findings
- **Test coverage:** Identify untested code paths, generate test stubs
- **Dependency updates:** Check for outdated dependencies, create update PRs
- **Code quality:** Identify code smells, suggest refactors


Friction Capture System
-----------------------

### Problem

Developers constantly encounter friction when working with AI agents — moments where the agent misunderstands, goes down a rabbithole, produces low-quality output, or makes a decision the developer disagrees with. Today, these moments pass uncaptured. The developer course-corrects and moves on. The learning is lost.

### Design

The friction capture system lets developers annotate sessions with friction notes. Each annotation is automatically tagged with the full state tuple, creating a forensic record.

**Capture:**

```
agenc friction "Agent kept modifying the wrong files — it didn't understand the project structure"
```

Or via the personal orchestrator: "I was frustrated with the last few turns in window 2, the agent was stuck in a loop."

**What Gets Stored:**

| Field | Description |
|---|---|
| `id` | UUID |
| `mission_id` | Which mission |
| `turn_id` | Which turn (state tuple reference) |
| `annotation` | Free-text description of the friction |
| `created_at` | Timestamp |

The `turn_id` links to the `turns` table, which contains the full state tuple (repo commit, config commit, CLI invocation, session ID). This means every friction annotation comes with complete forensic context.

**Analysis:**

Friction annotations can be queried to find patterns:

- "Which CLAUDE.md changes correlated with increased friction?"
- "Which types of tasks consistently cause problems?"
- "What was the agent seeing when it went wrong?" (reconstruct from state tuple)

**Proactive Surfacing:**

The personal orchestrator agent periodically reviews friction annotations and suggests corrective actions: "You've had 3 friction events this week related to file targeting. Consider adding project structure documentation to your CLAUDE.md."


Migration Path
--------------

### Phase 1: Eliminate Templates (Simplification)

**Dependencies:** None — this is pure removal.

1. Remove template CLI commands (`template new`, `template edit`, `template ls`)
2. Remove daemon template updater goroutine
3. Remove wrapper template polling and restart-on-template-change logic
4. Remove template rsync from mission creation
5. Remove `agentTemplates` from config schema
6. Remove `template-commit` control file
7. Update `mission new` to no longer accept an agent template argument
8. Update documentation

This is the highest-value first step: it removes significant complexity with no new features to build. The codebase becomes smaller and easier to reason about.

### Phase 2: Repo-Oriented Missions

**Dependencies:** Phase 1 (templates removed, so mission creation logic is simpler).

1. Make repo argument required in `mission new`
2. Implement per-session branch creation (`agenc/session/<short-id>`)
3. Add auto-commit to Stop hook
4. Add auto-push after commit
5. Add behind-main detection and `mission ls` indicator
6. Add `mission rebase` command
7. Add branch management commands (`mission merge`, `mission pr`, `mission cleanup`)
8. Update `mission ls` to show branch info

### Phase 3: Git State Capture

**Dependencies:** Phase 2 (repo-oriented missions provide the repo commit component).

1. Implement Claude config repo registration (`agenc config set claude-config-repo`)
2. Add config repo sync to daemon
3. Implement CLI invocation capture in wrapper
4. Create `turns` table in database
5. Record state tuple after each agent turn
6. Build `agenc turn ls` and `agenc turn inspect` commands

### Phase 4: Friction Capture

**Dependencies:** Phase 3 (friction annotations reference state tuples).

1. Create friction annotations table
2. Implement `agenc friction` command
3. Integrate with personal orchestrator for annotation via natural language
4. Build friction analysis queries

### Phase 5: Tmux Control Plane

**Dependencies:** Phase 2 (missions must be repo-oriented for fork to work).

1. Implement session orchestrator (tmux session management)
2. Implement fork hotkey
3. Implement interrogate hotkey
4. Implement pop/push mission stack
5. Implement personal orchestrator agent
6. Integrate friction capture with orchestrator


Startup Framing (Appendix)
---------------------------

This section is informational context, not an engineering spec.

### Ideal Customer Profile

Companies with teams of senior developers who are already using Claude Code (or similar AI coding tools) and struggling with:
- Rolling out consistent agent configuration across teams
- Capturing and sharing learnings from agent interactions
- Debugging bad agent outputs after the fact
- Managing multiple concurrent agent sessions
- Quantifying the cost of agent misalignment

### Pitch

"We've figured out how to enable highly-paid senior developers to run dozens of Claudes simultaneously AND capture the learnings back into the system to be shared across the team."

### Quantifying Lost Learnings

Every unrecorded friction point represents a lost learning. That learning will cost tokens (time and money) when the same mistake is made again — by the same developer, by a teammate, or by the same agent in a future session. The state tuple enables quantifying this: tie friction points to token cost to measure the financial impact of misalignment.

### Cloud Platform Vision

A cloud platform where developers can farm out encapsulated work environments (missions). Remote execution, team-shared session history, centralized friction analysis, and — critically — audit logging for free. Organizations get full visibility into what agents were doing, what resources they accessed, and what decisions were made.

### Audit Logging

The state tuple is, by construction, an audit log. Every agent turn is recorded with its complete input context. This satisfies compliance requirements that enterprises have around AI tool usage, without requiring any additional work from the developer.


Development Note: Beads
------------------------

AgenC development itself uses [beads](https://github.com/steveyegge/beads) for work tracking. Beads is the task management system for AgenC contributors — it replaces the previous `todos.md` approach with the `bd` CLI for structured work capture.

This is a development process choice for the AgenC project. Beads is **not** part of the Hyperspace architecture and is not used by AgenC's end users. It is mentioned here for completeness: AgenC developers use `bd` to track implementation tasks for the phases described in this spec.
