Ideal Customer Profile (ICP)
============================

Core Profile
------------

AgenC's target user is a **technical professional who wants to systematize their work through AI agents** but isn't necessarily a terminal power user.

### Demographics

- Software engineer, technical founder, or technically-oriented knowledge worker
- Often **working for themselves** — independent consultants, retired engineers building personal projects, solo founders, or freelancers. They have autonomy over their tools and workflow, and they're optimizing their own productivity rather than fitting into a team's existing stack. (Omar: retired engineer building websites, games, garden planning. Yannik: solo knowledge worker with exobrain. Kevin: solo founder.)
- Comfortable writing code and working in a terminal
- Uses Vim (or Vim keybindings) as their primary editing paradigm
- **Not** familiar with tmux — this is a new tool for them, not a comfortable home
- First-class systems user: they think in terms of building and refining systems, not ad-hoc one-off tasks

### Philosophy

Aligned around the idea of **"Inputs, Not Outputs"** (ref: https://mieubrisse.substack.com/p/inputs-not-outputs):

- Believes the highest leverage comes from building systems that compound over time
- Invests in configuring and refining their tools rather than doing repetitive manual work
- Values the feedback loop: use a tool, notice friction, improve the tool, repeat
- Thinks of themselves as the CEO of an organization of AI agents, not as a coder who uses AI for autocomplete

### Key Motivations

- Wants to encode their expertise and preferences into reusable agent configurations
- Wants AI agents that work the way *they* work, not generic assistants
- Values isolation and reproducibility (ephemeral missions, git-backed everything)
- Wants to delegate increasingly complex tasks to agents as trust builds

### What They Are NOT

- Not a "just give me Copilot" user — they want depth, not convenience
- Not intimidated by CLIs, but also not attached to terminal-maximalism for its own sake
- Not looking for a no-code AI builder — they want programmatic control
- Not a casual user — they are willing to invest setup time if the compounding payoff is clear

---

Implications for Product Decisions
-----------------------------------

| Decision area | Implication |
|---|---|
| **Onboarding** | Must get to first "wow" moment fast. Technical users tolerate complexity but not wasted time. Every setup step must feel purposeful. |
| **tmux dependency** | The ICP is NOT a tmux user. tmux is infrastructure that should be invisible, not a feature. Long-term, a GUI may better serve this audience. |
| **Documentation** | Must explain the *why* (systems thinking, feedback loops) not just the *how* (commands). The ICP will invest in understanding the model if they believe in the payoff. |
| **Skills/CLAUDE.md** | The config refinement loop is the core value prop. Must be dead simple to enter and iterate on. First-class tooling for this loop is table stakes. |
| **Vim** | Vim keybindings should work out of the box. Vim-style editing in the terminal should feel natural. |

---

Phase 2 ICP (Not Prioritized)
------------------------------

> **Status:** Documented for future reference. Do not broaden the current beachhead ICP until it is proven. Revisit when the technical beachhead is established and the "download and customize" model is ready.

Evidence from multiple non-technical users validates that a broader market exists beyond the current technical ICP.

### Profile

Tech-comfortable knowledge workers who are **not** terminal users or developers. They use AI extensively for personal productivity, self-improvement, and life management — but through chat interfaces, not code.

### Validated Signals

- **May** (PMM, personal Claude subscriber, March 2026 interview): Independently built end-of-day meta-refinement loops with Claude. Needs prioritization, not execution. Wants a personal assistant ("April") for thought capture, categorization (P0-P3), and calendar integration. Expressed feature fatigue and overwhelm.
- **Dan** (marketer, earlier market research): Tech-comfortable non-terminal user who sees value in AI-augmented personal workflows.
- **Khan** (advisor, earlier market research): Wants "an active companion in my life that doesn't feel invasive." Framed the need as relational, not transactional.

### Key Differences from Phase 1

| Dimension | Phase 1 (Current) | Phase 2 |
|---|---|---|
| **Primary need** | Delegate execution to agents | Prioritization and life management |
| **Interface** | Terminal / CLI | Chat, SMS, mobile |
| **Relationship to code** | Writes it | Doesn't need to |
| **Value of AgenC** | Agent orchestration | Persistent, personalized AI that compounds |
| **"Inputs Not Outputs"** | Skills, CLAUDE.md, self-refine | Meta-refinement loops, instruction improvement |

### What Carries Over

The "Inputs Not Outputs" philosophy is universal — validated across both technical (Omar, Charles) and non-technical (May) users. The meta-refinement loop (refine instructions → better AI interactions → compound over time) is independently invented by users in both segments. This is the connective tissue between Phase 1 and Phase 2.

### What's Different

Phase 2 users need **prioritization, not execution**. May's core unmet need: "I don't have a good enough algorithm for structuring everything that's in my head into a logical, tactical manner." This is a different product category — closer to a life coach than a coding assistant. The product form factor likely shifts from CLI to mobile/chat.
