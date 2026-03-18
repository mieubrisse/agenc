Omar AgenC Onboarding Session
==============================

- **Date:** 2026-03-10
- **Participants:** Kevin (AgenC creator), Omar (friend, software engineer, first-time AgenC user)
- **Grain recording:** https://grain.com/share/recording/7ca44826-fd80-444c-8da2-0e9ddd4a3049/gGwt75J9zei5kknsrFDuDUQ3T1ihrdb6BRyl3FQJ
- **Journal entry:** `omar-agenc-user-onboarding~2026-03-10_10-42-31~.md`
- **Analysis mission:** `d42d57cb-38b4-4052-b633-28f99c4552c5` (AgenC mission that produced this analysis — agents can read the full conversation transcript via `agenc mission print`)

---

Summary
-------

- Kevin walked Omar through a full AgenC installation and first-mission experience over ~1h40m
- The onboarding hit a wall of technical friction: Xcode updates, GitHub auth failures, Claude not on PATH for tmux, wrong tmux session attachment, and a painful restart cycle to fix environment issues
- Once past setup, Omar got productive quickly — created a Claude config repo, learned the command palette, installed the prompt-engineer skill, and launched missions
- tmux was the single biggest UX hurdle — unfamiliar hotkeys, confusing server/session model, opaque status bar
- Omar left excited ("It really is great to use") but flagged that the overall workflow is unclear — when to write skills vs CLAUDE.md, how to bake lessons back in
- Kevin concluded that a GUI may be necessary to lower the barrier to entry

---

Action Items
------------

### Kevin

- [ ] Add note to brew install about Xcode being a Homebrew requirement, not AgenC
- [ ] Fix GitHub cloning to use user's preferred method (SSH vs HTTPS)
- [ ] Make `agenc config init` idempotent — rerunning should retry failed steps, not just print "token: not set up"
- [ ] Fix `agenc attach` connecting to wrong tmux session (agenc-pool)
- [ ] Make PATH changes propagate with just `agenc server restart` — no tmux kill needed
- [ ] Fix "reconfig & reload" printing garbage output
- [ ] Add "reconfig all missions" capability
- [ ] Fix reconfig not preserving tab status/Claude status correspondence
- [ ] Build first-class onboarding flow for Claude config repo (create, version-control, push, register with `--always-synced`)
- [ ] Improve AgenC-injected context so agents understand to modify the config repo source, not `$CLAUDE_CONFIG_DIR`
- [ ] Add documentation: when to use CLAUDE.md vs skills (global vs per-repo, always-loaded vs on-demand)
- [ ] Add default allowed commands to settings.json (git, ls, touch, file, mkdir) to reduce permission prompt friction
- [ ] Explain "don't ask again" scope (per-mission) in UI
- [ ] Move tmux hotkey explainer to mission launch time
- [ ] Make Adjutant aware of user's GitHub username
- [ ] Teach Adjutant what "always synced" means
- [ ] Provide a starter tmux status bar config for new users
- [ ] Consider building prompt-engineer skill into AgenC itself (auto-generated per mission)
- [ ] Evaluate GUI/IDE investment to replace tmux dependency

### Omar

- [ ] Continue building out his Claude config (CLAUDE.md, skills)
- [ ] Explore the prompt-engineer skill for creating new skills
- [ ] Read through AgenC codebase/docs to solidify mental model

---

Themes
------

**1. Setup friction is a conversion killer.** The first 30 minutes were spent fighting installation and environment issues — Xcode, GitHub auth, PATH, tmux sessions. Omar was patient because he's a friend. A stranger would have bounced. Every minute of setup pain before the user sees value is a risk.

**2. tmux is a double-edged sword.** It gives powerful session management and backgrounding, but introduces an entire layer of concepts (servers, sessions, panes, environment inheritance) that new users don't have. Omar had never used `screen` before. The tmux server's environment not inheriting PATH changes was the single most painful debugging episode.

**3. The "config refinement loop" is the core value prop — but it's not self-evident.** The power of AgenC comes from iteratively refining your Claude config (CLAUDE.md, skills, settings). But Omar explicitly said he was unclear on the workflow: "when to be writing skills, or just a Claude md. How to bake lessons back into its own overall or skill behaviours." This is the most important thing to get right in onboarding — if users don't understand the feedback loop, they won't get compounding value.

**4. Dogfooding works.** Many quality-of-life features (Adjutant, custom palette commands) only exist because Kevin uses AgenC daily and feels the pain. This is a genuine competitive advantage.

**5. First-class concepts reduce confusion.** Every time something required manual steps (creating the config repo, symlinking, registering with `--always-synced`), Omar needed hand-holding. Making the config repo a first-class concept with a single command would eliminate an entire category of confusion.

---

Agreement and Disagreement
--------------------------

### Aligned On

- AgenC's mission model is powerful — Omar liked the ephemeral isolation
- The prompt-engineer skill approach is valuable ("using prompt eng to generate the Claude so far has been dope")
- Skills are the right abstraction for encoding reusable agent behaviors
- tmux is a significant learning curve

### Diverged On

- **GUI vs terminal**: Kevin is hesitant about the GUI investment; Omar's struggle with tmux suggests he'd strongly prefer a GUI. Kevin's own notes acknowledge this tension — he knows it'd help but is trying to hold off due to engineering cost.

---

Insights
--------

1. **The onboarding has two distinct failure modes.** The first is *technical* (things breaking during setup). The second is *conceptual* (not understanding the workflow). They need different solutions — the technical issues need bug fixes; the conceptual gap needs documentation, guided flows, and possibly an interactive tutorial.

2. **Omar's post-session feedback is the most important signal.** His WhatsApp messages reveal what stuck: the prompt-engineer skill (immediate tangible value) and ephemeral missions (novel and cool). He did *not* mention tmux, the command palette, or Adjutant — the infrastructure melted away. The value is in the *agent capabilities*, not the orchestration UI.

3. **The PATH/tmux debugging episode (~15 minutes) reveals a fundamental architecture issue.** The tmux server inheriting environment at launch time means any PATH change requires killing and restarting the server — which means restarting all sessions. This is not a bug to patch; it's a design constraint of tmux that will keep causing pain. This alone may justify the GUI investment.

4. **"Make it idempotent" appeared multiple times.** `agenc config init` failing and not recovering, reconfig printing garbage, missions exiting on clone errors — these all point to a pattern where AgenC's setup/config paths assume the happy path. Hardening error handling and making operations rerunnable would fix a large cluster of issues at once.

5. **Omar's game project (Godot/procedural generation) is a good test case for AgenC.** It's a different domain from typical Go/web work. If AgenC works well for game dev, that's a strong signal of generality.

---

Omar's Direct Feedback (WhatsApp)
----------------------------------

> "It really is great to use and using prompt eng to generate the Claude so far has been dope. It pretty much one shot a website"

> "I like the ephemeral mission thing. It caught me out a few times with state that hadn't updated (needed to git pull) but it's very cool to have."

> "Maybe this is there but I am unclear on the workflow as a whole, or rather how to execute that workflow in agenc. Things like when to be writing skills, or just a Claude md. How to bake lessons back into its own overall or skill behaviours etc"
