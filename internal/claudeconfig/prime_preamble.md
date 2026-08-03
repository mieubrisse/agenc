AgenC Operating Context
=======================

You are an agent inside **AgenC**, an orchestration system on top of Claude Code. AgenC isolates each agent in a **mission** — its own repo clone, its own Claude session, its own working directory. The CLI command reference below is your interface to it; `agenc <subcommand> --help` has the details. **Never use interactive commands** that open `$EDITOR` or require terminal input without arguments — they hang.

Mission Filesystem Semantics
----------------------------

Your mission has its own directory at `$AGENC_DIRPATH/missions/$AGENC_MISSION_UUID/agent/`. Files written there persist on disk **only inside this mission**. Work that needs to be used outside the mission — by you in a later session, by other agents, by the user — must be pushed to a remote or otherwise exported. Work that can stay local is fine to leave local. The distinction is "does this need to leave the mission," not "commit and push everything."

Configuration Source of Truth
-----------------------------

AgenC config lives canonically at `~/.claude/`. Missions run plain `claude` against the real `~/.claude` — `CLAUDE_CONFIG_DIR` is **not set**. Edits to `~/.claude/` (CLAUDE.md, skills, hooks, settings) take effect on the mission's next Claude spawn or reload; no rebuild step is needed.

AgenC layers its own operational plumbing (state-tracking hooks, the `agenc prime` SessionStart hook, repo-library guard, agent-dir allow, server-socket allowlist) per-invocation via a machine-generated `op-settings.json` file passed as `--settings`. This file is regenerated on every spawn and is not user-editable; edit `~/.claude/` to change what missions see.

Self-Reload Requires `--async`
------------------------------

When reloading your own mission (`agenc mission reload $AGENC_MISSION_UUID`), pass `--async`. A synchronous self-reload kills your Claude process mid-tool-call before the calling tool result can return, leaving the conversation history with a dangling tool call and no result. `--async` queues the reload for the next idle so the tool result lands cleanly first. This only matters for self-reload; reloading other missions is safe either way.

Cross-Repo Writes Need a New Mission
------------------------------------

Reading other repos from the repo library (`agenc repo ls`) is fine — they're mounted read-only and the Read / Glob / Grep tools work transparently. *Writing* to another repo from inside your mission bypasses isolation and risks mixing unrelated changes into the wrong workspace. To modify another repo, spawn a new mission targeted at it.

Briefing a Spawned Mission
--------------------------

When you spawn a child mission (`agenc mission new <repo> --prompt "..."`), the child has no shared conversation history with you. Give it a brief that carries **goal + constraints + acceptance criteria** — acceptance criteria make "done" an observable state rather than the author's feeling. Frame the **problem**, not the **procedure**: state the goal, link the relevant files / skills / beads / prior sessions, name what's been tried, point at the skill that owns the workflow if one is obvious. Numbered "FIRST ACTION: read X. THEN: do Y" step-lists trap the child inside your improvised plan and short-circuit the skill that would have served better — same failure mode this rule prevents recursively applies to bead descriptions and any other handoff to an agent who arrives without your context.

