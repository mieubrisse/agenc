Alembiq Architecture Design
===========================

Product: **Alembiq** — a SaaS platform for solopreneurs to manage AI agent workforces, built on top of Anthropic's Claude Managed Agents.

Domain: `alembiq.app`

Core Loop
---------

Run agents → extract lessons → upgrade agent configs/skills → agents get smarter. Every cycle, the factory refines itself. The name comes from the Arabic root of "alembic" — the vessel used for distillation.

Target User
-----------

Solopreneurs seeking high leverage through AI agents. Build for self first (dogfooding), then sell as a SaaS product.

Platform
--------

Built on **Claude Managed Agents** (Anthropic's hosted agent-as-a-service, currently in beta).

- API-based: all requests go to `api.anthropic.com` with `managed-agents-2026-04-01` beta header
- Pricing: $0.08/session-hour + standard per-token API costs
- Auth model: BYOK (Bring Your Own Key) for v1; metered billing later
- SDKs: TypeScript (primary), with official SDKs in 7 languages
- CLI: `ant` (Anthropic's CLI tool)

Tech Stack
----------

Cloned from `mieubrisse/SaaS-Boilerplate`:

- **Frontend:** Next.js 14+ (App Router), TypeScript, Tailwind CSS, shadcn/ui
- **Auth:** Clerk (social login, MFA, organizations)
- **Billing:** Stripe (subscription-based for platform access)
- **Database:** Drizzle ORM + PGlite (dev) / PostgreSQL (prod)
- **Deployment:** Vercel or any Node.js host
- **Managed Agents SDK:** `@anthropic-ai/sdk` (TypeScript)

Architectural Approach
----------------------

**Orchestration Layer (Approach B):** Alembiq has its own database that wraps and extends Managed Agents resources. The DB stores agents, skills, session metadata, and user organization data. The backend syncs with Managed Agents (creating/updating agents, uploading skills, managing sessions) but owns the user's view of their data.

- **Alembiq's DB is the primary source of truth** for the user's experience
- **Managed Agents is the execution engine** (running sessions, streaming events, executing tools in containers)
- **Sync is one-directional push:** Alembiq → Managed Agents. No reverse sync needed.

High-Level Architecture
-----------------------

```
┌─────────────────────────────────────────────────┐
│                   Browser                        │
│  Next.js App (React, shadcn, Tailwind)          │
│  ┌──────────┐ ┌──────────┐ ┌──────────────────┐ │
│  │ Sessions │ │ Agents   │ │ Config Engineer  │ │
│  │ Panel    │ │ Library  │ │ (special session)│ │
│  └──────────┘ └──────────┘ └──────────────────┘ │
└────────────────────┬────────────────────────────┘
                     │ Server Actions + SSE
┌────────────────────┴────────────────────────────┐
│              Next.js Backend                     │
│  ┌──────────────┐  ┌─────────────────────────┐  │
│  │ Alembiq DB   │  │ Managed Agents Client   │  │
│  │ (Drizzle/PG) │  │ (Anthropic TS SDK)      │  │
│  └──────────────┘  └─────────────────────────┘  │
└────────────────────┬────────────────────────────┘
                     │ API calls + SSE proxy
┌────────────────────┴────────────────────────────┐
│         Anthropic Managed Agents                 │
│  Agents · Environments · Sessions · Skills       │
│  Cloud containers · Event streams                │
└──────────────────────────────────────────────────┘
```

Ontology
--------

Alembiq's primitives map to Managed Agents resources:

| Alembiq Concept | Managed Agents Resource | Relationship |
|-----------------|------------------------|--------------|
| Agent | Agent | 1:1 — Alembiq agents wrap MA agents with extra metadata |
| Skill | Custom Skill | 1:1 — uploaded via Skills API |
| Session | Session | 1:1 — Alembiq adds notes, tags, organization |
| Environment | Environment | 1:1 — mostly invisible to user in v1 |

There is no Mission/Session split. A Session IS the workspace + conversation (matching Managed Agents' model). "Start fresh on the same project" = create a new session with the same agent.

Data Model
----------

Six core tables. Alembiq owns the user's ontology; Managed Agents IDs are stored as foreign references.

### `users`

Extends Clerk's user with Alembiq-specific data.

| Column | Type | Notes |
|--------|------|-------|
| clerkId | TEXT PK | From Clerk |
| anthropicApiKey | TEXT | Encrypted |
| defaultEnvironmentId | TEXT FK | |
| createdAt | TIMESTAMP | |

### `agents`

The user's agent definitions — Alembiq's core primitive.

| Column | Type | Notes |
|--------|------|-------|
| id | TEXT PK | |
| userId | TEXT FK | |
| name | TEXT | |
| description | TEXT | |
| model | TEXT | e.g. "claude-sonnet-4-6" |
| systemPrompt | TEXT | |
| toolConfig | JSON | Which tools enabled/disabled, permission policies |
| mcpServers | JSON | |
| skillIds | JSON | References to skill_library entries |
| managedAgentId | TEXT | Synced to Managed Agents |
| managedAgentVersion | INTEGER | |
| createdAt | TIMESTAMP | |
| updatedAt | TIMESTAMP | |

### `agent_versions`

Version history for rollback.

| Column | Type | Notes |
|--------|------|-------|
| id | TEXT PK | |
| agentId | TEXT FK | |
| version | INTEGER | |
| snapshot | JSON | Full config at that point |
| changeDescription | TEXT | What changed and why |
| createdAt | TIMESTAMP | |

### `skills`

User's custom skills library.

| Column | Type | Notes |
|--------|------|-------|
| id | TEXT PK | |
| userId | TEXT FK | |
| name | TEXT | |
| description | TEXT | |
| content | TEXT | SKILL.md body |
| managedSkillId | TEXT | Synced to Managed Agents Skills API |
| version | INTEGER | |
| createdAt | TIMESTAMP | |
| updatedAt | TIMESTAMP | |

### `environments`

Wraps Managed Agents environments.

| Column | Type | Notes |
|--------|------|-------|
| id | TEXT PK | |
| userId | TEXT FK | |
| name | TEXT | |
| packages | JSON | pip, npm, etc. |
| networkingConfig | JSON | unrestricted or limited |
| managedEnvironmentId | TEXT | |
| createdAt | TIMESTAMP | |
| updatedAt | TIMESTAMP | |

### `sessions`

Wraps Managed Agents sessions.

| Column | Type | Notes |
|--------|------|-------|
| id | TEXT PK | |
| userId | TEXT FK | |
| agentId | TEXT FK | |
| environmentId | TEXT FK | |
| title | TEXT | |
| status | TEXT | idle/running/terminated |
| repoUrl | TEXT | Nullable — blank sessions have none |
| managedSessionId | TEXT | |
| notes | TEXT | User annotations |
| tags | JSON | |
| createdAt | TIMESTAMP | |
| updatedAt | TIMESTAMP | |

Pages / Routes
--------------

### Dashboard (`/dashboard`)

Active sessions with status indicators (running/idle). Quick-create: new session from an agent. Recent activity feed.

### Session View (`/sessions/[id]`)

- Real-time SSE stream of agent output (messages, tool use, tool results)
- Message input for steering the agent
- Tool confirmation buttons (approve/deny) when permission policy is `always_ask`
- Interrupt button
- Session info sidebar (agent used, repo, notes)

### Agent Library (`/agents`)

List of agents with version numbers. Create new / duplicate / archive.

### Agent Editor (`/agents/[id]`)

- View/edit system prompt, tool config, MCP servers, attached skills
- Version history with diffs and rollback
- "Test" button → creates a session with this agent
- "Improve" button → opens a Config Engineer session scoped to this agent

### Skill Library (`/skills`)

List custom skills. Inline editor for SKILL.md content. Version history. Skills are shared across agents.

### Settings (`/settings`)

API key management. Default environment configuration. Account/billing (Clerk + Stripe).

The Config Engineer
-------------------

A system-level agent that ships with Alembiq. It has:

- The `prompt-engineer` and `claude-skill-management` skills baked in
- A system prompt that understands Alembiq's ontology
- Custom tools that let it read/write agents and skills in the Alembiq DB

When a user clicks "Improve this agent":

1. Alembiq creates a Config Engineer session
2. Injects context: the current agent config + recent session transcripts
3. The user describes what they want changed
4. Config Engineer edits the config and creates a new version
5. User can test immediately by spawning a session with the updated agent

This is a key differentiator — users don't need to write SKILL.md files or system prompts themselves. The Config Engineer does it for them.

Sync Model
----------

One-directional push: Alembiq → Managed Agents.

| User Action | Alembiq | Managed Agents |
|-------------|---------|----------------|
| Creates/edits agent | Saves to DB | `POST/PATCH /v1/agents` → stores managedAgentId |
| Creates/edits skill | Saves to DB | Skills API upload → stores managedSkillId |
| Starts session | Saves to DB | `POST /v1/sessions` → stores managedSessionId |
| Streams output | Proxies to frontend | `GET /v1/sessions/:id/stream` (SSE) |
| Sends message | Forwards | `POST /v1/sessions/:id/events` |

Credentials and Security
-------------------------

- **MCP auth:** Handled natively by Managed Agents vaults. No 1Password wrapper needed.
- **API key:** BYOK — user provides their Anthropic API key, stored encrypted in Alembiq's DB.
- **Container security:** Vault credentials are never reachable from the sandbox. Agent code cannot access tokens directly.

Git Repos in Sessions
----------------------

No pre-clone mechanism in Managed Agents. The agent clones repos via bash at session start. Mitigations:

- Shallow clones (`git clone --depth 1`) for speed
- Anthropic's cloud infra has fast GitHub connectivity
- Acceptable latency given the "manage outcomes, not individual agents" philosophy
- For private repos: GitHub MCP server authenticated via vault, or token passed in system prompt

Managed Agents Reference
-------------------------

### API Endpoints Used

- `POST/PATCH/GET /v1/agents` — Agent CRUD + versioning
- `POST/GET /v1/environments` — Environment CRUD
- `POST/GET/DELETE /v1/sessions` — Session CRUD
- `POST /v1/sessions/:id/events` — Send user events
- `GET /v1/sessions/:id/stream` — SSE event stream
- `POST/GET /v1/skills` — Custom skill upload + management

### Event Types

User → Agent: `user.message`, `user.interrupt`, `user.tool_confirmation`, `user.custom_tool_result`

Agent → User: `agent.message`, `agent.thinking`, `agent.tool_use`, `agent.tool_result`, `agent.custom_tool_use`

Session: `session.status_idle`, `session.status_running`, `session.error`

### Container Specs

Ubuntu 22.04, x86_64, 8GB RAM, 10GB disk. Pre-installed: Python, Node, Go, Rust, Java, Ruby, PHP, C/C++, git, ripgrep, SQLite.

V1 Scope
--------

**In scope:**
- Create sessions (with or without a repo)
- Stream session output in real-time
- Steer agents (send messages, interrupt)
- Switch between active sessions
- Agent library: create, edit, version, rollback
- Skill library: create, edit, share across agents
- Config Engineer: AI-assisted agent/skill editing
- BYOK API key management
- Clerk auth + Stripe subscription billing

**Out of scope for v1:**
- Metered billing (proxy API key)
- Cron / scheduled sessions
- Work queue / hopper system
- Adjutant equivalent
- Multi-agent orchestration (callable_agents)
- Memory stores
- Outcomes / rubric grading
- Environment management UI (use sensible defaults)

Future Roadmap
--------------

- Metered billing (better onboarding, no BYOK requirement)
- Cron jobs / scheduled sessions
- Work organization and prioritization system
- Multi-agent orchestration
- Memory stores for cross-session learning
- Outcomes with rubric-based grading
- Environment customization UI
