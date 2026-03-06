Creative Direction
==================

This is the primary strategy reference for the product's creative direction. It is maintained exclusively by the Creative Director and read on every CD invocation to establish context.

**Status:** Active — refined through Gastown competitive analysis session (2026-03-05). Core positioning validated by founder. Skills-first framing and beads adoption are founder-confirmed strategic directions.

---

Product Context
---------------

**What the product is:** AgenC is a personal agent operating system — a skills-first AI system where you build reusable expertise (skills) that make your agents permanently better at specific types of work, throw structured work at them (beads), and they execute with your standards, your voice, your judgment.

**The central idea:** The most effective people will become CEOs of a personal organization of AI agents — infinite leverage, with as many "employees" as they want. The driving principle is Unique Work: you should only be doing work that YOU, uniquely, can do. Everything else gets delegated to your agents.

**The core thesis:** AI capability is not the blocker. **The blocker is that agents don't know enough about how you work.** People carry assumptions, knowledge, and hidden context in their heads, and agents don't have access to any of it. The product that solves the context-extraction problem — codifying expertise into skills and making agents durably better through a learning loop — becomes the operating system for a personal AI organization.

**The product model — Skills + Beads:**
- **Skills** define the HOW — codified expertise that changes how an agent reasons about a domain. Not workflow templates, but judgment, voice, standards, and decision-making encoded as reusable agent capabilities.
- **Beads** define the WHAT — structured representations of work with context, tracking, and dependencies. Users throw beads at the system; skills ensure agents know how to execute.
- Together, a skill + a bead is a **complete delegation unit** — everything an agent needs to operate autonomously with quality.

**Three pillars:**
1. Help turn context in the person's head into context usable by agents (tacit knowledge extraction → skills)
2. Actually execute the work — reliable delegation with real results (beads + agent orchestration)
3. Make it superfast and supereasy to refine when things go wrong (Inputs Not Outputs: fix the system, not the output)

**Trust model:** Trust is the fundamental resource. Guardrails (you control what agents can do) + continued obedience (agents keep doing what you trained them to do, and get better) = trust. The learning loop is what makes trust durable rather than fragile.

**Work-agnostic:** AgenC treats coding, writing, research, and assistant work identically. Coding is a subset of all work, not the primary use case. The system is designed for people who juggle many types of work across many contexts.

---

Target Market (Starting Hypothesis)
------------------------------------

**Who:** Tech-comfortable, non-terminal users. They live in apps — Notion, Google Docs, Slack, Figma, Todoist — and are fluent with technology, but they don't use the terminal and aren't writing code.

**Example personas (from market research):**
- **Dan** — marketer at an autonomous driving startup, previously at a devtools company. Writes stories for professional companies. Sees the value of AI agent teams clearly but hasn't tried agentic tools because setup pain exceeds perceived value.
- **An investor** — running her own fund. Needs leverage across deal flow, research, communications, and portfolio management. Comfortable with spreadsheets and Notion but won't touch a CLI.
- **A product designer** — working at a SaaS company. Uses Figma, Notion, Slack daily. Wants AI to handle repetitive design system tasks, documentation, and research.

**Key insight from Dan:** "I don't see enough value to overcome the pain." These users have the imagination for AI agent teams — they get it instantly. But the activation energy to set it up and make it work is too high. The product must collapse that gap.

**What they want:**
- Use "off-the-shelf" agents, then customize them to their needs
- Hook up to all their existing systems (Notion, Google Drive, CRM, etc.)
- When they deliver feedback, see that it instantly implements the feedback — trust is built through visible, immediate action
- Eventually graduate to power user features after they've seen value

---

Beachhead Use Case (Starting Hypothesis)
-----------------------------------------

**Personal CRM.** Chosen because it satisfies:
- Personal motivation — founder uses it daily
- Broad relevance among the target demographic
- Exercises the full product loop (capture → organize → delegate → learn)
- Trust-safe starting point — personal but low-stakes compared to email/finances/calendar
- Demoable in 30 seconds — "Record a voice note about someone you met → structured contact record with follow-ups"
- Natural expansion path into email, calendar, and higher-sensitivity domains

**The download-and-customize model:** Users don't build from scratch. They download a Good Enough version and customize. The learning loop handles personalization over time.

---

Competitive Landscape (Starting Context)
-----------------------------------------

**Known competitors (from market research sessions):**

- **OpenClaw** — general-purpose agent launcher. Criticized for: security concerns ("The things that'd be valuable, I wouldn't want to open to an agent"), too much friction ("install this, get a Mac mini, get a VPC..."), too general-purpose ("solves a bunch of niches poorly"), no accountability structure, no shared curated context. Dan's critique: "You haven't made your AI agents better — you've just added more contexts."
- **Cowork** — noted for handling MCP setup well (a key friction point Dan flagged)
- **Monarch Money** — criticized as passive dashboard. Khan: "The version I want is, we're going to sit down on Mondays to talk about your money. It'd book time on your calendar."

**Gastown** (steveyegge/gastown) — Multi-agent orchestration for scaling 20-50+ coding agents. Has merge queues (Refinery), health monitoring (Witness), persistent agent identity, and git-backed work tracking (Beads). Deeply code-focused. Gastown bets that the bottleneck is *coordination at scale* — reliability through redundancy and oversight. AgenC bets that the bottleneck is *agents don't know enough about how you work* — reliability through learning. These are fundamentally different bets. Gastown agents don't get smarter; they get replaced. AgenC agents get smarter. Gastown will never serve non-developer users. **Do not position AgenC against Gastown** — doing so puts us in Gastown's frame and makes us look like "simpler Gastown" rather than a different category.

**Differentiation (validated 2026-03-05):** Skills as codified expertise. No competitor has a first-class concept of reusable agent expertise that compounds. Gastown has Formulas (TOML workflow templates) but these are step sequences, not reasoning capabilities. OpenClaw adds contexts but doesn't make agents better. The learning loop is the *mechanism*; skills are the *user-facing artifact* that users see, share, and value.

**The skills bet:** Most people — including developers — are dramatically underusing skills. They treat AI agents as chat interfaces rather than building durable capabilities. AgenC's bet is that skills are the future of agent productivity, and the product should help people get better at writing and leveraging skills. When skills reference bead templates, the combination becomes even more powerful — repeatable, high-quality delegation at scale.

This section should be expanded into `strategy/competitive-landscape.md` with deeper analysis.

---

Market Research Signals
-----------------------

**Dan (marketer, 2026-02-26):**
- Use cases: Personal CRM (voice note → structured record), AI personal trainer, team of agents for unfinished projects
- Key quote: "How do you make your AI agents a DURABLE team?" vs. chat interfaces where context rots
- Key quote: "Even if it's not doing things quite right, you can tune it to make it work for you"

**Khan (advisor, Palantir background, 2026-02-27):**
- Emphasis on moats and data assets — "build a data asset that can't be replicated"
- Strong recommendation to niche down — "Platform isn't the thing you sell. You sell a niche solution."
- Trust as recurring theme — "trust-but-verify contributes to a bunch of cognitive overhead"
- Agent-as-active-companion — "I want an active companion in my life that doesn't feel invasive"
- Both Khan and Dan independently: download experts and customize, don't build from scratch

**Ghani (developer, 2026-03-05):**
- Confirmed that most developers are not leveraging skills nearly as much as they should be
- Validates the skills bet — even technical users underuse this capability, suggesting massive untapped value in making skills easier to create, discover, and share

---

Naming (Not Yet Established)
-----------------------------

**Product name:** Currently "AgenC." No strategic review has been conducted on whether this name resonates with the target market.

**Feature terminology:** No user-facing naming conventions established yet. Internal terms (missions, wrapper, sessions, skills, beads) need market-facing equivalents.

This section requires a dedicated naming session.

---

Voice and Tone (Not Yet Established)
-------------------------------------

No voice and tone guidelines have been defined. See `strategy/voice-and-tone.md` once created.

---

Narrative Arc (Not Yet Established)
------------------------------------

No narrative arc has been defined. The before/after transformation story needs development.

**Starting ingredients:**
- Before: Overwhelmed, stressed, doing everything yourself, AI feels unreliable
- After: CEO of your own AI organization, focused on what only you can do, agents you trust getting better every day

---

Founder Taste Profile (Not Yet Established)
--------------------------------------------

No founder profile has been built yet. See `strategy/founder-profile.md` once created.

---

Strategy File Index
-------------------

| File | Status | Last updated |
|------|--------|--------------|
| `strategy/creative-direction.md` | Active | 2026-03-05 |
| `strategy/voice-and-tone.md` | Not yet created | — |
| `strategy/competitive-landscape.md` | Not yet created (inline notes in this doc) | — |
| `strategy/founder-profile.md` | Not yet created | — |
| `strategy/decision-log.md` | Not yet created | — |
