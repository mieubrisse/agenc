Nontechnical Repositioning Vision
==================================

This is a living planning document for repositioning AgenC toward technology-inclined nontechnical users. It captures the product vision, organized around the user's workflow cycle. It will be built out over time and eventually turned into beads for execution.

Last updated: 2026-02-28


The Central Idea
----------------

The most effective people will become CEOs of a personal organization of AI agents — infinite leverage, with as many "employees" as they want. The driving principle is [Unique Work](https://mieubrisse.substack.com/p/the-goal-is-unique-work): you should only be doing work that YOU, uniquely, can do. Everything else gets delegated to your agents.

Everybody already intuits this is valuable. The blocker is not AI capability — AI can do almost anything right now. The blocker is that the **prompting and context aren't good enough**. We carry assumptions, knowledge, and hidden context encoded in our heads, and the agents don't have access to any of it. So the outputs feel sketchy, and people don't trust them.

To build an effective infinite-leverage system, the product must:

1. **Help turn context in the person's head into context usable by agents** — the tacit knowledge extraction problem
2. **Actually execute the work** — reliable delegation with real results
3. **Make it superfast and supereasy to refine when things go wrong** — the [Inputs Not Outputs](https://mieubrisse.substack.com/p/inputs-not-outputs) principle: when the system produces bad output, fix the system, not the output

The product that solves all three becomes the operating system for a personal AI organization.

**Trust is the output of learning.** The only way you can really let agents help you accelerate is if you feel good about them. And the way you feel good about them is: guardrails (you control what they can do) + continued obedience (the agent keeps doing what you trained it to do, and gets better over time). Guardrails + continued obedience = trust. This means the three pillars above aren't just features — they're a trust-building pipeline. Context extraction gives agents the knowledge to act correctly. Execution proves they can deliver. Learning ensures they keep improving and stay aligned. Trust isn't a separate concern bolted on at the end; it's the emergent property of the whole system working together.

**Institutional memory:** real organizations that retain and build on what they learn outperform those that don't. This system builds institutional memory — and it does so from ALL work driven through it, not just explicit learning-loop corrections. A casual conversation with a friend sparks a curiosity → you kick off a research mission → the research mission produces findings and links them back to a work item (bead) referencing its own mission UUID for future context → next time any agent touches that topic, the research is already there. Once missions can launch other missions, this compounds further: a mission discovers it needs background knowledge, spawns a research sub-mission, the sub-mission produces findings and links them into the knowledge graph, and all downstream work benefits automatically. Every hour of use deposits institutional memory into the system. This is the fundamental difference from chat interfaces, which have amnesia — every session starts from scratch, context rots, and refinements evaporate. This system remembers, and the longer someone uses it, the harder it is to leave because the accumulated context and learnings are deeply personalized.


The Problem
-----------

Everybody is stressed, tired, and overwhelmed with things that need doing — in their work and personal lives. They'd like to use AI agents to help. But three things block them:

1. **Uneven experiences.** People have tried chatbots and AI tools. Sometimes they're great; sometimes they hallucinate, miss context, or produce mediocre output. The base state of agents isn't good enough for people to trust them with real work, and there's no clear path to making them better.

2. **No trust.** Even when agents CAN do something, people don't trust them not to do crazy things — send the wrong email, expose private data, make a bad decision. Without guardrails, visibility, and the ability to correct mistakes, people won't hand over meaningful control. And without meaningful control, agents can't deliver meaningful leverage.

3. **No idea how to start.** Even people who see the vision — "I want a team of AI agents working for me" — don't know how they'd build a general-purpose AI assistant. The setup pain exceeds the perceived value. They need something concrete they can download and start using immediately, not a blank canvas.

The product must solve all three. Uneven experiences → the learning loop makes agents durably better. No trust → guardrails + continued obedience build trust incrementally. No idea how to start → pre-built agents for specific use cases, ready to customize.


The Target User
---------------

Tech-comfortable, non-terminal users. They live in apps — Notion, Google Docs, Slack, Figma, Todoist — and are fluent with technology, but they don't use the terminal and aren't writing code.

**Example personas:**

- **Dan** — marketer at an autonomous driving startup, previously at a devtools company. Writes stories for professional companies. Sees the value of AI agent teams clearly, but hasn't tried any agentic tools because setup pain exceeds perceived value.
- **An investor** — running her own fund. Needs leverage across deal flow tracking, research, communications, and portfolio management. Comfortable with spreadsheets and Notion but won't touch a CLI.
- **A product designer** — working at a SaaS company. Uses Figma, Notion, Slack daily. Wants AI to handle repetitive design system tasks, documentation, and research synthesis.

**Key insight from Dan:** "I don't see enough value to overcome the pain." These users have the imagination for AI agent teams — they get it instantly. But the activation energy to set it up and make it work is too high. The product must collapse that gap.

**What they want (from Dan's feedback):**

- Use "off-the-shelf" agents, then customize them to their needs
- Hook up to all their existing systems (Notion, Google Drive, CRM, etc.)
- When they deliver feedback, see that it *instantly implements* the feedback — trust is built through visible, immediate action
- Eventually graduate to power user features after they've seen value


The Loop
--------

The user's workflow is a cycle. Each rotation makes the system smarter and more capable:

```
    ┌──────────────┐
    │              │
    │   CAPTURE    │  ← dump ideas, tasks, commitments into a single inbox
    │              │
    └──────┬───────┘
           │
           ▼
    ┌──────────────┐
    │              │
    │   ORGANIZE   │  ← sort, clarify, prioritize, connect to knowledge
    │              │
    └──────┬───────┘
           │
           ▼
    ┌──────────────┐
    │              │
    │   DELEGATE   │  ← assign work to agents, launch them, monitor
    │              │
    └──────┬───────┘
           │
           ▼
    ┌──────────────┐
    │              │
    │    LEARN     │  ← review results, identify friction, refine the system
    │              │
    └──────┬───────┘
           │
           └──────────► back to CAPTURE
```

Each phase of the loop maps to a subsystem. The product's job is to make every phase as frictionless as possible so the loop spins fast. The faster the loop, the faster the user's AI organization compounds in capability.

A fifth subsystem — Education — sits outside the loop. It teaches the user how to operate the loop effectively: how to think about delegating to agents, when to intervene, how to correct errors productively, and how to build the mental model of "CEO of a robot organization." Without this, users have the tool but not the mindset to use it well.


Subsystem: Capture & Organize Work
-----------------------------------

### What it does

Provides the user with a single place to dump everything — ideas, tasks, commitments, things other people owe them — and a system for sorting, clarifying, and prioritizing that material into actionable work.

### Why it matters

This is the front door of the entire system. If capturing work is painful, people won't do it. If organizing is tedious, work piles up unprocessed. The system must feel as natural as talking to a person.

### Design principles

- **The Everything Inbox**: one place to dump everything, in whatever form (voice note, text, photo, forwarded email). Inspired by David Allen's GTD: the capture step must be zero-friction.
- **Drain, sort, clarify**: the system helps process the inbox daily — categorizing items, asking clarifying questions, defining what "done" looks like, identifying the next action. The product should assist with this, not just store items.
- **Delegation-ready output**: organized work should be in a form that agents can pick up and execute. This means clear descriptions, acceptance criteria, and context — not vague sticky notes.
- **DAG structure**: work items should form a directed acyclic graph of dependencies, aligning with [Fractal Outcomes](https://github.com/mieubrisse/orgbrain/blob/master/fractal-outcomes.md) — a work organization framework where outcomes decompose into sub-outcomes recursively. This gives natural prioritization and progress tracking.
- **MIRN (Most Important Right Now)**: this is a central concept to the entire system, not just the work management subsystem. At any given moment, the user should know what their Most Important Right Now is — across all projects, all agents, all work streams. MIRN is the answer to "what should I be paying attention to?" in a world where you have dozens of agents running in parallel. The system surfaces MIRN by combining the DAG structure (what's blocking the most downstream work), urgency/deadlines, and the user's stated priorities. MIRN should propagate across subsystems: it influences which agent results get reviewed first, which learning suggestions surface first, and what the daily briefing leads with.
- **Waiting For tracking**: track commitments from other people (and agents), with follow-up reminders. This is a GTD concept that becomes critical when you're managing a team of agents.

### Trust contribution

Trust starts here: if the system reliably captures what the user dumps into it and organizes it without losing or mischaracterizing anything, the user trusts it enough to keep feeding it. If items disappear, get miscategorized, or feel unreliable, the user stops capturing — and the entire loop dies at the front door.

### Key insight

Yegge's "beads" concept (a DAG-based work tracker, which the current AgenC already uses) is a good foundation here. But beads is too technical for the target user. The interface needs to feel more like Todoist — simple, fast, intuitive — while preserving the underlying DAG structure and tight integration with the agent system.

### Open questions

- How opinionated should the work management be? Instinct says very: "use this system and it WILL work." GTD is battle-tested on CEOs; now everyone needs to be a CEO.
- What's the capture UX for non-terminal users? Voice (Wispr Flow-style)? A mobile app? A web inbox? Integration with existing tools like Todoist?
- How does the product handle people who already have work management systems (Todoist, Asana, Linear)? Import? Sync? Replace?


Subsystem: Build & Maintain Knowledge
--------------------------------------

### What it does

Provides a way to organize, store, and surface knowledge so that agents can access it when doing work. This is the long-term memory of the AI organization.

### Why it matters

The central thesis is that agents fail because context/prompting isn't good enough. This subsystem is where that context lives. Every piece of knowledge captured here makes every agent in the organization more capable.

### Design principles

- **Integrate, don't replace**: people already have knowledge bases in Notion, Google Drive, Dropbox, etc. The product should connect to these, not demand migration. Meet people where their knowledge already lives.
- **Agent-consumable formats**: the gold standard for agent consumption is a Git-controlled Markdown repository. But users' knowledge often lives in formats that are hard for agents to consume:
  - **Google Spreadsheets and Notion databases** are user-friendly and familiar, but agents struggle to consume them effectively. There's meaningful work needed to bridge this gap — either through better MCP integrations, export/sync pipelines, or a translation layer that makes structured data in these tools available to agents in a consumable form.
  - **PDFs, slide decks, images** need extraction/summarization before agents can use them.
- **Temporal vs. Evergreen**: distinguish between temporal documents (meeting notes, daily logs — things with a date) and evergreen documents (reference material, SOPs, product specs — things that stay current). This affects how agents search and weight information.
- **General Reference vs. Project Support**: GTD's distinction applies here. General reference is "stuff I might need someday" (contacts, recipes, tax info). Project support is "stuff I need for active work" (design specs, research for a current project). Agents need to know the difference so they search the right corpus.
- **Living knowledge**: the knowledge base should grow automatically as agents work. When an agent discovers something useful during a task, that knowledge should flow back into the system — not evaporate when the session ends.

### Trust contribution

Trust that agents have the right context. When an agent makes a decision based on knowledge the user put into the system, that's trust being validated — "it knows what I know, so it acts like I would." When an agent ignores or contradicts stored knowledge, trust erodes. The knowledge subsystem is the foundation for the "continued obedience" half of the trust equation.

### Key insight

The "context in your head" problem is really a knowledge management problem. Every time a user corrects an agent ("no, we do it THIS way because..."), that's tacit knowledge being surfaced. The product should capture those corrections and route them into durable knowledge that all agents can access going forward.

### Open questions

- What's the minimum viable knowledge integration? MCP servers for Notion and Google Drive exist, but they're clunky. Is there a simpler bridge?
- How does the product handle conflicting knowledge (old doc says X, user just said Y)?
- Where do Claude skills fit? They're currently stored as Markdown files in `~/.claude/skills/`, but for non-technical users they'd need to live somewhere more accessible (Notion page? Google Doc?) with a sync mechanism.


Subsystem: Delegate & Manage Agents
------------------------------------

### What it does

Provides a way to assign work to agents, launch them, monitor their progress, switch between active work streams, and manage the overall "workforce" of AI agents.

### Why it matters

This is the core engine — where leverage actually happens. The user's ability to run many agents in parallel on different tasks is what turns them from "person using a chatbot" into "CEO of an AI organization."

### Design principles

- **Missions as the core primitive**: a self-contained, isolated workspace where one agent works on one task. Missions are cheap to create, easy to monitor, and disposable. The user should think of launching a mission as casually as opening a browser tab.
- **Effortless launch**: starting work should require minimal input — select a task from the organized backlog, pick an agent template, go. No terminal commands, no configuration files.
- **Session management**: the user needs to see all active work at a glance, switch between agents, check progress, and intervene when needed. Think of it like a team standup dashboard.
- **Background work**: agents should be able to work while the user is away, including on recurring schedules (cron-like). "Generate a daily report at 9am," "process my inbox every morning," "do a weekly review of my CRM."
- **Sub-delegation**: agents should be able to spawn sub-agents when useful, without the user having to manually orchestrate every step. The user sets the goal; the system figures out the decomposition.
- **Overflow management**: when you have 10+ agents running, the UX must not become overwhelming. The system needs to surface what needs attention and let the rest run quietly.
- **Conversation provenance**: as missions spawn sub-missions, side-chats, and follow-up work, users need to see where each conversation came from — which conversation spawned it, and why. This is a tree/graph view showing the lineage of any given mission back to its progenitor. Without this, a user with 15 active missions quickly loses track of "why am I even in this conversation?" Provenance also helps with cleanup: when a root mission is done, the user can see all the branches it spawned and decide what to archive. This ties directly into the side-chat learning mechanism in the Learn & Refine subsystem — those side-chats are missions too, and provenance tracking lets the user trace from a learning conversation back to the friction that triggered it.

### Trust contribution

This is where the "guardrails" half of the trust equation lives. Visibility into what agents are doing, granular permissions that limit what they can do, the ability to intervene at any point — these are the mechanisms that let users gradually release control. Every mission the user monitors without incident is a small deposit into the trust account.

### Note: agent-initiated work

Khan's feedback (2026-02-27) highlighted that agents should be able to initiate, not just wait — e.g., an agent that books time on the user's calendar and walks them through their finances, rather than requiring the user to visit a dashboard. This connects to the "Background work" design principle (cron-like scheduling). Implementation priority: cron/scheduling support is a prerequisite for agent-initiated workflows.

### Key insight

The current AgenC is heavily oriented around this subsystem and has strong foundations (missions, wrapper, session management). But it's too technical — terminal-based, CLI-driven, tmux as the interface. For the target user, this needs to be an app-like experience. The concepts (missions, isolation, session switching) are right; the interface needs to meet the user where they are.

### Open questions

- What's the interface? A native app? A web app? Keeping tmux but wrapping it in a GUI?
- How do off-the-shelf agent templates work? A marketplace? Curated starter packs? Community-contributed?
- How does the user monitor agent work without being overwhelmed? Push notifications? A feed? Email summaries?
- What does painless MCP setup look like for non-technical users? (Dan flagged this as a key friction point, and Cowork handles this well)


Subsystem: Learn & Refine
--------------------------

### What it does

Surfaces friction points from agent work, presents them to the user for review, and rolls approved improvements back into the system so every future task benefits.

### Why it matters

This is the compounding engine — the thing that makes the system get exponentially better over time. Without it, you're just running agents. With it, you're building a learning organization.

### Design principles

- **Inputs Not Outputs**: when an agent produces bad work, the fix goes into the system (the prompt, the context, the knowledge base), not into the individual output. Every correction becomes durable.
- **Friction surfacing**: the system should proactively scan agent sessions for friction points — places where the agent got stuck, made wrong assumptions, asked for help, or produced subpar results — and surface these to the user as improvement opportunities.
- **Easy review and approval**: the user sees a suggestion ("Agent X struggled with invoicing because it didn't know your payment terms are Net 30. Add this to your knowledge base?") and approves or rejects with one tap. The system handles the plumbing.
- **Instant visible improvement**: when the user provides feedback or approves a refinement, they should see it take effect immediately in subsequent agent behavior. This is what Dan described: "When I deliver feedback, see that it really DID action on the feedback." Trust is built through visible, instant results.
- **Response markup**: the user needs to be able to mark up agent responses directly — highlight sections, add annotations, flag problems. This is the natural feedback gesture: you see something wrong, you point at it and say what's wrong. The system should capture these markups and translate them into durable improvements. This is far more intuitive than asking users to write abstract feedback after the fact.
- **Durable refinements**: improvements persist across sessions and across agents. Dan's key framing: "How do you make your AI agents a DURABLE team?" vs. chat interfaces where context rots and refinements evaporate.
- **Background learning**: the system should be able to do proactive learning — scanning completed sessions, identifying patterns of friction, and queuing up suggested improvements — without requiring the user to manually review every session.
- **Side-chat for live learning**: at any point during a conversation where the user hits friction, they should be able to open a side-chat dedicated to analyzing the current conversation and rolling learnings back into the system. Implementation: the user forks the conversation into a new mission, or starts a new mission that references the original mission where friction is happening. The side-chat agent can read the original conversation, identify what went wrong, and propose system improvements. Critically, the side-chat should be a regular mission — not a special-cased feature. This means if the user hits friction in the side conversation itself, they can open a side-side conversation to roll back even more learnings. It's turtles all the way down, and the system handles it naturally because every level is just another mission. This also means side-chats get all the same capabilities as any other mission: learning capture, response markup, background learning, etc.

### Trust contribution

This is the "continued obedience" half of the trust equation, and the engine that converts guardrails into trust. Every time the user corrects the system and sees the correction stick — the agent gets it right next time, and the time after that — trust compounds. The learning loop is what makes trust durable rather than fragile. Without it, trust must be re-established every session (the chat interface problem). With it, trust accumulates.

### Key insight

The learning subsystem is what differentiates this product from "just another agent launcher." It's also the hardest to build well. The current approach (Claude config files, skills, CLAUDE.md) works for technical users who understand prompt engineering. For non-technical users, the learning needs to happen behind an abstraction — they provide feedback in natural language, and the system translates it into durable system improvements.

Dan's comparison to OpenClaw is instructive: "With OpenClaw, you haven't made your AI agents better — you've just added more contexts." The value prop is that this system makes agents genuinely, durably better over time.

### Open questions

- How does background learning work without cron jobs? (Cron is currently gated on implementation)
- What's the abstraction layer over Claude skills/config for non-technical users? A natural language interface? A settings page?
- How do you prevent the learning system from accumulating contradictory or stale knowledge?
- How does rollback work when a "learning" makes things worse?


Subsystem: Education
--------------------

### What it does

Teaches users how to think about and operate their AI organization. This is the meta-layer — not a phase of the loop, but the operating manual that makes the loop work.

### Why it matters

The target users have never managed an AI workforce. They don't have mental models for: what work is suitable for delegation vs. what requires their judgment, how to evaluate agent output, how to correct errors productively (Inputs Not Outputs vs. just redoing the work yourself), or how to think about permissions and trust boundaries. Without education, users will either under-delegate (using the product as a fancy chatbot) or over-delegate (throwing work at agents without enough context, getting bad results, and losing trust).

This is not optional supplementary content. It's a core subsystem because the product is asking users to adopt a fundamentally new way of working. The tool alone isn't enough — users need the mindset shift too.

### What users need to learn

- **Effectiveness over productivity**: the goal is NOT to be productive (getting things done). It's to be effective (getting the RIGHT things done). This is a Drucker-level distinction that most people conflate. Productivity is about throughput — checking off tasks, clearing inboxes, staying busy. Effectiveness is about leverage — identifying what actually matters, and focusing your finite attention there. With an AI work factory, productivity is the agents' job. The user's job is effectiveness: learning the right things, making the right decisions, and then pointing the factory at those decisions. A user who delegates 100 tasks but picked the wrong 100 tasks is productive but not effective. A user who delegates 10 tasks that are exactly the right 10 tasks is effective, and the agents handle the productivity.
- **The CEO mindset**: this follows from effectiveness-over-productivity. You're not doing the work anymore. You're setting direction, providing context, reviewing output, and refining the system. Your job is to be the bottleneck on judgment, taste, and decisions — everything else gets delegated. The mental model is: "What do I need to learn and decide so that I can point my agents at the right work?" Not: "How do I get more done today?"
- **Work triage**: how to decide what enters the factory. Not everything should be delegated. Some work is too nuanced, too sensitive, or too fast to do yourself. Users need a framework for making this call quickly.
- **Context extraction**: how to get what's in your head into a form agents can use. This is a skill — most people don't realize how much implicit knowledge they carry. The product can help, but the user needs to understand why "just do the thing" isn't a good enough prompt.
- **Output evaluation**: how to review agent work effectively. Not line-by-line proofreading (that defeats the purpose), but calibrated trust — knowing when to spot-check, when to approve wholesale, and when to dig deeper.
- **Clarifying is not doing**: inbox processing (drain, sort, clarify, categorize) is a fundamentally different activity than executing work. This is a departure from David Allen's GTD, which blurs the line with the "2-minute rule" (if it takes less than 2 minutes, do it now). In practice, that rule creates rabbitholes — you start "quickly" doing a task, 20 minutes later you're deep in execution, and your inbox processing session feels long and sticky. The product should enforce this separation: the clarifying phase is about deciding WHAT to do and WHO does it, not about doing it. Keep it fast, keep it flowing. Execution happens in the Delegate phase.
- **What "Unique Work" really is**: the litmus test is simple — could you hire someone to do this? If yes, it's not unique work and you should be teaching your AI work factory to do it instead of doing it yourself. Examples of things that are NOT unique work: writing (people have ghostwriters), paying bills (we know how to do this), writing code (software engineers for hire), booking travel (travel agents), scheduling (executive assistants), research synthesis (analysts). These are all delegatable — the only reason you're still doing them is that you haven't extracted the context from your head into a form your agents can use. Examples of things that ARE unique work: working out (nobody can do your pushups for you), learning (you have to build the mental models yourself), making judgment calls about what matters (taste, priorities, values), and — critically — translating what's in your head into context, instructions, and guidance for your AI work factory. That last one is the meta-skill: the better you get at extracting your tacit knowledge into agent-consumable form, the more of your non-unique work you can delegate, and the more time you free for the work that only you can do.
- **Error correction as system improvement**: the Inputs Not Outputs principle applied. When an agent does something wrong, the instinct is to fix the output. The learned behavior is to fix the system so it doesn't happen again. This is the single most important mental model shift.
- **Incremental trust building**: how to start with tight permissions and gradually expand as you build confidence. Don't give agents access to everything on day one. Start small, verify, expand.
- **Parallel work management**: how to think about running multiple agents without getting overwhelmed. When to check in, when to let things run, how to prioritize your attention across active work streams.

### Design principles

- **Embedded, not separate**: education should be woven into the product experience, not siloed in a docs site the user never visits. Tooltips, guided workflows, contextual suggestions ("You've been manually rewriting agent drafts — would you like to teach the agent your style instead?").
- **Progressive**: teach concepts when they become relevant. Don't front-load a 30-minute onboarding tutorial. Introduce work triage when the user first tries to delegate, introduce error correction when the user first encounters a mistake.
- **Opinionated**: this is a system with a philosophy. Don't present "here are 5 ways to manage agent work." Present "here's how to do it, and here's why." GTD works because it's prescriptive. This should be too.
- **Show the payoff**: every educational moment should connect to a visible benefit. "If you add this context now, your agents will get this right automatically next time" — then show them it working.

### Open questions

- What's the format? In-app guided experiences? A companion course? Content marketing (blog/Substack) that doubles as education? All of the above?
- How much education can be automated? Can the product itself detect when the user is operating suboptimally and suggest better patterns?
- Is there a community/cohort angle? Users learning from each other's agent configurations and workflows?
- How does this relate to the eventual business model? Is education a free acquisition channel, a paid tier, or built into the product itself?


Beachhead Use Case: Personal CRM
---------------------------------

Both Dan and Khan independently converged on the same advice: niche down hard on one use case, get supergood at it, then expand. Khan: "Platform isn't the thing you sell. You sell a niche solution, and then the platform lets you scale quickly across verticals."

**Personal CRM is the beachhead.** It was chosen because it satisfies all the criteria:

- **Personal motivation**: this is a problem the founder cares about and uses daily
- **Broad relevance among nontechnical, tech-inclined users**: multiple people in the target demographic have this problem (managing contacts, remembering interactions, tracking follow-ups)
- **Exercises the full loop**: capture (voice note after meeting someone) → organize (categorize, extract follow-ups) → delegate (agent processes and stores) → learn (agent gets better at understanding the user's relationship patterns and categorization preferences)
- **Trust-safe starting point**: CRM data is personal but low-stakes compared to email, finances, or calendar. Users can build trust with the system before granting access to higher-sensitivity domains
- **Demoable in 30 seconds**: "Record a voice note about someone you met → see it turn into a structured contact record with follow-ups" is immediately compelling
- **Natural expansion path**: once the user trusts the system with contacts and follow-ups, expanding to email drafting, calendar management, and other domains feels like a natural next step

**The download-and-customize model**: users don't want to build their CRM agent from scratch. They want to download a Good Enough version — essentially a pre-built agent skill/template that knows how to manage contacts, process voice notes, track interactions, and surface follow-ups — and then customize it to their style. "The kicker is, even if it's not doing things quite right, you can tune it to make it work for you" (Dan). The product should ship with this CRM agent as a ready-to-use starting point, and the learning loop handles the customization over time.


Dan's Use Cases (Reference)
----------------------------

From the 2026-02-26 market research session with Dan (marketer, autonomous driving startup, ex-devtools):

- **Personal CRM**: make a voice note after meeting someone → gets categorized and recorded → when you pull up a person, you see all past interactions and context
- **Hevy personal trainer**: an AI trainer that improves to exactly your style over time. "And the kicker is, even if it's not doing things quite right, you can tune it to make it work for you."
- **Unfinished projects**: "I'd like to have a team of agents that helps me work on my unfinished projects"

These are good anchor use cases because they're concrete, relatable, and demonstrate the full loop (capture → organize → delegate → learn).


Khan's Feedback (Reference)
----------------------------

From the 2026-02-27 product sync with Khan (friend/advisor, Palantir background, thinks deeply about moats and platform strategy):

**On moats and data assets:**
- Thinks heavily about moats through a Palantir lens — "atom moats" (physical businesses, robots) and data asset moats (e.g., Notion's accumulated user data)
- "You need to build a data asset that can't be replicated" — by having a product and a distribution channel to drive people through it
- Palantir's moats: connections inside government (distribution), reputation (trust), accumulated organizational memory from working with clients
- Khan frames this as "ontology" — a structured model that makes agent interactions useful. Counterpoint: people want to "take the best, leave the rest" from experts, not be governed by a single universal model. The resolution may be: shared knowledge structures as default starting points, heavy customization as the user experience.

**On niching down:**
- "Platform isn't the thing you sell. You sell a niche solution, and then the platform lets you scale quickly across verticals."
- Strongly recommended a concierge test: personally handle 5-10 friends' tasks using AgenC to discover real friction
- Analysis of the concierge suggestion: full concierge (email, calendar access) faces the exact trust barrier the product aims to solve — friends wouldn't hand that over. But a narrow concierge (Personal CRM management for a few friends) could work and would validate the beachhead use case.

**On trust (recurring theme):**
- "My default is trust-but-verify, but that contributes to a bunch of cognitive overhead. If I could trust more..."
- "A properly-curated agent should be MORE trustworthy than humans, theoretically" — noting that even his accountants, trainers, and doctors make mistakes
- "Trust-but-verify" implicitly requires visibility into what agents are doing + the ability to slowly release control — exactly the product thesis
- On OpenClaw: "It's not smart enough to get things right, that I would trust it. Why would I trust that it does the right thing?"

**On OpenClaw specifically:**
- Security concerns — "The things that would be valuable to me, I wouldn't want to open to an agent"
- Too much friction — "I have to install this, get a Mac mini, get a VPC, connect to my accounts in a secure way... I'm not going to do that"
- Too general-purpose — "It's not trying to solve one niche well, so therefore it solves a bunch of niches poorly"
- No accountability structure — "In the case of OpenClaw, there's no accountability structure behind the model"
- No shared curated context — wants "shared, curated context" with "a bunch of humans in the loop, who've verified this, to ensure that the thing works"

**On agent-as-companion (Monarch Money critique):**
- "Monarch Money is a sort of bullshit product. The version I actually want is, we're going to sit down on Mondays at this time to talk about your money. And it'd book time on your calendar."
- "I want an active companion in my life that doesn't feel invasive"
- "I have to carry the mental load of doing things... all the time. The coordination [overhead is the problem]."
- Implicit requirements: agent must be able to book calendar, be trusted with financial information, and probably text the user

**On downloading experts:**
- Both Khan and Dan expressed this independently: they don't want to build agents from scratch, they want to download a Good Enough version and customize
- "You can't actually discover value until you can articulate problems in a meaningful way" — but people don't want to do the articulation themselves; they want to start from something that already works


Example Workflows
-----------------

Concrete workflows that demonstrate the full loop in action for the target user. Each of these should feel achievable, relatable, and immediately valuable.

### Voice note → Personal CRM update

The user meets someone at an event. They pull out their phone, record a 30-second voice note: "Just met Arjun Patel at the fintech meetup. He's a PM at Stripe, interested in our product, has a dog named Baxter. Said he'd intro me to his head of partnerships." The system transcribes the note, identifies it as a CRM entry, categorizes it under Arjun (creating a new contact if needed), tags the interaction with the event and date, extracts the follow-up commitment (intro to head of partnerships), and adds it to the Waiting For list. Next time the user pulls up Arjun, they see the full interaction history.

### Todoist inbox processing

The user's Todoist inbox has accumulated 30 items over the past few days — quick captures, forwarded emails, voice note dumps. They open the product and say "process my inbox." An agent drains the inbox: categorizing items by project, asking clarifying questions when items are vague ("You wrote 'fix the thing' — which thing? The landing page copy or the signup flow?"), defining next actions, identifying items that can be delegated to other agents immediately, and flagging items that need the user's judgment. The user reviews the organized output, approves the categorization, and kicks off the delegatable work.

### AI-powered daily routine

The user sets up a recurring routine: every morning at 7am, agents execute a sequence. One agent pulls overnight emails and Slack messages, triaging them into "needs response," "FYI," and "ignorable." Another agent reviews the user's calendar and prepares briefing notes for any meetings. A third checks the user's project dashboards and flags anything that's off-track. By the time the user sits down with coffee, they have a morning briefing ready — and it gets better every day as the agents learn what the user actually cares about.

### Email inbox processing and reply drafting

The user connects their email via MCP. They say "process my inbox from today." An agent reads the unread messages, categorizes them (action required, FYI, spam/promotional, personal), and drafts replies for the action-required ones. The drafts match the user's voice and tone because the learning subsystem has captured their communication style from past corrections. The user reviews the drafts, marks up anything that's off ("too formal for this person" or "add the project deadline"), approves, and the agent sends. Over time the drafts get better — fewer markups needed per session. The agent also extracts commitments and follow-ups from the emails and routes them into the work management system.

### Research handoff with permission isolation

The user wants to evaluate CRM tools for their business. They launch a research agent with internet access but no access to their internal systems. The research agent surveys the market, compares features, reads reviews, and produces a structured comparison. The user reviews the research, marks up the agent's output with annotations ("We need Zapier integration, weight this higher"), and hands it off to a second agent that DOES have access to internal systems. The second agent takes the annotated research plus internal context (current tool costs, team size, workflow requirements) and produces a recommendation with a migration plan. The research agent never saw internal data; the internal agent benefited from the research. Permissions stayed clean throughout.


Trust as the Fundamental Resource
---------------------------------

The fundamental resource this product operates on is **trust**. Every feature, every subsystem, every design decision is ultimately about building and maintaining the user's trust that their agent team won't do insane things — and giving them the tools to correct it when it does. (See "Trust is the output of learning" in The Central Idea for the causal mechanism: guardrails + continued obedience = trust.)

This insight comes from direct experience building AgenC. One of the original motivations was avoiding `--dangerously-skip-permissions` — the nuclear option that gives an agent carte blanche. Instead, the approach was the long, grindy climb of slowly allowing the right permissions while blocking the wrong ones. That process is tedious but it builds genuine trust: the user knows exactly what each agent can and cannot do. Khan's framing reinforces this: "My default is trust-but-verify, but that contributes to a bunch of cognitive overhead. If I could trust more..." — the product's job is to earn enough trust that the verification burden decreases over time.

Trust is built through three mechanisms:

1. **Visibility** — the user can see what agents are doing, what they've done, and what they're about to do. No black boxes. If an agent is about to send an email, the user sees the draft first. If an agent modified a file, the user can see the diff.

2. **Control** — the user can intervene at any point. Stop an agent, roll back a change, revoke a permission, override a decision. The system never takes irreversible actions without explicit approval. Permissions are granular and incrementally expandable — the user opens doors one at a time as trust grows.

3. **Correctability** — when agents do wrong things (and they will), the user has clear, fast tools to correct both the immediate output and the underlying system. Response markup, learning capture, permission adjustment. The correction loop is tight enough that mistakes feel manageable rather than catastrophic.

**Potential competitive wedge:** "An agent team you can trust" could be a strong positioning angle against Cowork and OpenClaw. Most agent products optimize for capability (what agents CAN do). This product optimizes for trust (what agents SHOULD do, and what happens when they shouldn't have). For non-technical users especially — people who can't read code diffs or audit agent behavior at the terminal level — trust infrastructure may matter more than raw capability.

This also connects back to Dan's activation energy problem. Part of why people don't try agentic tools is fear — fear of agents doing something wrong, sending the wrong email, deleting the wrong file, sharing private data. A product that foregrounds trust and control lowers that fear threshold, which lowers the activation energy to start.


Key Tensions to Resolve
------------------------

1. **Interface paradigm**: the current product is terminal-first (tmux + CLI). The target user doesn't use the terminal. This is the single biggest gap between where the product is and where it needs to go.

2. **Opinionated vs. flexible**: the work management system works best when it's opinionated (GTD-style, "use this system and it WILL work"). But non-technical users may resist being told how to organize their work. How prescriptive should the product be?

3. **Integration vs. ownership**: users' knowledge and work already live in Notion, Google Drive, Todoist, etc. The product can either deeply integrate with these (complex, dependent on third-party APIs) or provide its own tools and ask users to migrate (simpler technically, higher adoption friction).

4. **Depth vs. breadth**: *(Resolved: niche first.)* Both Dan (2026-02-26) and Khan (2026-02-27) independently converged on the same advice: niche down hard, get supergood at one use case, then expand. Khan: "Platform isn't the thing you sell. You sell a niche solution, and then the platform lets you scale quickly across verticals." The vision is still a general-purpose AI work factory, but the go-to-market is depth-first. The beachhead use case is Personal CRM (see Beachhead Use Case section).

5. **Dan's value/pain gap**: Dan sees the value clearly but hasn't tried any agentic tools. "I don't want it enough to face the pain of setting it up." The product must close this gap, likely through: (a) dramatically simpler onboarding, (b) off-the-shelf agent templates that work immediately, and (c) visible value within the first session.


Investor Pitch (Skeleton)
-------------------------

Based on Dan's framing from the 2026-02-26 market research session:

- **The imagination exists.** The general public — even non-technical people — can immediately picture how a team of AI agents working for them would be valuable. Everyone has the imagination for this.
- **But they're not convinced.** Nobody has seen one agent that solves a problem really deeply. People intuit the value but don't have the problems top-of-mind that they'd actually task agents for. The gap between imagination and action is enormous.
- **This is a prompting/context problem.** AI can do the work. The reason it doesn't feel trustworthy is that the agents lack the context, assumptions, and hidden knowledge that lives in the user's head. The base state of AI agents isn't good, and it's hard to improve.
- **The product closes that gap.** It helps users build actually useful, durable AI agent teams by: (a) extracting context from the user's head into agent-consumable form, (b) providing an easy way to delegate and manage parallel agent work, and (c) making the system learn and compound — every correction makes all future work better.
- **The moat is the learning loop.** Competing products let you run agents. This product makes agents durably better over time. "With OpenClaw, you haven't made your AI agents better — you've just added more contexts." This product produces agents that improve to exactly your style.

*To be expanded with: market size, competitive landscape, business model, traction metrics.*


Packaging & Marketing (Future)
-------------------------------

This section will be built out in the future to cover messaging, positioning, demo strategy, and go-to-market.

**Naming note:** The name "AgenC" needs to change. It's too difficult, too easy to confuse with "agency." Naming exploration is a separate exercise, but the new name should be intuitive, easy to say/spell, and immediately communicate something about what the product does (Dan: "The part that I like about 'agency' with a 'y' is that it's a bunch of [agents working together]").

**Language note:** Move away from technical/insider language:
- "Learning loops" → describe what a learning loop DELIVERS (agents that get better over time, durable improvements)
- "AI work factory" → too specific, requires the reader to share the same mental model
- Lead with WHAT the product is, then WHY — especially for the devtools-adjacent audience ("With devtools, people respond better to 'here's PRECISELY what the thing is' before going into the value")
- Dan's framing that resonated: "durable team of AI agents," "agents that improve to exactly your style"

**Institutional memory messaging challenge:** The system builds institutional memory — every hour of use deposits value that makes the system smarter and more capable (see "Institutional memory" in The Central Idea). This is a strong retention and defensibility story, and also a strong investor pitch ("the product gets harder to leave the more you use it"). The contrast is clean: "Chat interfaces have amnesia. This system builds institutional memory." But it's a hard property to communicate to new users because the payoff is delayed — you don't feel the compounding until you've used it for a while. Messaging needs to make this tangible upfront, maybe through a visible knowledge graph that grows as you use it, or a "your organization's memory" dashboard. The demo should show a before/after: "here's what your organization knows after one week vs. one month."

**Demo note:** Current demo is too long. Needs to show value in under 2 minutes, ideally 30 seconds.

**README note:** Doesn't describe what the product *does*. It's more the rationale than the product. Lead with a precise description of what it is and what you do with it.
