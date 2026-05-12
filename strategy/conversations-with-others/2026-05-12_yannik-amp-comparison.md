Yannik — AgenC vs Amp Differentiator Check
==========================================

- **Date:** 2026-05-12
- **Participants:** Kevin (AgenC creator), Yannik (AgenC early user, daily Amp user)
- **Grain recording:** N/A (WhatsApp exchange)
- **Journal entry:** N/A (not separately captured in journal)
- **Analysis mission:** `877ea3cb-f1b8-45f4-8690-d84cd35403b3` (run `agenc mission print 877ea3cb-f1b8-45f4-8690-d84cd35403b3` for the full analytical context)

---

Source Material
---------------

A short WhatsApp exchange. Two distinct framings from Yannik appeared in the same conversation:

**Framing 1 — Yannik does not see AgenC and Amp as direct competitors:**

> Kevin: "why do you use AgenC and not Amp?"
>
> Yannik: "amp's more like claude code"
>
> Yannik: "somewhat different niche"

**Framing 2 — When asked specifically what AgenC offers that Amp doesn't, the answer was three concrete items:**

> Kevin: "what's the value you get from AgenC that's not in Amp?"
>
> Yannik:
> 1. "being able to use my claude code sub[scription]"
> 2. "repo copying"
> 3. "being able to start new sessions and send key strokes"

These two framings constrain each other. The three-item list is not "AgenC's differentiators against a direct competitor." It is "specific things AgenC delivers that Amp doesn't, given that Yannik views Amp as occupying a different niche entirely." That's a much narrower claim — and it matters for how the data flows into positioning.

Source material is intentionally minimal — a quick WhatsApp reply, not a structured interview. Treat the analysis below as hypothesis-grade signal from one trusted early user, not validated truth.

**Signal weighting:** Yannik is, as of 2026-05-12, one of only two regular AgenC users — Kevin and Yannik. That makes his input unusually high-weight (no other user base to triangulate against) and unusually narrow (a sample of one outside the founder is not representative of any broader population).

---

Summary
-------

Yannik — a daily Amp user and one of AgenC's most engaged early users — was asked point-blank what value he gets from AgenC that he can't get from Amp. He named exactly three things, and only three:

1. **Billing model** — using his Claude Code subscription instead of paying Amp's per-token costs
2. **Workspace isolation** — AgenC's repo-cloning model (one cloned working copy per mission)
3. **Programmatic session control** — being able to spawn missions and send keystrokes from outside the agent

Everything else AgenC offers — skills, hooks, the command palette, MCP integrations, the "CEO of an agent org" framing, the Inputs-Not-Outputs philosophy, the entire creative direction in `strategy/creative-direction.md` — did not make his top-three list against the closest direct competitor he uses every day.

---

What This Conversation Is (And Isn't)
-------------------------------------

**What it is:** A sharp, unfiltered datapoint about how the differentiation actually lands in the head of the person AgenC is closest to converting from a competitor. Yannik is not a passive observer — he's an `Inputs Not Outputs` believer (he reads Kevin's blog), has been onboarded multiple times, has given structured feedback before, and chose to keep Amp as his daily driver anyway.

**What it isn't:** A complete answer. This is a WhatsApp reply, not a structured comparison. Yannik did not say "skills are not differentiating" — he just didn't volunteer them. There are at least two ways to read the absence, and the strategic implication differs sharply between them.

---

Strategic Contextualization
---------------------------

### The three items he did name, decoded

| Yannik's wording | What it maps to in AgenC | Why this lands for him |
|---|---|---|
| "use my claude code sub" | Mission wrapper runs `claude` from the user's own install — billed against the user's Anthropic Max plan, not against Sourcegraph's metered Amp pricing | He already pays for Claude Code. Amp's per-token economics on top of that feel like double-paying for the same model. |
| "repo copying" | The mission model: clone the repo into `agent/`, isolated working copy per mission | He runs multiple agents in parallel. Without per-mission worktree isolation, agents step on each other's branches. Amp's threading model doesn't give him that. |
| "start new sessions and send key strokes" | `agenc mission new ... --prompt` + `agenc mission send-keys ...` | He scripts his own workflows. Programmatic spawning + keystroke injection is the integration surface for everything from cron-style triggers to orchestrating an agent from another agent. |

These are all **plumbing differentiators**, not philosophy differentiators. Each one is something Amp structurally cannot easily match — billing is locked to Sourcegraph's economics, threading is not workspace-isolation, and programmatic-spawn is a CLI-first capability that conflicts with Amp's GUI-thread model.

### The notable absence: the skill-refinement approach

A correction to my first pass: **skills themselves are not an AgenC differentiator** — Claude natively supports skills. The thing that *might* be uniquely AgenC is the **approach to refining skills** (the prompt-engineer skill, the self-refine workflow, the iteration loop that turns friction into durable skill updates).

In the 2026-02-05 ai-coding sync, Yannik volunteered that Amp "DOES use CLAUDE.md (as well as AGENTS.md) and settings.json." In the 2026-03-17 AgenC chat, he specifically asked for **better tooling for skills**: "Currently requires mental power from me, because it's not clear how to abstract the work I'm doing into a skill." He clearly cares about the skill-creation/refinement workflow.

But when asked to name AgenC's value-over-Amp, he did not name the skill-refinement approach either. The current creative direction document leans on skills broadly as the central differentiator — language that needs sharpening to "skill **refinement** approach" if that's the real axis, and then re-tested against Yannik's behavior.

Two readings, both worth holding:

- **Reading A — the skill-refinement approach is differentiating, but invisibly.** Yannik uses it, gets value from it, but doesn't experience it as "the thing Amp can't do" because skill *content* is portable across tools. The differentiation is real; the *positioning* hasn't landed.
- **Reading B — the skill-refinement approach isn't load-bearing for him.** He values it, but it isn't part of his daily AgenC vs. Amp mental model. The unique value he experiences is workspace isolation, economics, and programmatic control. The refinement approach may be the right long-term investment but the wrong **lead** for the value story.

These two readings imply different positioning moves. Resolving which is correct requires asking him directly — not assuming.

### Cross-conversation pattern (with Charles, 2026-03-12)

Charles was the other sophisticated AI-development practitioner Kevin pitched in 2026. Like Yannik, Charles named **concrete plumbing capabilities** as the things that made AgenC interesting — specifically MCP server integrations ("That is cool. And I want that") — not skills, not the philosophy, not the orchestration story. Charles independently built his own prompt-management tool *because of* Kevin's `Inputs Not Outputs` post, but when watching the AgenC demo he latched onto the integration surface, not the skill surface.

Two sophisticated users, asked what's compelling about AgenC vs their existing tool, both answered **infrastructure, not skills.** That's two datapoints, not a validated finding — but it's two datapoints in the same direction, and the creative direction document leans the other way.

### Implication for positioning (medium confidence)

The current top-line positioning leans on skills as the core differentiator. Yannik's data suggests two corrections worth investigating:

1. **The differentiator probably isn't "skills" — it's "the AgenC approach to refining skills."** Claude already provides skills as a primitive. AgenC's possible unique contribution is the iteration workflow around them (prompt-engineer, self-refine, the structural-fix pattern). The creative direction language should be sharpened on this distinction before any further positioning work.
2. **The "AgenC vs Amp" frame is the wrong frame entirely** — at least for Yannik. He explicitly said Amp is "more like Claude Code" and that AgenC is in a "somewhat different niche." Treating Amp as a head-to-head competitor risks fighting on terrain Yannik doesn't even think is contested.

For the user who already has an AI coding tool but wants something AgenC-shaped, the value Yannik articulates is in plumbing — subscription compatibility, workspace isolation, programmatic control. The skill-refinement approach is the *deepening* layer that compounds over time.

**Crux that would change this read:** A follow-up with Yannik on whether the skill-refinement approach is load-bearing for him in daily use. If yes → the positioning needs sharper language about *refinement*, not about skills broadly. If no → the value story leads with plumbing, and the refinement approach is a "stay" feature, not a "switch" feature.

### Counter-pressure: don't over-rotate on a WhatsApp exchange

Three thoughts I want to flag against the analysis above:

1. **WhatsApp ≠ considered.** Yannik gave a 30-second reply. His list is what's top-of-mind, not what's deepest. A structured interview would surface a different list.
2. **N=1 outside the founder.** Yannik is one of two AgenC regular users — Kevin and Yannik. His signal is high-weight because there is almost no other user base to triangulate against, and *low-weight* because a sample of one outside the founder is not representative of any broader population. Both are true simultaneously.
3. **The ICP doc names a wider ICP.** Yannik (and Charles) sit at the sophisticated end. Their conversion wedge may not generalize to less-sophisticated users for whom AgenC's skill-refinement workflow *is* the lead value because they don't already have a comparable agent setup.

These are why the implications above are **medium confidence, not high.**

---

Action Items
------------

- Run a focused follow-up with Yannik on the skill-*refinement* axis (not "skills" broadly — Claude has skills too). The question is roughly: "When you use AgenC's skill-refinement workflow — prompt-engineer, self-refine, the iteration loop — is *that* load-bearing for you, or is it noise? If it's load-bearing, can you describe what you'd lose if it went away?" The answer disambiguates Reading A from Reading B.
- Audit `strategy/creative-direction.md` against this conversation: (a) sharpen "skills as differentiator" language to "skill-refinement approach as differentiator," since Claude has skills natively; (b) re-examine whether Amp belongs in the competitive frame at all, given Yannik's "different niche" read. Creative Director call, not a unilateral edit.
- Preserve Yannik's three-item framing as candidate value-prop copy — *not* as competitive comparison copy. Yannik does not view AgenC and Amp as head-to-head, so a "vs Amp" page is the wrong artifact. But "use the Claude Code subscription you already have, run agents in isolated workspaces, script the whole thing" is strong real-user language for the general value story and worth keeping in a copy parking lot.

---

Cross-Conversation Patterns
---------------------------

Comparing this conversation with [Charles (2026-03-12)](./2026-03-12_charles-agenc.md):

### Pattern: Infrastructure beats philosophy as the named differentiator

Both Yannik (daily Amp user) and Charles (Cursor power user) were asked, in different ways, what they found compelling about AgenC vs. their incumbent tool. Both answered with concrete infrastructure capabilities, not the skills-or-philosophy positioning the creative direction emphasizes.

- **Charles**: MCP server integrations as the "hook" reaction. Volunteered "That is cool. And I want that" specifically about MCP — not about skills or the Inputs-Not-Outputs philosophy he'd already internalized.
- **Yannik**: Three items — subscription economics, workspace isolation, programmatic session control. None of them touched the skill-refinement approach.

**Signal:** For users who already have a functioning AI development setup, the *entry wedge* is infrastructure capability, not abstraction philosophy. The philosophy may be why they *stay* and *deepen*, but it isn't why they *engage*.

**Counter-signal:** Both Charles and Yannik are at the sophisticated end of the ICP. Less-sophisticated users without an incumbent tool may have a different lead-value pattern. Omar (2026-03-10) found the prompt-engineer **skill** to be the thing that stuck — a less-sophisticated user, no incumbent. So the pattern may be **bifurcated by sophistication**: refinement-workflow value leads for users without an incumbent; infrastructure capability leads for users who already have one.

This bifurcation hypothesis is worth testing explicitly in the next user conversation.

### Pattern: The "AgenC vs X" frame may be a category error

Yannik explicitly placed Amp closer to Claude Code than to AgenC, calling AgenC a "somewhat different niche." Charles, during his demo, never engaged with the "repos as agents" metaphor and instead got excited about MCP — a feature that doesn't fit Cursor's product shape at all. Neither user comparison-shopped AgenC against their incumbent in the way "competitive analysis" usually presumes.

**Signal:** It may be that AgenC's primary competitor is *not yet using anything in this shape* — i.e., the alternative is "doing it yourself in Claude Code without AgenC's orchestration layer." Framing strategy around head-to-head against Amp, Cursor, or even Claude Code may be the wrong altitude. Worth testing with the next user conversation whether they perceive AgenC as occupying a competitive slot at all, or as a different kind of thing.
