Creative Direction
==================

This is the primary strategy reference for the product's creative direction. It is maintained exclusively by the Creative Director and read on every CD invocation to establish context.

**Status:** Bootstrap seed — this document was seeded from the nontechnical repositioning vision (2026-02-27) and has not yet been refined through a full CD intake session. All sections below represent starting context, not validated strategic decisions.

---

Product Context
---------------

**What the product is:** AgenC is a system for orchestrating multiple parallel AI agent sessions. It manages isolation, session switching, configuration, and — critically — the learning loop that makes agents durably better over time.

**The central idea:** The most effective people will become CEOs of a personal organization of AI agents — infinite leverage, with as many "employees" as they want. The driving principle is Unique Work: you should only be doing work that YOU, uniquely, can do. Everything else gets delegated to your agents.

**The core thesis:** AI capability is not the blocker. The blocker is that prompting and context aren't good enough. People carry assumptions, knowledge, and hidden context in their heads, and agents don't have access to any of it. The product that solves the context-extraction problem — and makes agents durably better through a learning loop — becomes the operating system for a personal AI organization.

**Three pillars:**
1. Help turn context in the person's head into context usable by agents (tacit knowledge extraction)
2. Actually execute the work — reliable delegation with real results
3. Make it superfast and supereasy to refine when things go wrong (Inputs Not Outputs: fix the system, not the output)

**Trust model:** Trust is the fundamental resource. Guardrails (you control what agents can do) + continued obedience (agents keep doing what you trained them to do, and get better) = trust. The learning loop is what makes trust durable rather than fragile.

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

**Differentiation hypothesis:** The learning loop. Competitors let you run agents; this product makes agents durably better. Every correction becomes permanent. Trust compounds instead of resetting each session.

This section needs substantial expansion — full competitive landscape analysis is a CD priority.

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
| `strategy/creative-direction.md` | Seeded (needs intake session) | 2026-03-01 |
| `strategy/voice-and-tone.md` | Not yet created | — |
| `strategy/competitive-landscape.md` | Not yet created | — |
| `strategy/founder-profile.md` | Not yet created | — |
| `strategy/decision-log.md` | Not yet created | — |
