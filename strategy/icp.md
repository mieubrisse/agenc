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
