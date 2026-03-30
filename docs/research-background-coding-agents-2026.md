Background Coding Agents: State of the Art (Early 2026)
========================================================

Research conducted March 30, 2026.

Table of Contents
-----------------

1. [Executive Summary](#executive-summary)
2. [The Permission/Autonomy Problem](#the-permissionautonomy-problem)
3. [Major Tools and Platforms](#major-tools-and-platforms)
4. [Multi-Agent Orchestration](#multi-agent-orchestration)
5. [Sandboxing and Isolation](#sandboxing-and-isolation)
6. [Architectural Patterns](#architectural-patterns)
7. [Key Metrics and Statistics](#key-metrics-and-statistics)
8. [Sources](#sources)


Executive Summary
-----------------

By early 2026, autonomous coding agents have shifted from experimental curiosities to production infrastructure. The defining transformation is the move from chat-based assistance to autonomous execution loops -- agents that work for extended periods, building features, fixing bugs, and creating pull requests with minimal human intervention.

Key trends:
- **Multi-agent is mainstream**: In February 2026, every major tool shipped multi-agent capabilities in the same two-week window (Grok Build, Windsurf, Claude Code Agent Teams, Codex CLI, Devin).
- **Background/async agents are standard**: Cursor, GitHub Copilot, Jules, Codex, and Claude Code all support fire-and-forget task execution that produces PRs.
- **Permission handling remains the central design challenge**: Every tool takes a different approach, from full bypass (`--dangerously-skip-permissions`) to AI-powered classifiers (Claude Code Auto Mode) to sandboxed containers (Docker Sandboxes).
- **78% of Claude Code sessions in Q1 2026 involve multi-file edits** (up from 34% in Q1 2025), with average session length growing from 4 to 23 minutes.
- **The AI agents market is projected at $7.84B in 2025, growing to $52.62B by 2030** (46.3% CAGR).


The Permission/Autonomy Problem
--------------------------------

The core tension in autonomous coding agents: they need to execute commands, write files, and interact with the system -- but unrestricted access is dangerous. Every tool addresses this differently.

### Claude Code: Three Permission Modes

1. **Manual mode** (default): Every tool call requires explicit user approval. Users approve 93% of prompts, indicating massive approval fatigue.

2. **`--dangerously-skip-permissions`**: Bypasses the entire safety stack -- command blocklist, write access restrictions, permission prompts, and MCP server trust verification. All subagents inherit this mode. Should only be used in isolated environments (containers/VMs). Your host machine has SSH keys, `.env` files, browser cookies, and Keychain -- all accessible to the agent.

3. **Auto Mode** (released March 24, 2026): A two-layer defense system using AI classifiers:
   - **Input layer**: Server-side prompt-injection probe scans tool outputs before they enter context
   - **Output layer**: Transcript classifier (Sonnet 4.6) evaluates each action in two stages:
     - Stage 1: Fast single-token filter (high recall, errs toward blocking)
     - Stage 2: Chain-of-thought reasoning only when Stage 1 flags an action
   - The classifier sees only user messages and tool calls -- assistant explanations are stripped to prevent the agent from rationalizing bad decisions
   - Three permission tiers: safe tools auto-approved, file writes within project auto-approved, shell commands/web fetches/subagent spawns routed to classifier
   - 20+ default block rules across four categories: destroy/exfiltrate, degrade security, cross trust boundaries, bypass review
   - Performance: 0.4% false positive rate on real traffic, 17% false negative rate on genuinely overeager actions
   - "Deny-and-continue" logic: 3 consecutive or 20 total denials trigger human escalation
   - Currently available as research preview for Team plan users

### Claude Code Hooks

PreToolUse hooks provide the most granular permission control:
- Run before Claude performs any action (file write, command execution)
- Can return "allow" (skip permission prompt), "deny" (cancel with reason sent to Claude), or "ask" (show prompt to user)
- Three handler types: command hooks (shell scripts), prompt hooks (Claude model evaluation), agent hooks (subagents with tool access)
- PostToolUse hooks run after actions for cleanup (formatting, testing) but cannot undo

### OpenAI Codex

- Cloud sandbox mode: internet access disabled during task execution, isolated container
- CLI mode: granular approval controls for local execution
- Guided by `AGENTS.md` files in the repository

### GitHub Copilot Coding Agent

- Runs in GitHub Actions-powered ephemeral environment
- Restricted internet access and limited repository permissions
- Can only push to branches it creates (e.g., `copilot/*`)
- All PRs require independent human review -- cannot approve or merge its own work
- Environment customized via `.github/workflows/copilot-setup-steps.yml`

### Cursor Background Agents

- Run in isolated Ubuntu-based machines in the cloud
- Have internet access and can install packages
- Operate without maximum tool call limits
- Generate PRs upon completion for human review

### Gemini CLI

- Native gVisor sandboxing (March 2026)
- Experimental LXC container sandboxing
- Plan Mode enabled by default for task decomposition


Major Tools and Platforms
-------------------------

### Tier 1: Terminal/CLI Agents

**Claude Code** (Anthropic)
- Terminal-first agentic coding tool
- Agent SDK: Python/TypeScript wrapper around `claude -p` that communicates over stdin/stdout via JSON-lines
- Headless mode for CI/CD pipelines and automation
- Agent Teams for multi-agent coordination (experimental, v2.1.32+)
- Subagents for focused delegation within a session
- Hooks system for lifecycle automation
- Skills and MCP server integration
- Models: Claude Opus 4.6, Claude Sonnet 4.6
- Source: https://code.claude.com/docs/en/headless

**OpenAI Codex CLI** (OpenAI)
- Open-source, built in Rust
- Full-screen terminal interface
- Two execution modes: cloud sandbox (parallel background) and local CLI
- Models: GPT-5.4, GPT-5.3-Codex, GPT-5.2-Codex
- Guided by `AGENTS.md` repository files
- $1M in API grants for open-source projects at launch
- Available on macOS and Linux (Windows experimental via WSL)
- Source: https://github.com/openai/codex

**Gemini CLI** (Google)
- Open-source, ReAct loop architecture
- Uses Gemini 3.1 Pro Preview (as of Feb 2026)
- MCP server support, `GEMINI.md` system prompts
- Plan Mode enabled by default (March 2026)
- gVisor and LXC sandboxing
- Source: https://github.com/google-gemini/gemini-cli

**Amp** (Sourcegraph, spun out as independent company)
- Formerly Cody
- VS Code extension and CLI
- "Deep mode" for extended autonomous reasoning
- Enterprise-oriented (self-serve plans discontinued 2025)
- Ad-supported free tier (Amp Free)
- Source: https://sourcegraph.com/amp

### Tier 2: IDE-Integrated Agents

**Cursor**
- Agent Mode for autonomous task execution
- Background Agents (launched 2026, v0.50): clone repo in cloud, work autonomously, open PRs
- Up to 8 parallel background agents
- Ubuntu-based cloud execution with internet access
- Source: https://cursor.com/product

**Windsurf** (Cognition)
- 5 parallel agents (shipped Feb 2026)
- Pro tier at $20/month
- Source: https://cursor.com (competitor comparison)

**Google Antigravity**
- Agent-first IDE, announced Nov 2025 alongside Gemini 3
- Two modes: Editor View (IDE) and Manager Surface (agent orchestration)
- 76.2% on SWE-bench Verified
- Free in public preview, supports Gemini 3 Pro, Claude Sonnet 4.5, and GPT-OSS
- Source: https://developers.googleblog.com/build-with-google-antigravity-our-new-agentic-development-platform/

**Cline**
- Open-source VS Code extension, BYOK (bring your own key)
- 5M+ installs, zero markup on API costs
- Plan & Act mode for approval control
- Daily API costs: $5-15 with Sonnet 4.6, $15-40 with Opus 4.6
- Source: VS Code Marketplace

### Tier 3: Cloud/Async Agents

**GitHub Copilot Coding Agent** (GitHub/Microsoft)
- Assign a GitHub issue to Copilot, it works in background
- Runs in GitHub Actions ephemeral environment
- Creates draft PRs, pushes commits, logs progress in real-time
- Supports self-hosted runners (Oct 2025)
- Agentic code review ships March 2026 -- can pass suggestions to coding agent for fix PRs
- Available with Pro, Pro+, Business, Enterprise plans
- Source: https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent

**Google Jules**
- Asynchronous coding agent, integrates with GitHub
- Clones codebase into secure Google Cloud VM
- Creates plans, makes changes, creates PRs
- Out of beta August 2025, powered by Gemini 2.5 (Gemini 3 available for subscribers)
- Private by default, no training on private code
- Source: https://jules.google

**OpenAI Codex (Cloud)**
- Cloud sandbox preloaded with repository
- Parallel background tasks
- Internet disabled during execution
- Source: https://openai.com/codex/

**Devin 2.0** (Cognition)
- Compound AI system -- swarm of specialized models
- End-to-end: requirements to working software
- Devin 2.0 (April 2025): 83% more junior-level tasks per Agent Compute Unit vs 1.x
- Multi-modal: processes UI mockups, Figma, video screen recordings
- Slack integration, API access, mobile app
- Dedicated knowledge management system
- Source: https://cognition.ai/blog/devin-2


Multi-Agent Orchestration
--------------------------

### Built-in Multi-Agent Systems

**Claude Code Agent Teams** (experimental, v2.1.32+)
- Architecture: Team lead + teammates + shared task list + mailbox messaging
- Each teammate is a full independent Claude Code instance with its own context window
- Teammates load CLAUDE.md, MCP servers, and skills but NOT the lead's conversation history
- Task states: pending, in_progress, completed; support dependencies
- File locking prevents simultaneous edits to the same file
- Communication: peer-to-peer messaging, broadcast, automatic idle notifications
- Display modes: in-process (Shift+Down to cycle) or split panes (tmux/iTerm2)
- Quality gate hooks: TeammateIdle, TaskCreated, TaskCompleted
- Permissions: teammates inherit lead's permission settings at spawn time
- Recommended team size: 3-5 teammates, 5-6 tasks per teammate
- Real-world: Anthropic used 16 agents over 2,000 sessions to build a 100K-line Rust C compiler ($20K cost, 2B input tokens, 140M output tokens)
- Source: https://code.claude.com/docs/en/agent-teams

**Claude Code Subagents**
- Run within a single session, report results back to caller only
- Foreground (blocking) or background (concurrent) execution
- Background subagents: Claude Code prompts for tool permissions upfront, subagent inherits permissions and auto-denies anything not pre-approved
- Lower token cost than agent teams (results summarized back)
- Source: https://code.claude.com/docs/en/sub-agents

### External Orchestration Tools

**Conductor** (macOS app)
- Runs multiple Claude Code and Codex agents in parallel
- Each agent in its own isolated Git worktree
- Central dashboard for reviewing and merging PRs
- Source: macOS App Store

**Vibe Kanban** (BloopAI)
- Cross-platform CLI + web UI
- Kanban board for agent tasks with diff review
- Supports Claude Code, Codex, Gemini, Amp
- Free tool (pay for underlying AI services only)
- Source: https://github.com/BloopAI/vibe-kanban

**Claude Squad** (smtg-ai)
- Uses tmux for isolated terminal sessions per agent
- Git worktrees for codebase isolation (each agent on its own branch)
- Manages Claude Code, Codex, OpenCode, Amp
- Source: https://github.com/smtg-ai/claude-squad

**Gastown** (Steve Yegge)
- Go-based system for running multiple AI agents in shared workspace
- Persistent work tracking
- Multi-agent orchestration on top of existing coding agents
- Source: https://github.com/steveyegge/gastown

**OpenClaw** (Peter Steinberger, formerly Clawdbot/Moltbot)
- Open-source AI agent with Gateway daemon architecture
- Gateway: long-lived daemon with WebSocket API, system events (cron, heartbeat)
- Job queue system: spawns agent runs in separate workers, streams progress, persists results
- Source: https://github.com/ (OpenClaw)

**Antfarm** (snarktank)
- TypeScript CLI, zero external dependencies
- Multi-agent orchestration layer for OpenClaw
- Specialized agents: planner, developer, verifier, tester, reviewer
- Deterministic YAML-defined workflows with built-in verification
- Source: https://github.com/snarktank/antfarm

**Container Use** (Dagger)
- Open-source MCP server
- Each agent gets its own containerized sandbox + Git worktree
- Flow: Branch -> Worktree -> Container
- Supports interactive debugging, service tunneling, terminal access
- Works with Claude Code, Cursor, and MCP-compatible agents
- Requires Docker and Git
- Early development stage
- Source: https://github.com/dagger/container-use

**Overstory** (jayminwest)
- Multi-agent orchestration with pluggable runtime adapters
- Workers spawn in git worktrees via tmux
- Custom SQLite mail system for coordination
- Tiered conflict resolution for merging
- Source: https://github.com/jayminwest/overstory


Sandboxing and Isolation
-------------------------

### Docker Sandboxes (Docker Inc.)
- MicroVM-based isolation (macOS and Windows): lightweight microVMs with private Docker daemons
- Each sandbox completely isolated from host Docker daemon, containers, and files outside workspace
- Network isolation with allow/deny lists
- Unique capability: agents can build and run Docker containers while remaining isolated from host
- Bind-mounted workspace directory for filesystem access
- Process containment and resource limits
- Source: https://www.docker.com/products/docker-sandboxes/

### Competing Isolation Approaches (2025-2026)
1. **MicroVMs** (Firecracker, Kata Containers): Strongest isolation, dedicated kernels per workload
2. **gVisor**: User-space kernel, syscall interception without full VMs
3. **Hardened containers**: Only suitable for trusted code

### Platform-Specific Sandboxing
- **OpenAI Codex Cloud**: Isolated container, internet disabled during execution
- **GitHub Copilot**: GitHub Actions ephemeral environment with restricted permissions
- **Cursor Background Agents**: Ubuntu-based cloud VMs with internet access
- **Google Jules**: Secure Google Cloud VM, data stays isolated
- **Google Antigravity**: Cloud-based agent workspaces
- **Claude Code**: Local sandbox with configurable `settings.json` filesystem/network restrictions; Docker Sandboxes integration available


Architectural Patterns
-----------------------

### The Ralph Loop

Popularized by Geoffrey Huntley and Ryan Carson. A stateless-but-iterative pattern for autonomous development:

1. Pick an unfinished task from `prd.json`
2. Implement (write/modify code)
3. Validate (run tests, type checks)
4. If passing: commit, update task status, log learnings to `progress.txt`
5. Reset agent context completely
6. Repeat for next task

Key insight: all memory lives in files and git, not in the model's context. Each iteration starts fresh, avoiding accumulated confusion. Named after Ralph Wiggum (The Simpsons) for being "clueless yet relentlessly persistent."

Source: https://github.com/snarktank/ralph

### The Factory Model (Addy Osmani)

Six-step production line for multi-agent work:

1. **Plan**: Decompose task, define success criteria
2. **Spawn**: Create specialized agents for each piece
3. **Monitor**: Track progress, redirect failing approaches
4. **Verify**: Quality gates (linting, testing, review)
5. **Integrate**: Merge results, resolve conflicts
6. **Retro**: Log learnings for future iterations

"The bottleneck is no longer generation. It's verification."

Source: https://addyosmani.com/blog/code-agent-orchestra/

### Three-Tier Agent Usage Model

- **Tier 1**: Interactive single-agent work (Claude Code, Codex CLI) -- for real-time coding
- **Tier 2**: Local parallel orchestrators (Conductor, Vibe Kanban, Claude Squad) -- 3-10 agents on known codebases
- **Tier 3**: Cloud async agents (Copilot Coding Agent, Jules, Codex Web) -- fire-and-forget background tasks

Most developers in 2026 use all three tiers.

### Spec-Driven Workflows

Developers write detailed feature specifications and architecture notes that agents reference during implementation. "The real skill in working with coding agents is no longer prompt design -- it's context engineering."

### Quality Gates

Essential safeguards for autonomous agents:
- Maximum iteration count
- Docker/container sandbox
- Human approval gate at key decision points
- Token budget limits
- Dedicated @reviewer teammate with read-only access (permanent CI-like quality)
- `CLAUDE.md` / `AGENTS.md` files for persistent context (human-curated shows ~4% improvement over LLM-generated)

### The C Compiler Case Study (Anthropic)

Architecture for 16 parallel agents building a 100K-line Rust C compiler:
- Docker containers, each cloning a shared git repo into `/workspace`, pushing to `/upstream`
- Core loop: `while true; do claude --dangerously-skip-permissions -p "$(cat AGENT_PROMPT.md)"; done`
- Task claiming via file locking in `current_tasks/` directory
- Git synchronization forced conflicts when two agents competed for same task
- No orchestration agent -- agents selected "the next most obvious problem"
- Later introduced specialized roles (dedup, perf, codegen, design critique, docs)
- Results: 99% pass rate on standard compiler test suites, builds Linux 6.9 on x86/ARM/RISC-V
- Cost: ~$20K, 2B input tokens, 140M output tokens, ~2,000 sessions over two weeks


Key Metrics and Statistics
---------------------------

### Usage Metrics (Anthropic 2026 Agentic Coding Trends Report)
- 78% of Claude Code sessions in Q1 2026 involve multi-file edits (up from 34% Q1 2025)
- Average session length: 23 minutes (up from 4 minutes in autocomplete era)
- Average tool calls per session: 47
- Developer acceptance rate: 89% with diff summaries, 62% for raw output
- Developers integrate AI into 60% of their work
- Active oversight maintained on 80-100% of delegated tasks

### Market
- AI agents market: $7.84B (2025) projected to $52.62B (2030), 46.3% CAGR
- 57% of companies run AI agents in production
- Gartner: 1,445% surge in multi-agent system inquiries Q1 2024 to Q2 2025

### Quality Concerns
- Google 2025 DORA Report: 90% AI adoption increase correlates with 9% climb in bug rates, 91% increase in code review time, 154% increase in PR size

### Benchmark Performance (SWE-bench Verified)
- Claude Sonnet 4.5: 77.2% (leaderboard leader)
- Google Antigravity: 76.2%
- Claude Opus 4.5 + Live-SWE-agent: 79.2%
- mini-SWE-agent: >74%

### Cost Data
- Cline with Sonnet 4.6: $5-15/day, with Opus 4.6: $15-40/day
- Power users: $200-500/month in API costs
- C compiler project (16 agents, 2 weeks): ~$20,000
- Steve Yegge claims 12,000 lines of code daily with multi-agent setup


Sources
-------

### Anthropic / Claude Code
- [Claude Code Permissions](https://code.claude.com/docs/en/permissions)
- [Claude Code Auto Mode](https://www.anthropic.com/engineering/claude-code-auto-mode)
- [Building a C Compiler with Parallel Claudes](https://www.anthropic.com/engineering/building-c-compiler)
- [Claude Code Agent Teams](https://code.claude.com/docs/en/agent-teams)
- [Claude Code Subagents](https://code.claude.com/docs/en/sub-agents)
- [Claude Code Headless/SDK](https://code.claude.com/docs/en/headless)
- [Claude Agent SDK (npm)](https://www.npmjs.com/package/@anthropic-ai/claude-agent-sdk)
- [Claude Code Hooks Guide](https://code.claude.com/docs/en/hooks-guide)
- [2026 Agentic Coding Trends Report](https://resources.anthropic.com/2026-agentic-coding-trends-report)
- [Claude Code Auto Mode (9to5Mac)](https://9to5mac.com/2026/03/24/claude-code-gives-developers-auto-mode-a-safer-alternative-to-skipping-permissions/)

### OpenAI / Codex
- [Codex CLI (GitHub)](https://github.com/openai/codex)
- [Introducing Codex](https://openai.com/index/introducing-codex/)
- [GPT-5.3-Codex](https://openai.com/index/introducing-gpt-5-3-codex/)
- [Codex CLI (TechCrunch)](https://techcrunch.com/2025/04/16/openai-debuts-codex-cli-an-open-source-coding-tool-for-terminals/)

### Google
- [Jules](https://jules.google)
- [Gemini CLI (GitHub)](https://github.com/google-gemini/gemini-cli)
- [Google Antigravity](https://developers.googleblog.com/build-with-google-antigravity-our-new-agentic-development-platform/)
- [Gemini 3 in Jules](https://developers.googleblog.com/jules-gemini-3/)

### GitHub / Copilot
- [About Copilot Coding Agent](https://docs.github.com/en/copilot/concepts/agents/coding-agent/about-coding-agent)
- [Copilot Coding Agent 101](https://github.blog/ai-and-ml/github-copilot/github-copilot-coding-agent-101-getting-started-with-agentic-workflows-on-github/)
- [Meet the New Coding Agent](https://github.blog/news-insights/product-news/github-copilot-meet-the-new-coding-agent/)

### Devin
- [Devin 2.0](https://cognition.ai/blog/devin-2)
- [Agents 101](https://devin.ai/agents101)
- [Devin AI Wikipedia](https://en.wikipedia.org/wiki/Devin_AI)

### SWE-agent / Benchmarks
- [SWE-agent (GitHub)](https://github.com/SWE-agent/SWE-agent)
- [mini-SWE-agent (GitHub)](https://github.com/SWE-agent/mini-swe-agent)
- [SWE-bench Verified Leaderboard](https://llm-stats.com/benchmarks/swe-bench-verified-(agentic-coding))

### Multi-Agent Orchestration
- [Addy Osmani: Code Agent Orchestra](https://addyosmani.com/blog/code-agent-orchestra/)
- [Claude Squad (GitHub)](https://github.com/smtg-ai/claude-squad)
- [Vibe Kanban (GitHub)](https://github.com/BloopAI/vibe-kanban)
- [Container Use / Dagger (GitHub)](https://github.com/dagger/container-use)
- [Antfarm (GitHub)](https://github.com/snarktank/antfarm)
- [Overstory (GitHub)](https://github.com/jayminwest/overstory)
- [Ralph Loop (GitHub)](https://github.com/snarktank/ralph)
- [Gastown](https://github.com/steveyegge/gastown)
- [OpenClaw Architecture Gist](https://gist.github.com/championswimmer/bd0a45f0b1482cb7181d922fd94ab978)

### Sandboxing
- [Docker Sandboxes](https://www.docker.com/products/docker-sandboxes/)
- [Docker Sandboxes Blog](https://www.docker.com/blog/docker-sandboxes-run-claude-code-and-other-coding-agents-unsupervised-but-safely/)
- [Sandboxing AI Agents in 2026 (Northflank)](https://northflank.com/blog/how-to-sandbox-ai-agents)

### Cursor
- [Cursor Background Agents (Steve Kinney)](https://stevekinney.com/courses/ai-development/cursor-background-agents)
- [Cursor 2.0 Agent-First Architecture](https://www.digitalapplied.com/blog/cursor-2-0-agent-first-architecture-guide)

### Amp / Sourcegraph
- [Amp](https://sourcegraph.com/amp)
- [Amp Spinout (AI Native Dev)](https://ainativedev.io/news/sourcegraph-spins-out-ai-coding-agent-amp-as-a-standalone-company)

### Industry Analysis
- [State of AI Coding Agents 2026 (Medium)](https://medium.com/@dave-patten/the-state-of-ai-coding-agents-2026-from-pair-programming-to-autonomous-ai-teams-b11f2b39232a)
- [Deloitte: AI Agent Orchestration](https://www.deloitte.com/us/en/insights/industry/technology/technology-media-and-telecom-predictions/2026/ai-agent-orchestration.html)
- [O'Reilly: Conductors to Orchestrators](https://www.oreilly.com/radar/conductors-to-orchestrators-the-future-of-agentic-coding/)
- [Axios: Gas Town, OpenClaw Rise](https://www.axios.com/2026/02/24/agents-openclaw-moltbook-gastown)
- [Mike Mason: AI Coding Agents in 2026](https://mikemason.ca/writing/ai-coding-agents-jan-2026/)
