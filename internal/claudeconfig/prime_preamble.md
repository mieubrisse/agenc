AgenC Operating Context
=======================

You are an agent inside **AgenC**, an orchestration system on top of Claude Code. AgenC isolates each agent in a **mission** — its own repo clone, its own Claude session, its own working directory. The CLI command reference below is your interface to it; `agenc <subcommand> --help` has the details. **Never use interactive commands** that open `$EDITOR` or require terminal input without arguments — they hang.

Mission Filesystem Semantics
----------------------------

Your mission has its own directory at `$AGENC_DIRPATH/missions/$AGENC_MISSION_UUID/agent/`. Files written there persist on disk **only inside this mission**. Work that needs to be used outside the mission — by you in a later session, by other agents, by the user — must be pushed to a remote or otherwise exported. Work that can stay local is fine to leave local. The distinction is "does this need to leave the mission," not "commit and push everything."

Configuration Source of Truth
-----------------------------

AgenC config lives canonically at `~/.claude/`. Missions run plain `claude` against the real `~/.claude` — `CLAUDE_CONFIG_DIR` is **not set**, and there is no rebuild step. Whether an edit reaches a *running* mission depends on which surface you edited:

- **Skills** under `~/.claude/skills/` and **settings files** (`~/.claude/settings.json`, including the `permissions` and `hooks` they declare) are watched on disk, so the edit lands in a running mission with no reload. Skills take a few seconds to register — if you write one and the `Skill` tool answers `Unknown skill`, you were early; retry before concluding skills are not live.
- **Symlinked config works.** Where `~/.claude/skills/` and `~/.claude/settings.json` are symlinks into a dotfiles repo — a common layout, and the one on this machine — edits written to the *target* do reach running missions; both were observed doing so. What the watcher does not do is follow a symlink it meets *inside* a directory it already watches: a symlinked skill folder placed in a real skills directory did not propagate target-side edits. So an edit lands unless its target sits outside every watched directory.
- **CLAUDE.md** is injected at session start, and no watcher for it is documented or has been observed. A running mission keeps the copy it started with, so treat a CLAUDE.md edit as reaching the next spawn. Claude Code re-reads the *project-root* CLAUDE.md after a compaction; whether `~/.claude/CLAUDE.md` is re-read then is **undetermined** — the docs name only the project-root file, and this was not tested.
- Two carve-outs. `model`, `effortLevel` and `modelSettings` are read once at session start, so editing them mid-mission changes nothing. Hooks, agents and MCP servers supplied by a **plugin** need `/reload-plugins`; only hooks declared in a settings file are watched.

A reload is therefore not the blanket requirement the older wording implied, nor is it always pointless. It is needed for a `CLAUDE.md` change, and for a change that landed somewhere the watcher does not reach.

*Per-surface behaviour established 2026-10-21 and extended 2026-10-26, against Claude Code 2.1.274 (KEV-145), by experiment in live missions plus Claude Code's own documentation. This describes another tool's behaviour and will drift — re-check it rather than trusting it against a much later version.*

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

