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

Strategic Priorities (Ordered)
-------------------------------

### Priority 1: Content Pipeline (target: 2 weeks)

**Goal:** Build-in-public pipeline fully operational. All building work can be transformed into published content.

**Definition of done:**
- Can kick off a mission to the personal-writing repo that produces a high-quality Substack draft in the founder's voice from source material (journal entries, conversation transcripts, design sessions)
- Equivalent workflow exists for Twitter/X posts
- Utilizing the Matt Gray "Content Waterfall" system: large Substack posts get broken into smaller Twitter posts
- personal-writing repo publishes to Substack on merge-to-main

**Why first:** Audience compounds over time. Every week without publishing is lost compound growth. The content IS the methodology, and the methodology IS the moat. This doesn't require solving the permissions problem.

**Parallel track:** Content production continues throughout all subsequent priorities. The building sessions themselves are the content source material.

### Priority 2: Trust Boundary / Containerization

**Goal:** Eliminate permissions fatigue. Agents operate autonomously inside containers where they literally cannot cause damage to the host system.

**Design direction** (from mission 5e878df4):
- Docker containers per mission
- `--dangerously-skip-permissions` (bypass Claude's permission system entirely)
- Claude config mounted read-only
- No git credentials inside container — all remote git operations go through `agenc push/pull` via the AgenC server socket
- Server inspects operations before executing with real credentials on the host
- Destructive git operations (force push, branch delete) are structurally impossible — not gated by prompts, but by credential absence

**Why second:** This is the prerequisite for agents running autonomously, which unlocks everything downstream. It's the #1 daily pain point and the hardest problem.

### Priority 3: Task Adapter Layer

**Goal:** Missions can pull work from and report status to external task systems. Unified "what's in flight" view.

**Design direction:**
- Missions get an `objective` field storing a reference to an external task (system + ID)
- Adapters for at least: Todoist, GitHub Issues, Obsidian (covers founder + known user needs)
- `agenc status` shows active missions grouped by objectives, blocked work, missions needing attention
- Agents can read task details from external systems and write status updates back

**Why third:** Closes the loop between "work that needs doing" and "agents doing work." Enables the CEO view. Depends on containerization being in place for agents to operate autonomously on tasks.

### Priority 4: Web Dashboard (MVP for Revenue)

**Goal:** Non-tmux users can use AgenC. This is the revenue unlock.

**What it needs to provide:**
- "What am I working on" view (active missions, status, objectives)
- Task/dependency view with filters (pulling from external task systems via adapters)
- Mission output viewer (read transcripts without tmux)
- Annotation workflow (see Claude's output, add comments fed back to the agent)

**Design considerations:**
- Tmux remains as agent runtime underneath — dashboard is the CEO view on top
- Must make the "missions" metaphor accessible to non-ultra-technical users
- Agents still run in containers; the GUI abstracts away the infrastructure
- The dashboard talks to the existing AgenC server API
- Build the API as if the dashboard exists from Priority 2 onward

**Revenue target:** ~416 users at $40/month = $200k/year gross (covers ~$100k living expenses after taxes).

### Priority 5: Orchestration

**Goal:** Parent missions can coordinate with child missions. Full "CEO delegates to managers who delegate to workers" workflow.

**Includes:**
- Server-mediated mission communication (not just launch + poll)
- Parent missions assigning sub-tasks to children
- Continuous bidirectional communication between parent and child
- Task decomposition: parent breaks work into subtasks, assigns to children, aggregates results

---

Open Questions
--------------

1. **Beads future:** Continue investing in Beads, or abandon? The Dolt-backed approach has merit (centralized, separate from repos) but the implementation is unreliable. If abandoning, what replaces it for repo-level issue tracking?

2. **MCP vs. CLI for integrations:** MCP servers get attention from service providers (Todoist, Grain) and have a standardized auth story. But agents already know CLIs, and distribution via apt/homebrew is mature. Which integration pattern should AgenC standardize on?

3. **Pricing model:** $40/month is a starting hypothesis. Need actual pricing data. Usage-based? Flat? Tiered by number of concurrent agents?

4. **Product name:** "AgenC" has not been validated with the target market. The current name feels developer-focused; the product is for solopreneurs.

5. **Beachhead use case:** `creative-direction.md` identifies Personal CRM. This roadmap identifies content production pipeline. Are these the same thing (different lens) or do they conflict?

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
