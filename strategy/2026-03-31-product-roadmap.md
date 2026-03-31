AgenC Product Roadmap and Strategic Priorities
================================================

Date: 2026-03-31
Source: Strategy session (mission 21f207ea), building on journal entry "wtf-am-i-doing-with-agenc"

This document captures the current strategic direction for AgenC as a product. It complements `creative-direction.md` (positioning/marketing) and `icp.md` (target user). Where this document conflicts with those, this document represents the more recent thinking and those docs should be updated accordingly.

---

Product Vision
--------------

AgenC is a **personal work operating system for solopreneurs** — the Superhuman of agentic work. The full pipeline:

1. Work enters the system (Todoist quick add, voice notes, email, etc.)
2. Agent-assisted triage: AI takes first passes, user gives guidance via annotation, work gets clarified and labeled
3. Greenlit work gets picked up by autonomous Claudes who research, propose, and execute
4. User oversees at CEO level — seeing views of work in flight, intervening only when needed
5. Corrections roll back into the system easily

**Key emotional metrics:** The user feels incredibly effective, powerful, and rewarded. Using the system creates dopamine feedback loops. The user operates like a CEO, not a babysitter.

**What AgenC is NOT:** A task system. A knowledge base. A teams product.

---

Core Strategic Insights (March 2026)
-------------------------------------

### 1. AgenC is an aggregator/orchestration layer, not a system of record

Users will come with their own task systems (Todoist, Linear, Obsidian, GitHub Issues) and knowledge stores (Notion, Google Drive, Markdown repos, personal journals). Many users will have MORE than one of each.

AgenC should sit above all of these and provide:
- A unified view across disparate task and knowledge systems
- The substrate for agents to work effectively across these systems
- Adapters/integrations that bridge AgenC's agent runtime to external systems
- Encouragement/enforcement for agents to store their work in structured, auditable fashion

This means:
- Zero migration cost for new users (they keep what they have)
- Every integration adds stickiness
- The value is in the glue/orchestration, not in the individual components

The analogy: AgenC is to the user's tools what a Chief of Staff is to a CEO's existing systems. The CoS doesn't replace the calendar, the email, or the task list — they make them all work together.

### 2. The methodology is the moat

The product embeds a "how to be a CEO of Claudes" methodology — drawing from GTD, MIRN, project organization, knowledge management. Most solopreneurs don't have this knowledge.

Like Superhuman (sells the Superhuman workflow, not just fast email) or Notion (sells "second brain," not just databases), AgenC sells the methodology of effective agent management. The tool implements the methodology.

This makes the content strategy load-bearing: Substack/Twitter content isn't just marketing — it IS the methodology. It drives people to the tool.

### 3. The teaching component is crucial for non-technical users

A product that effectively teaches people how to harness it will be key to adoption. The methodology knowledge (GTD, task decomposition, delegation, review cycles) exists in the founder's head and needs to be embedded in the product and the content.

---

What Works Well (Current AgenC)
--------------------------------

The **mission lifecycle and runtime** layer is solid:
- Fire-and-forget missions with UUID references
- Quick Claude launching from command palette
- Idle-killing of backgrounded missions
- Auto-renaming of mission tabs
- Tab color changes for mission state (running/attention/completed)
- Emoji indicators for repo identification
- Simple state model: attached or not attached
- Client-server architecture (missions don't need write access to ~/.agenc)
- Single static Go binary, distributed via Goreleaser
- Adjutant (agent that configures AgenC itself)
- Missions spawning sub-missions in other repos
- MCP server integration with 1Password secret injection

---

What Doesn't Work Well (Current AgenC)
---------------------------------------

### Trust/Safety Layer (CRITICAL)
- **Permissions fatigue** — Claude is too conservative with permission prompts. Even with a well-crafted settings.json, Claude flags escaped spaces, $(), etc. This is the #1 daily pain point.
- **No containerization** — agents run on the user's local machine. Any damage is real damage. This prevents truly autonomous operation.
- **Prompt injection risk** — with agents on the local machine, a bad actor could control the agent.

### Interface/Workflow Layer
- **Tmux learning curve** — too steep for most people, including fairly technical users (Aaron, Omar). Copy mode is confusing. Even the founder finds aspects annoying.
- **Session hoarding** — hard to background work and find it later, so users keep too many sessions open, leading to overwhelm. Reboots lose the mental map of in-flight work.
- **No annotation workflow** — no good way to "annotate Claude's response with comments." The side-pane Vim approach is painful.
- **MCP server overhead** — per-session MCP server processes require 1Password auth, startup time, and RAM for each mission. Wish MCP servers behaved like shared services or CLIs.
- **Sub-mission communication** — parent missions can only launch children and poll for results. No continuous coordination, no task delegation to children.

### Task/Work Tracking
- **Fragmented task substrate** — half Todoist, half Beads, half GitHub Issues. No unified view.
- **Beads reliability** — Beads (bd) is consistently flaky and buggy. Significant time spent debugging Beads itself rather than building on top of it.
- **No mission-to-task binding** — no way to track that mission X is working on Todoist task Y.

---

Validation Strategy
--------------------

**Core insight (2026-03-31):** The primary risk is not technical — it's validation avoidance. Months of building without charging anyone or getting product into users' hands. The Kurtosis experience (founder's previous startup) created a subconscious pattern of building infrastructure to avoid the moment of market truth.

**The corrective:** Ship now. Charge money. Learn from real users. Build only what they prove they need.

### Diagnostic Framework (Weekly Ritual)

Use this to diagnose what's actually happening at each stage:

| Signal | Diagnosis |
|--------|-----------|
| Nobody reads your content | Distribution problem — message or channel isn't working |
| People read but nobody wants to try AgenC | Positioning problem — content resonates but AgenC doesn't feel like the solution |
| People try it but stop after a week | Product problem — too much friction, not enough value |
| People try it and keep using it but won't pay | Pricing or value-capture problem |
| People try it, keep using it, and pay | You have something |

This is encoded as the `funnel-analysis` skill for weekly review.

### Early User Data (as of 2026-03-31)

Three onboarding sessions conducted:
- **Omar** — technically smart, FIRE'd, returning to Claude. Liked it, but tmux was a sticking point.
- **Pedro** — smart but non-technical consultant, no command line experience. Tmux was a hard blocker.
- **Yannik** — technical software consultant, heavy Claude user. Liked the promise, but friction points and tmux learning curve.

**Key finding:** Tmux is a universal sticking point across all user types.

### Monetization Strategy (Phase 1)

Don't build a payment platform. Charge manually:
- OSS stays OSS (open core model when web dashboard arrives later)
- Charge for **access to the founder** — onboarding, setup, skill creation for user's workflow, weekly check-ins
- Stripe payment links or Venmo for early users
- This IS the "CEO of Claudes methodology" being sold as a service, before it becomes a product feature

---

Phased Roadmap
---------------

### Phase 1: Validate (April - May 2026, 8 weeks)

**Hard milestone (June 1, 2026):** 6+ Substack posts published, 10+ active AgenC users, at least 1 paying customer. If zero paying customers and nobody asking to pay, reassess whether this is the right product.

**Week 1 (March 31 - April 4): Content pipeline online**
- Substack publishing automation (GitHub Action using `python-substack` library)
- Twitter/X posting mechanism (research in progress — missions 916d601b, 123fdd45)
- Content Waterfall skill (Substack posts → Twitter threads)
- First Substack post published
- First Twitter presence established

**Weeks 2-4 (April): Publish + recruit early users**
- Publish 1+ posts per week (minimum)
- Every post ends with CTA: "Building a personal work OS powered by AI agents. Want early access? Reply / DM me"
- Start conversations with people who engage
- Identify 5-10 potential early users from network + inbound
- Target audience: bright technical solopreneurs (Twitter) + thoughtful writers (Substack)

**May: Onboard + charge**
- Personally onboard early users
- Charge them (even $10-20/month to start)
- Weekly check-ins with each user: "What did you try to do this week, and what happened?"
- Build ONLY what early users are actually blocked on
- Keep publishing (1+ posts/week)

### Phase 2: Decide (June 1, 2026)

Based on 8 weeks of data, answer:
- Are people engaging with the content? (subscribers, replies, shares)
- Are early users actually using AgenC? Or did they try once and stop?
- Did anyone pay? Did anyone almost pay?
- What's the #1 thing people want that AgenC doesn't do?

**If signal is positive:** Double down. Set a goal of $2k MRR by September. Build the top-requested feature (likely web dashboard or containerization based on user demand).

**If signal is neutral/negative:** Real data to make a decision with — not vibes, not Kurtosis PTSD. Options: pivot product, pivot audience, or wind down and do something else.

### Phase 3: Scale or Pivot (July 2026 onward)

Depends on Phase 2 answer. If scaling:
- Web dashboard (revenue unlock for non-tmux users)
- Containerization (if users demand autonomous agents)
- Task adapter layer (if users demand external tool integration)
- Orchestration (parent→child mission coordination)

### Content Strategy (parallel track, ongoing)

- New Substack newsletter focused on Personal Claude OS / CEO-of-Claudes methodology (or rename existing)
- Twitter for reaching technical solopreneurs
- Matt Gray "Content Waterfall": Substack → Twitter threads
- Content IS the methodology. The methodology IS the moat.
- Minimum cadence: 1 post/week

---

Technical Infrastructure Backlog
---------------------------------

These are still important but now driven by user demand, not pre-built:

| Feature | Build when... |
|---------|--------------|
| **Containerization** | Users demand autonomous agents, or permissions fatigue blocks onboarding |
| **Task adapter layer** | Users want AgenC to pull work from their existing tools |
| **Web dashboard** | Ready to serve non-tmux users (likely Phase 3) |
| **Mission orchestration** | Users need multi-agent coordination |

Design directions for each are preserved in this doc's git history and in referenced missions.

---

Open Questions
--------------

1. **Substack strategy:** New newsletter focused on "CEO of Claudes" methodology, or rename existing "Kevin Today" Substack?

2. **Beads future:** Continue investing in Beads, or abandon? The Dolt-backed approach has merit (centralized, separate from repos) but the implementation is unreliable.

3. **MCP vs. CLI for integrations:** MCP servers get attention from service providers (Todoist, Grain) and have a standardized auth story. But agents already know CLIs, and distribution via apt/homebrew is mature.

4. **Pricing model:** $40/month is a starting hypothesis. Need actual pricing data. Start lower ($10-20) for early users.

5. **Product name:** "AgenC" has not been validated with the target market. The current name feels developer-focused; the product is for solopreneurs.

---

Tensions with Existing Strategy Docs
--------------------------------------

The following points in `creative-direction.md` may need revisiting:

| Topic | creative-direction.md | This document |
|-------|----------------------|---------------|
| Core model | Skills + Beads | Aggregator layer over existing tools |
| Task system | Beads as first-class concept | External task systems with adapters |
| Beachhead | Personal CRM | Content production pipeline |
| Target user | "Tech-comfortable, non-terminal users" | Solopreneurs (may or may not be technical) |

These tensions don't need to be resolved immediately, but should be addressed before the web dashboard (Priority 4) since that's when the product positioning becomes customer-facing.

---

References
----------

- Journal entry: `wtf-am-i-doing-with-agenc~2026-03-31_12-23-23`
- Containerization design: mission 5e878df4-6ddd-45d7-b78c-aedfe45775a2
- Linear research: mission 4d2bdf2c-f4da-4d05-aa19-7740ad1a6238, journal entry `linear-research-briefing~2026-03-31`
- Strategy session: mission 21f207ea-5b91-447d-a61e-184dedd06ec9
